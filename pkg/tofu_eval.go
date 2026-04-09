// Copyright 2016-2025, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pkg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	goplugin "github.com/hashicorp/go-plugin"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/configs"
	"github.com/pulumi/opentofu/configs/configload"
	"github.com/pulumi/opentofu/encryption"
	"github.com/pulumi/opentofu/logging"
	"github.com/pulumi/opentofu/modsdir"
	tfplugin "github.com/pulumi/opentofu/plugin"
	"github.com/pulumi/opentofu/providercache"
	"github.com/pulumi/opentofu/providers"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/opentofu/states/statefile"
	"github.com/pulumi/opentofu/tofu"
)

// StateFormat represents the format of a Terraform/OpenTofu state file.
type StateFormat int

const (
	// StateFormatRaw is a raw .tfstate file (version 4 JSON with no "format_version" key).
	StateFormatRaw StateFormat = iota
	// StateFormatTofuShowJSON is the output of `tofu show -json` (has a "format_version" key).
	StateFormatTofuShowJSON
)

// DetectStateFormat auto-detects whether the file at path is a raw .tfstate
// or the JSON output of `tofu show -json`. Raw tfstate files have a top-level
// "version" key but no "format_version" key, while tofu show JSON has a
// "format_version" key.
func DetectStateFormat(path string) (StateFormat, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading state file %s: %w", path, err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return 0, fmt.Errorf("parsing state file %s as JSON: %w", path, err)
	}

	if _, hasFormatVersion := top["format_version"]; hasFormatVersion {
		return StateFormatTofuShowJSON, nil
	}
	return StateFormatRaw, nil
}

// LoadConfig loads a Terraform/OpenTofu configuration from the given directory.
// The tfDir must be an absolute path. Module sources are resolved relative to it
// using the .terraform/modules/ directory.
//
// Because the configload package resolves module Dir entries from modules.json
// relative to the process working directory, this function rewrites relative
// Dir entries to be absolute (resolved against tfDir) in a temporary copy of
// the modules manifest.
func LoadConfig(tfDir string) (*configs.Config, error) {
	origModulesDir := filepath.Join(tfDir, ".terraform", "modules")

	// Read the original manifest and make Dir entries absolute relative to tfDir.
	manifest, err := modsdir.ReadManifestSnapshotForDir(origModulesDir)
	if err != nil {
		// If there's no manifest (no modules), proceed with the original dir.
		manifest = nil
	}

	modulesDir := origModulesDir
	if manifest != nil {
		// Rewrite relative Dir entries to absolute paths.
		for key, record := range manifest {
			if !filepath.IsAbs(record.Dir) {
				record.Dir = filepath.Join(tfDir, record.Dir)
				manifest[key] = record
			}
		}

		// Write the rewritten manifest to a temp directory.
		tmpDir, err := os.MkdirTemp("", "tofu-modules-*")
		if err != nil {
			return nil, fmt.Errorf("creating temp dir for modules manifest: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		if err := manifest.WriteSnapshotToDir(tmpDir); err != nil {
			return nil, fmt.Errorf("writing rewritten modules manifest: %w", err)
		}
		modulesDir = tmpDir
	}

	loader, err := configload.NewLoader(&configload.Config{
		ModulesDir: modulesDir,
	})
	if err != nil {
		return nil, fmt.Errorf("creating config loader for %s: %w", tfDir, err)
	}

	config, diags := loader.LoadConfig(tfDir, configs.RootModuleCallForTesting())
	if diags.HasErrors() {
		return nil, fmt.Errorf("loading config from %s: %s", tfDir, diags.Error())
	}
	return config, nil
}

// LoadRawState loads a raw .tfstate file into a *states.State.
func LoadRawState(path string) (*states.State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading state file %s: %w", path, err)
	}

	sf, err := statefile.Read(bytes.NewReader(data), encryption.StateEncryptionDisabled())
	if err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", path, err)
	}
	return sf.State, nil
}

// LoadProviders discovers provider plugins from the .terraform/providers/
// directory and returns a map of provider factories suitable for use with
// tofu.ContextOpts.
func LoadProviders(config *configs.Config, tfDir string) (map[addrs.Provider]providers.Factory, error) {
	providerDir := providercache.NewDir(filepath.Join(tfDir, ".terraform", "providers"))
	allProviders := providerDir.AllAvailablePackages()

	factories := make(map[addrs.Provider]providers.Factory, len(allProviders))
	for provAddr, cachedList := range allProviders {
		if len(cachedList) == 0 {
			continue
		}
		// Use the first (highest precedence) cached provider.
		// Capture loop variable for closure.
		cached := cachedList[0]
		factories[provAddr] = func() (providers.Interface, error) {
			execFile, err := cached.ExecutableFile()
			if err != nil {
				return nil, fmt.Errorf("getting executable for provider %s: %w", provAddr, err)
			}
			clientConfig := &goplugin.ClientConfig{
				HandshakeConfig:  tfplugin.Handshake,
				Logger:           logging.NewProviderLogger(""),
				AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
				Managed:          true,
				Cmd:              exec.Command(execFile),
				AutoMTLS:         true,
				VersionedPlugins: tfplugin.VersionedPlugins,
			}
			client := goplugin.NewClient(clientConfig)
			rpcClient, err := client.Client()
			if err != nil {
				return nil, fmt.Errorf("connecting to provider %s: %w", provAddr, err)
			}
			raw, err := rpcClient.Dispense(tfplugin.ProviderPluginName)
			if err != nil {
				return nil, fmt.Errorf("dispensing provider %s: %w", provAddr, err)
			}
			p := raw.(*tfplugin.GRPCProvider)
			p.PluginClient = client
			return p, nil
		}
	}
	return factories, nil
}

// Evaluate creates a tofu.Context with providers loaded from the tfDir,
// ready for per-instance Eval() calls. Callers should use
// tofuCtx.Eval(ctx, config, state, moduleAddr, opts) to evaluate expressions
// in specific module instances.
//
// The returned cleanup function must be called when the Context is no longer
// needed to kill provider plugin processes.
func Evaluate(config *configs.Config, state *states.State, tfDir string) (*tofu.Context, func(), error) {
	factories, err := LoadProviders(config, tfDir)
	if err != nil {
		return nil, nil, fmt.Errorf("loading providers for evaluation: %w", err)
	}

	tofuCtx, diags := tofu.NewContext(&tofu.ContextOpts{
		Providers: factories,
	})
	if diags.HasErrors() {
		return nil, nil, fmt.Errorf("creating tofu context: %w", diags.Err())
	}

	cleanup := func() {
		goplugin.CleanupClients()
	}
	return tofuCtx, cleanup, nil
}

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

// Package tfprovider provides functionality to load and interact with Terraform
// providers directly. This is used to call UpgradeResourceState RPC for state
// migration when the state schema version differs from the current provider version.
//
// This package is based on the dynamic provider's loader implementation in the Terraform bridge:
// github.com/pulumi/pulumi-terraform-bridge/dynamic/internal/shim/run/loader.go
package tfprovider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/apparentlymart/go-versions/versions"
	plugin "github.com/hashicorp/go-plugin"
	disco "github.com/hashicorp/terraform-svchost/disco"
	tfaddr "github.com/opentofu/registry-address"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/getproviders"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/logging"
	tfplugin "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/plugin"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/plugin6"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/providercache"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/vendored/opentofu/providers"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// envPluginCache allows users to override where we cache TF providers.
// This uses the same cache location as the dynamic provider so downloads are shared.
// It defaults to `$PULUMI_HOME/dynamic_tf_plugins`.
const envPluginCache = "PULUMI_DYNAMIC_TF_PLUGIN_CACHE_DIR"

// Provider represents a running Terraform provider with access to its gRPC interface.
// You must call Close on any Provider that has been created.
type Provider interface {
	providers.Interface
	io.Closer

	Name() string
	Version() string
}

// LoadProvider loads a Terraform provider by its registry address and version.
// The providerAddr is the provider source address (e.g., "hashicorp/aws" or "registry.terraform.io/hashicorp/aws").
// The version must be an exact version (e.g., "5.0.0").
func LoadProvider(ctx context.Context, providerAddr, version string) (Provider, error) {
	addr, err := tfaddr.ParseProviderSource(providerAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid provider name %q: %w", providerAddr, err)
	}

	v, err := getproviders.ParseVersion(version)
	if err != nil {
		return nil, fmt.Errorf("invalid version %q: %w", version, err)
	}

	return getProviderServer(ctx, addr, v, disco.New())
}

// provider wraps a providers.Interface with metadata.
type provider struct {
	providers.Interface
	name    string
	version string
	close   func() error
}

func (p *provider) Name() string    { return p.name }
func (p *provider) Version() string { return p.version }
func (p *provider) Close() error    { return p.close() }

func getPluginCache() (string, error) {
	if dir := os.Getenv(envPluginCache); dir != "" {
		return dir, nil
	}
	return workspace.GetPulumiPath("dynamic_tf_plugins")
}

func getProviderServer(
	ctx context.Context, addr tfaddr.Provider, version versions.Version,
	registrySource *disco.Disco,
) (Provider, error) {
	cacheDir, err := getPluginCache()
	if err != nil {
		return nil, err
	}

	systemCache := providercache.NewDir(cacheDir)

	// Check the cache first
	if p := systemCache.ProviderVersion(addr, version); p != nil {
		slog.InfoContext(ctx, "Found cached provider",
			slog.Any("addr", addr.String()),
			slog.Any("version", version.String()))
		return runProvider(p)
	}

	// Download the provider
	source := getproviders.NewRegistrySource(registrySource)

	meta, err := source.PackageMeta(ctx, addr, version, getproviders.CurrentPlatform)
	if err != nil {
		return nil, fmt.Errorf("failed to get package metadata for %s %s: %w", addr, version, err)
	}

	slog.InfoContext(ctx, "Downloading provider",
		slog.Any("addr", addr.String()),
		slog.Any("version", version.String()))

	_, err = systemCache.InstallPackage(ctx, meta, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to install provider %s %s: %w", addr, version, err)
	}

	p := systemCache.ProviderVersion(addr, version)
	contract.Assertf(p != nil, "We just downloaded (%s,%s) so it should be in the cache", addr, version)

	return runProvider(p)
}

// includePanic is a gRPC interceptor that includes plugin panic messages in errors.
func includePanic(
	ctx context.Context, method string,
	req, reply any,
	cc *grpc.ClientConn, invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	err := invoker(ctx, method, req, reply, cc, opts...)
	if status.Code(err) != codes.Unavailable {
		return err
	}

	panics := logging.PluginPanics()
	if len(panics) == 0 {
		return err
	}

	return fmt.Errorf("%w:\n%s", err, strings.Join(panics, "\n"))
}

// runProvider starts a provider binary and returns a Provider interface.
func runProvider(meta *providercache.CachedProvider) (Provider, error) {
	execFile, err := meta.ExecutableFile()
	if err != nil {
		return nil, err
	}

	config := &plugin.ClientConfig{
		HandshakeConfig:  tfplugin.Handshake,
		Logger:           logging.NewProviderLogger(""),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Managed:          true,
		// Use context.Background so the provider lifetime can escape the calling context
		Cmd:              exec.CommandContext(context.Background(), execFile),
		AutoMTLS:         true,
		VersionedPlugins: tfplugin.VersionedPlugins,
		SyncStdout:       logging.PluginOutputMonitor(fmt.Sprintf("%s:stdout", meta.Provider)),
		SyncStderr:       logging.PluginOutputMonitor(fmt.Sprintf("%s:stderr", meta.Provider)),
		GRPCDialOptions: []grpc.DialOption{
			grpc.WithUnaryInterceptor(includePanic),
		},
	}

	client := plugin.NewClient(config)
	rpcClient, err := client.Client()
	if err != nil {
		return nil, fmt.Errorf("failed to create RPC client: %w", err)
	}

	raw, err := rpcClient.Dispense(tfplugin.ProviderPluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to dispense provider: %w", err)
	}

	switch client.NegotiatedVersion() {
	case 5:
		p := raw.(*tfplugin.GRPCProvider)
		p.PluginClient = client
		p.Addr = meta.Provider
		return &provider{
			Interface: p,
			name:      meta.Provider.Type,
			version:   meta.Version.String(),
			close:     rpcClient.Close,
		}, nil
	case 6:
		p := raw.(*plugin6.GRPCProvider)
		p.PluginClient = client
		p.Addr = meta.Provider
		return &provider{
			Interface: p,
			name:      meta.Provider.Type,
			version:   meta.Version.String(),
			close:     rpcClient.Close,
		}, nil
	default:
		rpcClient.Close()
		return nil, fmt.Errorf("unsupported provider protocol version: %d", client.NegotiatedVersion())
	}
}

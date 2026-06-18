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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
)

// SecretMapping maps a Pulumi config key to a Terraform state resource attribute.
// Format: "configKey=terraformAddress:attribute"
type SecretMapping struct {
	ConfigKey        string
	TerraformAddress string
	Attribute        string
}

// ParseSecretMapping parses a mapping string of the form "configKey=terraformAddress:attribute".
func ParseSecretMapping(s string) (SecretMapping, error) {
	eqIdx := strings.Index(s, "=")
	if eqIdx < 0 {
		return SecretMapping{}, fmt.Errorf("invalid mapping %q: expected format configKey=terraformAddress:attribute", s)
	}

	configKey := s[:eqIdx]
	rest := s[eqIdx+1:]

	// Split on the last ":" to separate address from attribute,
	// since terraform addresses can contain ":" in rare cases.
	colonIdx := strings.LastIndex(rest, ":")
	if colonIdx < 0 {
		return SecretMapping{}, fmt.Errorf("invalid mapping %q: expected format configKey=terraformAddress:attribute", s)
	}

	return SecretMapping{
		ConfigKey:        configKey,
		TerraformAddress: rest[:colonIdx],
		Attribute:        rest[colonIdx+1:],
	}, nil
}

// SetSecrets reads secret values from a Terraform state file and sets them
// as encrypted secrets in a Pulumi stack config.
//
// It initializes the stack if it doesn't exist.
func SetSecrets(stateFilePath, projectDir, projectName, stack, runtime string, mappings []SecretMapping) error {
	// Read and parse the state file.
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return fmt.Errorf("reading state file: %w", err)
	}

	var stateFile struct {
		Resources []struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Module    string `json:"module"`
			Mode      string `json:"mode"`
			Instances []struct {
				IndexKey   interface{}            `json:"index_key"`
				Attributes map[string]interface{} `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(data, &stateFile); err != nil {
		return fmt.Errorf("parsing state file: %w", err)
	}

	// Build a lookup map: terraform address -> attributes.
	// Addresses look like "aws_s3_bucket.example" or
	// "module.foo.aws_ssm_parameter.bar[\"key\"]"
	attrsByAddress := make(map[string]map[string]interface{})
	for _, res := range stateFile.Resources {
		for _, inst := range res.Instances {
			// Build the full address.
			addr := ""
			if res.Module != "" {
				addr = res.Module + "."
			}
			if res.Mode == "data" {
				addr += "data."
			}
			addr += res.Type + "." + res.Name
			if inst.IndexKey != nil {
				switch key := inst.IndexKey.(type) {
				case string:
					addr += fmt.Sprintf("[%q]", key)
				case float64:
					addr += fmt.Sprintf("[%d]", int(key))
				}
			}
			attrsByAddress[addr] = inst.Attributes
		}
	}

	// Ensure a Pulumi project exists before stack operations.
	if err := ensurePulumiProject(projectDir, projectName, runtime); err != nil {
		return err
	}

	// Extract secret values and build config map.
	configMap := make(auto.ConfigMap, len(mappings))
	for _, m := range mappings {
		attrs, ok := attrsByAddress[m.TerraformAddress]
		if !ok {
			return fmt.Errorf("terraform address %q not found in state", m.TerraformAddress)
		}

		value, ok := attrs[m.Attribute]
		if !ok {
			return fmt.Errorf("attribute %q not found on resource %q", m.Attribute, m.TerraformAddress)
		}

		fmt.Fprintf(os.Stderr, "  Mapping secret %s from %s:%s\n", m.ConfigKey, m.TerraformAddress, m.Attribute)
		configMap[m.ConfigKey] = auto.ConfigValue{Value: fmt.Sprintf("%v", value), Secret: true}
	}

	if err := writeConfigValues(projectDir, stack, configMap); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Set %d secrets on stack %s\n", len(mappings), stack)
	return nil
}

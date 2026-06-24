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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pulumi/opentofu/addrs"
	"github.com/pulumi/opentofu/configs"
	"github.com/pulumi/opentofu/states"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/zclconf/go-cty/cty"
)

// ModuleMap is the top-level structure for the module-map.json sidecar file.
type ModuleMap struct {
	Modules       map[string]*ModuleMapEntry `json:"modules"`
	RootResources []ModuleResource           `json:"rootResources,omitempty"`
	Providers     map[string]string          `json:"providers,omitempty"`
}

// ModuleResource represents a single resource within a module instance.
type ModuleResource struct {
	Mode             string                 `json:"mode"` // "managed" or "data"
	TranslatedURN    string                 `json:"translatedUrn"`
	TerraformAddress string                 `json:"terraformAddress"`
	ImportID         string                 `json:"importId"`
	Attributes       map[string]interface{} `json:"attributes,omitempty"`
}

// ModuleMapEntry represents a single module instance in the module map.
type ModuleMapEntry struct {
	TerraformPath string                     `json:"terraformPath"`
	Source        string                     `json:"source,omitempty"`
	IndexKey      string                     `json:"indexKey,omitempty"`
	IndexType     string                     `json:"indexType,omitempty"`
	Resources     []ModuleResource           `json:"resources"`
	Interface     *ModuleInterface           `json:"interface,omitempty"`
	Modules       map[string]*ModuleMapEntry `json:"modules,omitempty"`
}

// ModuleInterface describes the inputs and outputs of a module.
type ModuleInterface struct {
	Inputs  []ModuleInterfaceField `json:"inputs"`
	Outputs []ModuleInterfaceField `json:"outputs"`
}

// ModuleInterfaceField describes a single input variable or output value.
type ModuleInterfaceField struct {
	Name           string      `json:"name"`
	Type           interface{} `json:"type,omitempty"`
	Required       bool        `json:"required,omitempty"`
	Default        interface{} `json:"default,omitempty"`
	Description    string      `json:"description,omitempty"`
	Expression     string      `json:"expression,omitempty"`
	EvaluatedValue interface{} `json:"evaluatedValue,omitempty"`
}

// BuildModuleMap constructs a ModuleMap from Terraform configuration and state.
// tofuCtx and state may be nil if evaluation is not available.
// pulumiProviders may be nil if URN generation should fall back to raw addresses.
func BuildModuleMap(
	config *configs.Config,
	evalScopes *EvalScopes,
	state *states.State,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName string,
	projectName string,
) (*ModuleMap, error) {
	mm := &ModuleMap{
		Modules: make(map[string]*ModuleMapEntry),
	}

	// Store provider registry addresses for downstream consumers (e.g., patch-state).
	if pulumiProviders != nil {
		mm.Providers = make(map[string]string, len(pulumiProviders))
		for tfAddr := range pulumiProviders {
			mm.Providers[string(tfAddr)] = ""
		}
	}

	fmt.Fprintf(os.Stderr, "  Building module entries...\n")
	err := buildModuleMapLevel(mm.Modules, config, evalScopes, state, pulumiProviders, stackName, projectName, nil) //nolint:lll
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "  Found %d module entries\n", len(mm.Modules))

	// Collect root-level resources (empty segments = root module).
	fmt.Fprintf(os.Stderr, "  Matching root-level resources...\n")
	rootResources := matchResources(state, nil, pulumiProviders, stackName, projectName)
	if len(rootResources) > 0 {
		mm.RootResources = rootResources
	}
	fmt.Fprintf(os.Stderr, "  Found %d root resources\n", len(rootResources))

	return mm, nil
}

// buildModuleMapLevel processes one level of module calls and recurses into children.
// parentSegments tracks the module path prefix for nested modules.
func buildModuleMapLevel(
	target map[string]*ModuleMapEntry,
	config *configs.Config,
	evalScopes *EvalScopes,
	state *states.State,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName string,
	projectName string,
	parentSegments []moduleSegment,
) error {
	if config == nil || config.Module == nil {
		return nil
	}

	for name, call := range config.Module.ModuleCalls {
		// Discover instances of this module from state.
		instances := discoverModuleInstances(state, parentSegments, name)
		fmt.Fprintf(os.Stderr, "    module %s: %d instance(s)\n", name, len(instances))

		// Get call-site expression text for each attribute.
		callExpressions := getCallExpressions(call)

		for _, inst := range instances {
			segments := make([]moduleSegment, len(parentSegments)+1)
			copy(segments, parentSegments)
			segments[len(parentSegments)] = moduleSegment{name: name, key: inst.key}

			mapKey := name
			if inst.key != "" {
				mapKey = name + "[" + formatKey(inst.key) + "]"
			}

			fmt.Fprintf(os.Stderr, "      %s: matching resources...\n", mapKey)
			entry := &ModuleMapEntry{
				TerraformPath: buildModulePath(segments),
				Source:        call.SourceAddrRaw,
				IndexKey:      inst.key,
				Resources:     matchResources(state, segments, pulumiProviders, stackName, projectName),
			}
			fmt.Fprintf(os.Stderr, "      %s: %d resources\n", mapKey, len(entry.Resources))

			// Determine index type.
			if inst.key != "" {
				if _, err := fmt.Sscanf(inst.key, "%d", new(int)); err == nil {
					entry.IndexType = "int"
				} else {
					entry.IndexType = "string"
				}
			}

			// Build interface from child config.
			childConfig := config.Children[name]
			if childConfig != nil && childConfig.Module != nil {
				fmt.Fprintf(os.Stderr, "      %s: building interface...\n", mapKey)
				entry.Interface = buildModuleInterface(childConfig, callExpressions)

				// If eval is available, populate evaluatedValue for inputs.
				if evalScopes != nil {
					fmt.Fprintf(os.Stderr, "      %s: evaluating expressions...\n", mapKey)
					populateEvaluatedValues(entry.Interface, evalScopes, segments)
				}
			}

			// Recurse into nested modules.
			if childConfig != nil && len(childConfig.Module.ModuleCalls) > 0 {
				entry.Modules = make(map[string]*ModuleMapEntry)
				err := buildModuleMapLevel(
					entry.Modules, childConfig, evalScopes, state,
					pulumiProviders, stackName, projectName, segments,
				)
				if err != nil {
					return err
				}
				if len(entry.Modules) == 0 {
					entry.Modules = nil
				}
			}

			target[mapKey] = entry
		}
	}

	return nil
}

// moduleInstance represents a discovered module instance from state.
type moduleInstance struct {
	key string // empty for non-indexed, "0"/"1" for count, "key" for for_each
}

// discoverModuleInstances finds unique module instances from raw state that match
// the given parent path and module name.
func discoverModuleInstances(state *states.State, parentSegments []moduleSegment, moduleName string) []moduleInstance {
	seen := map[string]bool{}
	var instances []moduleInstance

	parentDepth := len(parentSegments)

	if state != nil {
		for _, module := range state.Modules {
			segments := moduleSegmentsFromAddr(module.Addr)
			if len(segments) <= parentDepth {
				continue
			}

			// Check that parent path matches.
			match := true
			for i, ps := range parentSegments {
				if segments[i].name != ps.name || segments[i].key != ps.key {
					match = false
					break
				}
			}
			if !match {
				continue
			}

			// Check that the next segment matches our module name.
			seg := segments[parentDepth]
			if seg.name != moduleName {
				continue
			}

			instanceKey := seg.key
			if !seen[instanceKey] {
				seen[instanceKey] = true
				instances = append(instances, moduleInstance{key: instanceKey})
			}
		}
	}

	// Sort instances for deterministic output.
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].key < instances[j].key
	})

	// If no instances found in state, still emit one entry for non-indexed modules
	// if they exist in config (they might just have no resources).
	if len(instances) == 0 {
		instances = append(instances, moduleInstance{key: ""})
	}

	return instances
}

// matchResources finds resources in raw state that belong to the given module instance
// and returns ModuleResource entries with URN, Terraform address, and import ID.
func matchResources(
	state *states.State,
	segments []moduleSegment,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName string,
	projectName string,
) []ModuleResource {
	var resources []ModuleResource
	modulePath := buildModulePath(segments)

	if state != nil {
		for _, module := range state.Modules {
			modSegments := moduleSegmentsFromAddr(module.Addr)
			if buildModulePath(modSegments) != modulePath {
				continue
			}

			for _, res := range module.Resources {
				providerName := res.ProviderConfig.Provider.String()
				resourceType := res.Addr.Resource.Type

				for instKey, inst := range res.Instances {
					if inst.Current == nil {
						continue
					}

					// Build the full address: module path + resource address + instance key
					address := res.Addr.Resource.String()
					if instKey != nil {
						address += instKey.String()
					}
					if len(module.Addr) > 0 {
						address = module.Addr.String() + "." + address
					}

					// Parse attributes from AttrsJSON.
					var attrs map[string]interface{}
					importID := ""
					if inst.Current.AttrsJSON != nil {
						if err := json.Unmarshal(inst.Current.AttrsJSON, &attrs); err == nil {
							if id, ok := attrs["id"]; ok {
								importID = fmt.Sprintf("%v", id)
							}
						}
					}

					// Determine mode string.
					mode := "managed"
					if res.Addr.Resource.Mode == addrs.DataResourceMode {
						mode = "data"
					}

					// Data sources don't map to Pulumi resources.
					urn := ""
					if mode == "managed" {
						urn = buildResourceURN(address, providerName, resourceType, pulumiProviders, stackName, projectName)
					}

					mr := ModuleResource{
						Mode:             mode,
						TranslatedURN:    urn,
						TerraformAddress: address,
						ImportID:         importID,
					}

					// Include attributes, redacting sensitive paths from state.
					if attrs != nil {
						redactSensitivePaths(attrs, inst.Current.AttrSensitivePaths)
						mr.Attributes = attrs
					}

					resources = append(resources, mr)
				}
			}
		}
	}

	if resources == nil {
		resources = []ModuleResource{}
	}
	return resources
}

// buildResourceURN constructs a Pulumi URN for a Terraform resource, or falls back
// to the raw Terraform address if provider mapping is unavailable.
func buildResourceURN(
	address string,
	providerName string,
	resourceType string,
	pulumiProviders map[providermap.TerraformProviderName]*ProviderWithMetadata,
	stackName string,
	projectName string,
) string {
	if pulumiProviders == nil {
		return address
	}

	prov, ok := pulumiProviders[providermap.TerraformProviderName(providerName)]
	if !ok {
		return address
	}

	typeToken, err := bridge.PulumiTypeToken(resourceType, prov.Provider)
	if err != nil {
		return address
	}

	pulumiName := PulumiNameFromTerraformAddress(address, resourceType)
	return fmt.Sprintf("urn:pulumi:%s::%s::%s::%s", stackName, projectName, typeToken, pulumiName)
}

// getCallExpressions extracts the raw HCL expression text for each attribute
// in a module call's config body.
func getCallExpressions(call *configs.ModuleCall) map[string]string {
	result := make(map[string]string)
	if call.Config == nil {
		return result
	}

	attrs, _ := call.Config.JustAttributes()
	for attrName, attr := range attrs {
		rng := attr.Expr.Range()
		src, err := os.ReadFile(rng.Filename)
		if err != nil {
			continue
		}
		startByte := rng.Start.Byte
		endByte := rng.End.Byte
		if startByte >= 0 && endByte <= len(src) && startByte < endByte {
			result[attrName] = string(src[startByte:endByte])
		}
	}

	return result
}

// buildModuleInterface constructs a ModuleInterface from a child config's
// variables and outputs.
func buildModuleInterface(childConfig *configs.Config, callExpressions map[string]string) *ModuleInterface {
	iface := &ModuleInterface{}

	// Build inputs from variables.
	varNames := make([]string, 0, len(childConfig.Module.Variables))
	for name := range childConfig.Module.Variables {
		varNames = append(varNames, name)
	}
	sort.Strings(varNames)

	for _, varName := range varNames {
		v := childConfig.Module.Variables[varName]
		field := ModuleInterfaceField{
			Name:        varName,
			Description: v.Description,
		}

		// Type: convert cty.Type to a string representation.
		if v.Type != cty.NilType {
			field.Type = v.Type.FriendlyName()
		}

		// Required: a variable is required if it has no default value.
		if v.Default == cty.NilVal {
			field.Required = true
		} else {
			field.Default = ctyValueToInterface(v.Default)
		}

		// Expression from call site.
		if expr, ok := callExpressions[varName]; ok {
			field.Expression = expr
		}

		iface.Inputs = append(iface.Inputs, field)
	}

	// Build outputs.
	outputNames := make([]string, 0, len(childConfig.Module.Outputs))
	for name := range childConfig.Module.Outputs {
		outputNames = append(outputNames, name)
	}
	sort.Strings(outputNames)

	for _, outName := range outputNames {
		o := childConfig.Module.Outputs[outName]
		field := ModuleInterfaceField{
			Name:        outName,
			Description: o.Description,
		}
		iface.Outputs = append(iface.Outputs, field)
	}

	return iface
}

// populateEvaluatedValues uses pre-built EvalScopes to evaluate variable values
// in a specific module instance and populates the EvaluatedValue field.
func populateEvaluatedValues(
	iface *ModuleInterface,
	evalScopes *EvalScopes,
	segments []moduleSegment,
) {
	// Build the module instance address from segments.
	addr := addrs.RootModuleInstance
	for _, seg := range segments {
		if seg.key == "" {
			addr = addr.Child(seg.name, addrs.NoKey)
		} else if _, err := fmt.Sscanf(seg.key, "%d", new(int)); err == nil {
			var idx int
			fmt.Sscanf(seg.key, "%d", &idx)
			addr = addr.Child(seg.name, addrs.IntKey(idx))
		} else {
			addr = addr.Child(seg.name, addrs.StringKey(seg.key))
		}
	}

	scope := evalScopes.Scope(addr)
	if scope == nil {
		return
	}

	for i, input := range iface.Inputs {
		expr, parseDiags := hclsyntax.ParseExpression(
			[]byte("var."+input.Name), "<eval>", hcl.Pos{Line: 1, Column: 1},
		)
		if parseDiags.HasErrors() {
			continue
		}

		val, evalDiags := scope.EvalExpr(expr, cty.DynamicPseudoType)
		if evalDiags.HasErrors() {
			continue
		}

		iface.Inputs[i].EvaluatedValue = ctyValueToInterface(val)
	}
}

// ConfigEntry represents a config value to be set on a Pulumi stack.
// When Secret is true, the value is encrypted in the stack config.
type ConfigEntry struct {
	ConfigKey string
	Value     string
	Secret    bool
}

// SensitiveSecret is an alias for backwards compatibility.
type SensitiveSecret = ConfigEntry

// DiscoverSensitiveSecrets walks the state and collects all sensitive attribute
// values, returning them as config key / value pairs. The config key is derived
// by flattening the terraform address and attribute name.
//
// projectName is used for length checking: Pulumi config keys are limited to
// 128 chars total including the "project:" namespace prefix.
//
// After collecting all secrets, this function:
//  1. Deduplicates keys by appending _2, _3, etc. and warns to stderr
//  2. Checks key lengths and returns an error if any exceed the limit
func DiscoverSensitiveSecrets(state *states.State, projectName string) ([]SensitiveSecret, error) {
	if state == nil {
		return nil, nil
	}

	type rawSecret struct {
		address   string
		attribute string
		value     string
	}

	var raw []rawSecret
	for _, module := range state.Modules {
		for _, res := range module.Resources {
			for instKey, inst := range res.Instances {
				if inst.Current == nil || len(inst.Current.AttrSensitivePaths) == 0 {
					continue
				}

				// Build the full address.
				address := res.Addr.Resource.String()
				if instKey != nil {
					address += instKey.String()
				}
				if len(module.Addr) > 0 {
					address = module.Addr.String() + "." + address
				}

				// Parse attributes.
				var attrs map[string]interface{}
				if inst.Current.AttrsJSON == nil {
					continue
				}
				if err := json.Unmarshal(inst.Current.AttrsJSON, &attrs); err != nil {
					continue
				}

				// Collect sensitive top-level attributes.
				for _, pvm := range inst.Current.AttrSensitivePaths {
					if len(pvm.Path) != 1 {
						continue
					}
					step, ok := pvm.Path[0].(cty.GetAttrStep)
					if !ok {
						continue
					}
					value, exists := attrs[step.Name]
					if !exists || value == nil {
						continue
					}
					raw = append(raw, rawSecret{
						address:   address,
						attribute: step.Name,
						value:     fmt.Sprintf("%v", value),
					})
				}
			}
		}
	}

	// Generate keys and handle dedup + length checking.
	maxKeyLen := 128 - len(projectName) - 1 // subtract "project:" namespace
	keyCounts := make(map[string]int)
	keyToAddress := make(map[string]string) // first address that produced each key

	var secrets []SensitiveSecret
	var tooLong []string

	for _, r := range raw {
		key := flattenAddress(r.address, r.attribute)
		keyCounts[key]++
		count := keyCounts[key]

		if count == 1 {
			keyToAddress[key] = r.address
		}

		finalKey := key
		if count > 1 {
			finalKey = fmt.Sprintf("%s_%d", key, count)
			fmt.Fprintf(os.Stderr, "  WARNING: duplicate config key %q from:\n    1: %s\n    %d: %s\n",
				key, keyToAddress[key], count, r.address)
		}

		if len(finalKey) > maxKeyLen {
			tooLong = append(tooLong, fmt.Sprintf(
				"key %q (%d chars, max %d) from %s",
				finalKey, len(finalKey), maxKeyLen, r.address))
		}

		secrets = append(secrets, ConfigEntry{
			ConfigKey: finalKey,
			Value:     r.value,
			Secret:    true,
		})
	}

	if len(tooLong) > 0 {
		for _, msg := range tooLong {
			fmt.Fprintf(os.Stderr, "  ERROR: config key too long: %s\n", msg)
		}
		return secrets, fmt.Errorf("%d config key(s) exceed the 128-char Pulumi limit (including %q namespace)", len(tooLong), projectName+":")
	}

	return secrets, nil
}

// flattenAddress converts a terraform address + attribute into a concise Pulumi config key.
//
// Terraform addresses like:
//
//	module.capture_secrets["dmvhm-capture-service-develop"].aws_secretsmanager_secret_version.this["dmvhm-capture-service-develop/cap_client_oauth"]
//
// are shortened by:
//  1. Stripping all "module." prefixes
//  2. Stripping resource types (e.g. aws_secretsmanager_secret_version)
//  3. Stripping generic resource names like "this", "ssm_parameters"
//  4. Deduplicating for_each keys that repeat between module and resource levels
//
// The result is a human-readable key like "capture_secrets_cap_client_oauth_secret_string".
func flattenAddress(address, attribute string) string {
	clean := strings.NewReplacer(
		"\"", "",
		" ", "_",
	)
	address = clean.Replace(address)

	// Generic resource names that add no value.
	genericNames := map[string]bool{
		"this":           true,
		"ssm_parameters": true,
	}

	// Parse the address into module segments and a resource tail.
	// Address forms:
	//   module.A[k1].module.B[k2].resource_type.name[k3]   (nested modules)
	//   module.A[k1].resource_type.name[k3]                 (single module)
	//   resource_type.name[k3]                              (root resource)
	type segment struct {
		name string
		key  string
	}
	var moduleSegments []segment
	var resourceName, resourceKey string

	// Split into dot-separated parts, handling brackets.
	remaining := address
	var parts []string
	for remaining != "" {
		// Find next dot that isn't inside brackets.
		depth := 0
		dotIdx := -1
		for i, c := range remaining {
			switch c {
			case '[':
				depth++
			case ']':
				depth--
			case '.':
				if depth == 0 {
					dotIdx = i
				}
			}
			if dotIdx >= 0 {
				break
			}
		}
		if dotIdx >= 0 {
			parts = append(parts, remaining[:dotIdx])
			remaining = remaining[dotIdx+1:]
		} else {
			parts = append(parts, remaining)
			remaining = ""
		}
	}

	// Walk parts: "module" keywords indicate module segments; the last two non-module
	// parts are resource_type and resource_name.
	i := 0
	for i < len(parts) {
		if parts[i] == "module" && i+1 < len(parts) {
			name, key := splitForEachKey(parts[i+1])
			moduleSegments = append(moduleSegments, segment{name: name, key: key})
			i += 2
		} else {
			break
		}
	}

	// Remaining parts: resource_type[.resource_name[key]]
	// parts[i] = resource type (discarded), parts[i+1] = resource name + optional key
	if i+1 < len(parts) {
		// Skip resource type at parts[i]
		resourceName, resourceKey = splitForEachKey(parts[i+1])
	} else if i < len(parts) {
		// Only resource type, no separate name (rare)
		resourceName, _ = splitForEachKey(parts[i])
	}

	// Build key from meaningful segments.
	var keyParts []string

	// Collect all sanitized module keys for dedup.
	var allModuleKeys []string
	for _, ms := range moduleSegments {
		keyParts = append(keyParts, ms.name)
		if ms.key != "" {
			sanitized := sanitizeSegment(ms.key)
			keyParts = append(keyParts, sanitized)
			allModuleKeys = append(allModuleKeys, sanitized)
		}
	}

	// Include resource name only if it's not generic.
	if resourceName != "" && !genericNames[resourceName] {
		keyParts = append(keyParts, resourceName)
	}

	// Include resource key, deduplicating against module keys.
	if resourceKey != "" {
		sanitized := sanitizeSegment(resourceKey)
		// Try to strip redundant prefixes from module keys.
		for _, mk := range allModuleKeys {
			if sanitized == mk {
				sanitized = ""
				break
			}
			if strings.HasPrefix(sanitized, mk+"_") {
				sanitized = sanitized[len(mk)+1:]
				break
			}
		}
		if sanitized != "" {
			keyParts = append(keyParts, sanitized)
		}
	}

	keyParts = append(keyParts, attribute)
	return strings.Join(keyParts, "_")
}

// splitForEachKey splits "name[key]" into ("name", "key") or ("name", "") if no key.
func splitForEachKey(s string) (string, string) {
	if idx := strings.Index(s, "["); idx >= 0 {
		key := strings.TrimRight(s[idx+1:], "]")
		return s[:idx], key
	}
	return s, ""
}

// sanitizeSegment replaces non-alphanumeric chars with underscores and collapses runs.
func sanitizeSegment(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// SetSecretsFromState writes config entries to Pulumi stack config using the automation API.
// Entries with Secret=true are encrypted; others are set as plain config.
// Secret values are never printed or logged.
func SetSecretsFromState(entries []ConfigEntry, projectDir, projectName, stack, runtime string) error {
	// Ensure a Pulumi project exists before stack operations.
	if err := ensurePulumiProject(projectDir, projectName, runtime); err != nil {
		return err
	}

	configMap := make(auto.ConfigMap, len(entries))
	for _, e := range entries {
		configMap[e.ConfigKey] = auto.ConfigValue{Value: e.Value, Secret: e.Secret}
	}

	if err := writeConfigValues(projectDir, stack, configMap); err != nil {
		return err
	}

	var secretCount, plainCount int
	for _, e := range entries {
		if e.Secret {
			secretCount++
		} else {
			plainCount++
		}
	}
	fmt.Fprintf(os.Stderr, "Set %d secrets and %d plain config values on stack %s\n", secretCount, plainCount, stack)
	return nil
}

// writeConfigValues creates a local workspace, ensures the stack exists, and writes config values.
// It sets each key individually to avoid overwriting existing config.
func writeConfigValues(projectDir, stack string, configMap auto.ConfigMap) error {
	ctx := context.Background()
	ws, err := auto.NewLocalWorkspace(ctx, auto.WorkDir(projectDir))
	if err != nil {
		return fmt.Errorf("creating workspace: %w", err)
	}

	// Create the stack if it doesn't already exist.
	fmt.Fprintf(os.Stderr, "Ensuring stack %s exists...\n", stack)
	if err := ws.CreateStack(ctx, stack); err != nil && !auto.IsCreateStack409Error(err) {
		return fmt.Errorf("creating stack %s: %w", stack, err)
	}

	for key, val := range configMap {
		if err := ws.SetConfig(ctx, stack, key, val); err != nil {
			return fmt.Errorf("setting config key %q: %w", key, err)
		}
	}
	return nil
}

// redactSensitivePaths replaces sensitive attribute values with "(sensitive)" based on
// the AttrSensitivePaths from the state. This uses the sensitivity information that
// Terraform/OpenTofu persists in the state file itself, which tracks sensitivity from
// both provider schemas and sensitive variable propagation.
func redactSensitivePaths(attrs map[string]interface{}, paths []cty.PathValueMarks) {
	for _, pvm := range paths {
		if len(pvm.Path) == 0 {
			continue
		}
		// For top-level attributes, the first step is a GetAttrStep.
		step, ok := pvm.Path[0].(cty.GetAttrStep)
		if !ok {
			continue
		}
		if _, exists := attrs[step.Name]; exists {
			if len(pvm.Path) == 1 {
				// Top-level attribute is sensitive — redact it.
				attrs[step.Name] = "(sensitive)"
			}
			// Nested paths: we could recurse, but for now top-level is sufficient.
		}
	}
}

// ctyValueToInterface converts a cty.Value to a plain Go value suitable for JSON serialization.
// Values marked as sensitive (via cty marks from OpenTofu's sensitivity tracking) are redacted.
func ctyValueToInterface(v cty.Value) interface{} {
	if v == cty.NilVal || !v.IsKnown() {
		return nil
	}

	// Strip marks (sensitivity, etc.). If the value is marked sensitive, redact it.
	if v.IsMarked() {
		unmarked, marks := v.Unmark()
		for mark := range marks {
			if mark == "sensitive" {
				return "(sensitive)"
			}
		}
		v = unmarked
	}

	if v.IsNull() {
		return nil
	}

	ty := v.Type()

	switch {
	case ty == cty.String:
		return v.AsString()
	case ty == cty.Number:
		bf := v.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case ty == cty.Bool:
		return v.True()
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		var result []interface{}
		for it := v.ElementIterator(); it.Next(); {
			_, elem := it.Element()
			result = append(result, ctyValueToInterface(elem))
		}
		return result
	case ty.IsMapType() || ty.IsObjectType():
		result := make(map[string]interface{})
		for it := v.ElementIterator(); it.Next(); {
			key, elem := it.Element()
			result[key.AsString()] = ctyValueToInterface(elem)
		}
		return result
	default:
		return nil
	}
}

// WriteModuleMap serializes a ModuleMap to JSON and writes it to the given path.
func WriteModuleMap(mm *ModuleMap, path string) error {
	data, err := json.MarshalIndent(mm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling module map: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing module map to %s: %w", path, err)
	}
	return nil
}

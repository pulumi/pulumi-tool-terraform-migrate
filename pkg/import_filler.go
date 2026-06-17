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
	"fmt"
	"strings"
)

// ImportEntry represents a single resource in a Pulumi import file.
type ImportEntry struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	ID        string `json:"id,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Component bool   `json:"component,omitempty"`
	Version   string `json:"version,omitempty"`
}

// ImportFile represents the top-level Pulumi import file structure.
type ImportFile struct {
	NameTable map[string]string `json:"nameTable,omitempty"`
	Resources []ImportEntry     `json:"resources"`
}

// FillResult contains statistics from the fill operation.
type FillResult struct {
	Filled    int
	Skipped   int
	Unmatched int
	Warnings  []string
}

// FillImportFile matches TF resources from a digest to Pulumi import file entries
// and fills in placeholder import IDs. It modifies importFile in place.
//
// moduleMappings maps TF module paths to Pulumi component names (e.g., "module.caas_rds" → "caas_rds").
// resourceMappings maps TF resource addresses to Pulumi resource names (e.g., "aws_s3_bucket.my_bucket" → "my_bucket").
func FillImportFile(digest *ModuleMap, importFile *ImportFile, moduleMappings, resourceMappings map[string]string) *FillResult {
	result := &FillResult{}

	// Build a lookup of all TF resources by address for resource-level mappings.
	tfByAddress := map[string]*ModuleResource{}
	for i := range digest.RootResources {
		r := &digest.RootResources[i]
		if r.Mode == "managed" {
			tfByAddress[r.TerraformAddress] = r
		}
	}
	collectAllResources(digest.Modules, tfByAddress)

	// Build a lookup of import entries by name for resource-level mappings.
	importByName := map[string]*ImportEntry{}
	for i := range importFile.Resources {
		entry := &importFile.Resources[i]
		if !entry.Component {
			importByName[entry.Name] = entry
		}
	}

	// Phase 1: Apply resource-level mappings (direct TF address → Pulumi name).
	for tfAddr, pulumiName := range resourceMappings {
		tfRes, tfOk := tfByAddress[tfAddr]
		entry, importOk := importByName[pulumiName]
		if !tfOk {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("TF resource %q from resource mapping not found in digest", tfAddr))
			continue
		}
		if !importOk {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Pulumi resource %q from resource mapping not found in import file", pulumiName))
			continue
		}
		if entry.ID != "<PLACEHOLDER>" {
			continue // already filled
		}
		entry.ID = tfRes.ImportID
		result.Filled++
	}

	// Phase 2: Group remaining import entries by parent for module-level matching.
	byParent := map[string][]*ImportEntry{}    // parentName -> children
	var orphans []*ImportEntry                  // entries with no parent
	componentNames := map[string]bool{}         // track component entries

	for i := range importFile.Resources {
		entry := &importFile.Resources[i]
		if entry.Component {
			componentNames[entry.Name] = true
			result.Skipped++
			continue
		}
		if entry.ID != "<PLACEHOLDER>" {
			continue // already filled by resource mapping
		}
		if entry.Parent != "" {
			byParent[entry.Parent] = append(byParent[entry.Parent], entry)
		} else {
			orphans = append(orphans, entry)
		}
	}

	// Group TF resources by module path from digest.
	tfByModule := map[string][]ModuleResource{}
	collectModuleResources(digest.Modules, tfByModule)

	// Match components to modules using module mappings.
	for tfModulePath, componentName := range moduleMappings {
		tfResources, tfOk := tfByModule[tfModulePath]
		importEntries, importOk := byParent[componentName]

		if !tfOk {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("TF module %q from mapping not found in digest", tfModulePath))
			continue
		}
		if !importOk {
			// All children may have been filled by resource mappings in Phase 1,
			// or the component has no unfilled children. Only warn if the component
			// name doesn't appear anywhere in the import file.
			if !componentNames[componentName] {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("component %q from mapping not found in import file", componentName))
			}
			continue
		}

		matched, warnings := matchChildren(tfResources, importEntries)
		result.Filled += matched
		result.Warnings = append(result.Warnings, warnings...)

		// Remove matched parent from byParent so it's not double-counted as unmatched.
		delete(byParent, componentName)
	}

	// Handle root resources: match orphaned import entries against digest rootResources.
	if len(orphans) > 0 && len(digest.RootResources) > 0 {
		matched, warnings := matchChildren(digest.RootResources, orphans)
		result.Filled += matched
		result.Warnings = append(result.Warnings, warnings...)
	}

	// Count unmatched: entries still with <PLACEHOLDER> that aren't components.
	for i := range importFile.Resources {
		entry := &importFile.Resources[i]
		if !entry.Component && entry.ID == "<PLACEHOLDER>" {
			result.Unmatched++
		}
	}

	return result
}

// collectAllResources walks the nested module map and indexes all managed resources by TF address.
func collectAllResources(modules map[string]*ModuleMapEntry, out map[string]*ModuleResource) {
	for _, entry := range modules {
		for i := range entry.Resources {
			r := &entry.Resources[i]
			if r.Mode == "managed" {
				out[r.TerraformAddress] = r
			}
		}
		if entry.Modules != nil {
			collectAllResources(entry.Modules, out)
		}
	}
}

// collectModuleResources walks the nested module map and flattens resources by their TF path.
func collectModuleResources(modules map[string]*ModuleMapEntry, out map[string][]ModuleResource) {
	for _, entry := range modules {
		// Only include managed resources (skip data sources).
		var managed []ModuleResource
		for _, r := range entry.Resources {
			if r.Mode == "managed" {
				managed = append(managed, r)
			}
		}
		if len(managed) > 0 {
			out[entry.TerraformPath] = managed
		}
		if entry.Modules != nil {
			collectModuleResources(entry.Modules, out)
		}
	}
}

// matchChildren matches TF resources to import entries within a single group
// by type + resource name. The import entry name follows the convention
// "${componentName}-${tfResourceName}" (set by the component skill), so we
// extract the suffix after the parent prefix and match it against the TF
// resource name (last segment of the terraform address).
//
// Falls back to type-only matching when there's exactly one candidate of a
// given type (for components that predate the naming convention).
func matchChildren(tfResources []ModuleResource, importEntries []*ImportEntry) (matched int, warnings []string) {
	// Index TF resources by type::name key for exact matching.
	type typeNameKey struct{ pulumiType, tfName string }
	byTypeName := map[typeNameKey]*ModuleResource{}
	// Also index by type only for fallback.
	byType := map[string][]ModuleResource{}

	for i := range tfResources {
		r := &tfResources[i]
		pulumiType := extractTypeFromURN(r.TranslatedURN)
		if pulumiType == "" {
			continue
		}
		tfName := extractResourceName(r.TerraformAddress)
		byTypeName[typeNameKey{pulumiType, tfName}] = r
		byType[pulumiType] = append(byType[pulumiType], *r)
	}

	used := map[string]bool{}
	for _, entry := range importEntries {
		if entry.ID != "<PLACEHOLDER>" {
			continue
		}

		// Extract the suffix from the import entry name.
		suffix := extractImportSuffix(entry.Name, entry.Parent)

		// Try exact match by type + name first.
		if suffix != "" {
			key := typeNameKey{entry.Type, suffix}
			if r, ok := byTypeName[key]; ok && !used[r.TerraformAddress] {
				entry.ID = r.ImportID
				used[r.TerraformAddress] = true
				matched++
				continue
			}
		}

		// Fallback: if exactly one unused candidate of this type, use it.
		candidates := unusedOfType(byType, entry.Type, used)
		if len(candidates) == 1 {
			entry.ID = candidates[0].ImportID
			used[candidates[0].TerraformAddress] = true
			matched++
		} else if len(candidates) > 1 {
			warnings = append(warnings,
				fmt.Sprintf("no name match and %d type candidates for %s %q (suffix %q)",
					len(candidates), entry.Type, entry.Name, suffix))
		}
	}
	return matched, warnings
}

// extractTypeFromURN extracts the Pulumi type token from a URN string.
// URN format: urn:pulumi:stack::project::type::name
// If the string is not a valid URN (e.g., a raw TF address fallback), it returns "".
func extractTypeFromURN(urn string) string {
	if !strings.HasPrefix(urn, "urn:pulumi:") {
		return ""
	}
	// Split on "::" — parts: [urn:pulumi:stack, project, type, name]
	parts := strings.SplitN(urn, "::", 4)
	if len(parts) < 4 {
		return ""
	}
	return parts[2]
}

// unusedOfType returns TF resources of the given Pulumi type that haven't been used yet.
func unusedOfType(byType map[string][]ModuleResource, pulumiType string, used map[string]bool) []ModuleResource {
	var result []ModuleResource
	for _, r := range byType[pulumiType] {
		if !used[r.TerraformAddress] {
			result = append(result, r)
		}
	}
	return result
}

// extractResourceName extracts the TF resource name from a terraform address.
// "module.vpc.aws_vpc.main" → "main"
// "module.vpc.aws_subnet.public[0]" → "public_0"
// "aws_s3_bucket.my_bucket" → "my_bucket"
func extractResourceName(address string) string {
	// Split on dots respecting brackets.
	parts := splitAddressParts(address)
	if len(parts) == 0 {
		return ""
	}
	// The resource name is the last part (e.g., ssm_parameters["/develop/dmvhm/cm/api_stage"]).
	// Kept as-is to match Pulumi resource name suffixes directly.
	return parts[len(parts)-1]
}

// extractImportSuffix extracts the resource name suffix from a Pulumi import
// entry name by stripping the parent component name prefix.
// ("caas_rds-aurora_cluster", "caas_rds") → "aurora_cluster"
// ("my-bucket", "") → "my-bucket"
func extractImportSuffix(name, parent string) string {
	if parent == "" {
		return name
	}
	prefix := parent + "-"
	if strings.HasPrefix(name, prefix) {
		return name[len(prefix):]
	}
	return name
}

// normalizeInstanceKey converts TF instance key notation to the format used in
// Pulumi resource names (underscores instead of brackets).
// "public[0]" → "public_0"
// `params["my_key"]` → "params_my_key"
// "main" → "main" (no key)
func normalizeInstanceKey(s string) string {
	idx := strings.Index(s, "[")
	if idx < 0 {
		return s
	}
	base := s[:idx]
	key := s[idx+1 : len(s)-1] // strip [ and ]
	key = strings.Trim(key, `"`)
	return base + "_" + key
}

// TranslateImportIDs translates TF import IDs to Pulumi-expected formats.
// TF and Pulumi often use different import ID formats for the same resource type.
// This function looks up digest attributes to construct the correct Pulumi ID.
func TranslateImportIDs(importFile *ImportFile, digest *ModuleMap) int {
	// Build TF resource lookup by importId.
	tfByID := map[string]*ModuleResource{}
	for i := range digest.RootResources {
		r := &digest.RootResources[i]
		if r.Mode == "managed" && r.ImportID != "" {
			tfByID[r.ImportID] = r
		}
	}
	for _, entry := range digest.Modules {
		for i := range entry.Resources {
			r := &entry.Resources[i]
			if r.Mode == "managed" && r.ImportID != "" {
				tfByID[r.ImportID] = r
			}
		}
	}

	translated := 0
	for i := range importFile.Resources {
		entry := &importFile.Resources[i]
		if entry.Component || entry.ID == "" || entry.ID == "<PLACEHOLDER>" {
			continue
		}

		tf := tfByID[entry.ID]
		if tf == nil {
			continue
		}
		attrs := tf.Attributes

		var newID string
		switch entry.Type {
		case "aws:wafv2/ipSet:IpSet", "aws:wafv2/webAcl:WebAcl":
			// uuid -> id/name/scope
			if name, _ := attrs["name"].(string); name != "" {
				scope, _ := attrs["scope"].(string)
				newID = entry.ID + "/" + name + "/" + scope
			}

		case "aws:ec2/routeTableAssociation:RouteTableAssociation":
			// rtbassoc -> subnet/rtb
			if sid, _ := attrs["subnet_id"].(string); sid != "" {
				rtb, _ := attrs["route_table_id"].(string)
				newID = sid + "/" + rtb
			}

		case "aws:ec2/securityGroupRule:SecurityGroupRule":
			// sgrule -> sg_type_proto_from_to_source
			sg, _ := attrs["security_group_id"].(string)
			t, _ := attrs["type"].(string)
			proto, _ := attrs["protocol"].(string)
			fp := fmt.Sprintf("%v", attrs["from_port"])
			tp := fmt.Sprintf("%v", attrs["to_port"])
			var src string
			if self, _ := attrs["self"].(bool); self {
				src = "self"
			} else if cidrs, ok := attrs["cidr_blocks"].([]interface{}); ok && len(cidrs) > 0 {
				parts := make([]string, len(cidrs))
				for i, c := range cidrs {
					parts[i] = fmt.Sprintf("%v", c)
				}
				src = strings.Join(parts, "_")
			} else {
				src = sg
			}
			newID = sg + "_" + t + "_" + proto + "_" + fp + "_" + tp + "_" + src

		case "aws:appautoscaling/target:Target":
			ns, _ := attrs["service_namespace"].(string)
			rid, _ := attrs["resource_id"].(string)
			dim, _ := attrs["scalable_dimension"].(string)
			newID = ns + "/" + rid + "/" + dim

		case "aws:appautoscaling/policy:Policy":
			ns, _ := attrs["service_namespace"].(string)
			rid, _ := attrs["resource_id"].(string)
			dim, _ := attrs["scalable_dimension"].(string)
			name, _ := attrs["name"].(string)
			newID = ns + "/" + rid + "/" + dim + "/" + name

		case "aws:iam/rolePolicyAttachment:RolePolicyAttachment":
			role, _ := attrs["role"].(string)
			if role == "" {
				if roles, ok := attrs["roles"].([]interface{}); ok && len(roles) > 0 {
					role, _ = roles[0].(string)
				}
			}
			arn, _ := attrs["policy_arn"].(string)
			if role != "" && arn != "" {
				newID = role + "/" + arn
			}

		case "aws:iam/policyAttachment:PolicyAttachment":
			if name, _ := attrs["name"].(string); name != "" {
				newID = name
			}

		case "aws:opensearch/serverlessAccessPolicy:ServerlessAccessPolicy",
			"aws:opensearch/serverlessSecurityPolicy:ServerlessSecurityPolicy":
			name, _ := attrs["name"].(string)
			typ, _ := attrs["type"].(string)
			if name != "" && typ != "" {
				newID = name + "/" + typ
			}

		case "aws:ecs/cluster:Cluster":
			if name, _ := attrs["name"].(string); name != "" {
				newID = name
			}

		case "aws:ecs/service:Service":
			cluster, _ := attrs["cluster"].(string)
			svcName, _ := attrs["name"].(string)
			if cluster != "" && svcName != "" {
				if strings.Contains(cluster, "arn:") {
					parts := strings.Split(cluster, "/")
					cluster = parts[len(parts)-1]
				}
				newID = cluster + "/" + svcName
			}

		case "aws:ecs/taskDefinition:TaskDefinition":
			if arn, _ := attrs["arn"].(string); arn != "" {
				newID = arn
			}

		case "aws:apigatewayv2/apiMapping:ApiMapping":
			if domain, _ := attrs["domain_name"].(string); domain != "" {
				newID = entry.ID + "/" + domain
			}

		case "aws:kinesis/stream:Stream":
			if name, _ := attrs["name"].(string); name != "" {
				newID = name
			}

		case "aws:lambda/permission:Permission":
			fn, _ := attrs["function_name"].(string)
			stmt, _ := attrs["statement_id"].(string)
			if fn != "" && stmt != "" {
				newID = fn + "/" + stmt
			}

		case "aws:s3/bucketObject:BucketObject", "aws:s3/bucketObjectv2:BucketObjectv2":
			bucket, _ := attrs["bucket"].(string)
			key, _ := attrs["key"].(string)
			if bucket != "" && key != "" {
				newID = "s3://" + bucket + "/" + key
			}
		}

		if newID != "" && newID != entry.ID {
			entry.ID = newID
			translated++
		}
	}

	return translated
}

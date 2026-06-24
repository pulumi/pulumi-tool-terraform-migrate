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
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/providermap"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// SchemaFieldInfo describes a single top-level field from the provider schema
// for a Terraform resource type.
type SchemaFieldInfo struct {
	// TFName is the Terraform attribute name (e.g. "force_destroy").
	TFName string
	// PulumiName is the Pulumi property name (e.g. "forceDestroy").
	PulumiName string
	// SchemaDefault is the static default value from the TF schema, if any.
	SchemaDefault any
	// HasDefault is true when the TF schema specifies a non-nil default.
	HasDefault bool
	// IsAsset is true when the bridge overlay marks this field as an asset/archive.
	IsAsset bool
	// AssetKind is the bridge AssetTranslationKind (0=FileAsset, 2=FileArchive).
	AssetKind int
	// ArchiveFormat is the resource.ArchiveFormat (e.g. 3=ZIPArchive).
	ArchiveFormat resource.ArchiveFormat
	// HashField is the companion hash field name for asset translations, if any.
	HashField string
	// IsComputed is true for output-only fields (Computed && !Optional && !Required).
	IsComputed bool
	// IsInput is true for user-settable fields (Required || Optional).
	IsInput bool
}

// BuildPulumiToTFTypeMap builds a reverse mapping from Pulumi type token
// (e.g. "aws:lambda/function:Function") to TF resource type (e.g. "aws_lambda_function")
// across all loaded providers.
func BuildPulumiToTFTypeMap(
	providers map[providermap.TerraformProviderName]*ProviderWithMetadata,
) map[string]string {
	result := make(map[string]string)
	for _, prov := range providers {
		for tfType := range prov.Resources {
			tok, err := bridge.PulumiTypeToken(tfType, prov.Provider)
			if err != nil {
				continue
			}
			result[string(tok)] = tfType
		}
	}
	return result
}

// GetSchemaFieldInfo extracts field metadata for every top-level attribute of a
// TF resource type. The returned map is keyed by TF attribute name.
func GetSchemaFieldInfo(
	prov *ProviderWithMetadata,
	tfResourceType string,
) map[string]*SchemaFieldInfo {
	shimResource := prov.P.ResourcesMap().Get(tfResourceType)
	if shimResource == nil {
		return nil
	}

	resourceInfo := prov.Resources[tfResourceType]
	var fieldInfos map[string]*info.Schema
	if resourceInfo != nil {
		fieldInfos = resourceInfo.Fields
	}

	schemaMap := shimResource.Schema()
	result := make(map[string]*SchemaFieldInfo)

	schemaMap.Range(func(tfName string, schema shim.Schema) bool {
		fi := &SchemaFieldInfo{
			TFName:     tfName,
			PulumiName: tfbridge.TerraformToPulumiNameV2(tfName, schemaMap, fieldInfos),
		}

		// Defaults
		if d := schema.Default(); d != nil {
			fi.HasDefault = true
			fi.SchemaDefault = d
		}

		// Input / Computed classification
		fi.IsInput = schema.Required() || schema.Optional()
		fi.IsComputed = schema.Computed() && !schema.Optional() && !schema.Required()

		// Asset info from bridge overlay
		if fieldInfos != nil {
			if si, ok := fieldInfos[tfName]; ok && si != nil && si.Asset != nil {
				fi.IsAsset = true
				fi.AssetKind = int(si.Asset.Kind)
				fi.ArchiveFormat = si.Asset.Format
				fi.HashField = si.Asset.HashField
			}
		}

		result[tfName] = fi
		return true
	})

	return result
}

// LookupProviderForPulumiType finds the ProviderWithMetadata and TF resource type
// for a given Pulumi type token using the pre-built reverse type map.
func LookupProviderForPulumiType(
	pulumiType string,
	typeMap map[string]string,
	providers map[providermap.TerraformProviderName]*ProviderWithMetadata,
) (*ProviderWithMetadata, string, bool) {
	tfType, ok := typeMap[pulumiType]
	if !ok {
		return nil, "", false
	}

	for _, prov := range providers {
		if _, exists := prov.Resources[tfType]; exists {
			return prov, tfType, true
		}
	}
	return nil, tfType, false
}

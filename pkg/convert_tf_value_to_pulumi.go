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
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/valueshim"
	"github.com/pulumi/pulumi-tool-terraform-migrate/pkg/bridge"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"github.com/zclconf/go-cty/cty"
)

type terraformState struct {
	stateValue cty.Value
	meta       map[string]interface{}
}

var (
	_ tfbridge.TerraformState               = terraformState{}
	_ tfbridge.TerraformStateWithTypedValue = terraformState{}
)

func (t terraformState) Value() valueshim.Value {
	return valueshim.FromCtyValue(t.stateValue)
}

func (t terraformState) Meta() map[string]interface{} {
	return t.meta
}

// copied from https://github.com/pulumi/pulumi-terraform-bridge/blob/main/pkg/tfshim/sdk-v2/provider2.go#L139
func (t terraformState) Object(schemaMap shim.SchemaMap) (map[string]interface{}, error) {
	res, err := bridge.ObjectFromCty(t.stateValue)
	if err != nil {
		return nil, err
	}
	// grpc servers add a "timeouts" key to compensate for infinite diffs; this is not needed in
	// the Pulumi projection.
	delete(res, schema.TimeoutsConfigKey)
	return res, nil
}

type setChecker struct{}

func (s setChecker) IsSet(ctx context.Context, v interface{}) ([]interface{}, bool) {
	return nil, false
}

func convertTFValueToPulumiValue(
	tfValue cty.Value, res shim.Resource, pulumiResource *info.Resource, sensitivePaths []cty.Path,
) (resource.PropertyMap, error) {
	instanceState := terraformState{
		stateValue: tfValue,
		// TODO[pulumi/service#35118]: meta handling
		meta: nil,
	}

	// This assumes that the schema version of the resource state is exactly the same as the one in the provider.
	// TODO: add an assert for this.
	props, err := tfbridge.MakeTerraformResult(context.TODO(), setChecker{}, instanceState, res.Schema(), pulumiResource.Fields, nil, true)
	if err != nil {
		return nil, fmt.Errorf("failed to make Terraform result: %w", err)
	}

	secretPaths := ctyPathsToPropertyPaths(sensitivePaths, res, pulumiResource)
	secretedProps, err := ensureSecrets(props, secretPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure secrets: %w", err)
	}

	if err := tfbridge.RawStateInjectDelta(context.TODO(), res.Schema(), pulumiResource.Fields, props, res.SchemaType(), instanceState); err != nil {
		return nil, err
	}

	return secretedProps, nil
}

func ensureSecrets(props resource.PropertyMap, sensitivePaths []resource.PropertyPath) (resource.PropertyMap, error) {
	propValue := resource.NewObjectProperty(props)
	for _, propertyPath := range sensitivePaths {
		pathProp, ok := propertyPath.Get(propValue)
		if !ok {
			return nil, fmt.Errorf("failed to get property value for path: %s", propertyPath)
		}
		if !pathProp.IsSecret() {
			secretProp := resource.MakeSecret(pathProp)
			propertyPath.Set(propValue, secretProp)
		}
	}
	return propValue.ObjectValue(), nil
}

// ctyPathsToPropertyPaths converts cty.Paths to resource.PropertyPaths, translating
// Terraform attribute names to Pulumi property names while preserving concrete indices.
//
// This is based on the implementation of [tfbridge.SchemaPathToPropertyPath] in pulumi-terraform-bridge.
// Unlike [tfbridge.SchemaPathToPropertyPath] which uses "*" for all element access,
// this function preserves the actual index values from the cty.Path.
func ctyPathsToPropertyPaths(paths []cty.Path, res shim.Resource, pulumiResource *info.Resource) []resource.PropertyPath {
	propertyPaths := make([]resource.PropertyPath, 0, len(paths))
	for _, path := range paths {
		if pp := ctyPathToPropertyPath(path, res.Schema(), pulumiResource.Fields); pp != nil {
			propertyPaths = append(propertyPaths, pp)
		}
	}
	return propertyPaths
}

func ctyPathToPropertyPath(
	ctyPath cty.Path,
	schemaMap shim.SchemaMap,
	schemaInfos map[string]*tfbridge.SchemaInfo,
) resource.PropertyPath {
	return ctyPathToPropertyPathInner(resource.PropertyPath{}, ctyPath, schemaMap, schemaInfos)
}

func ctyPathToPropertyPathInner(
	basePath resource.PropertyPath,
	ctyPath cty.Path,
	schemaMap shim.SchemaMap,
	schemaInfos map[string]*tfbridge.SchemaInfo,
) resource.PropertyPath {
	if len(ctyPath) == 0 {
		return basePath
	}

	if schemaInfos == nil {
		schemaInfos = make(map[string]*tfbridge.SchemaInfo)
	}

	firstStep, ok := ctyPath[0].(cty.GetAttrStep)
	if !ok {
		return nil
	}

	tfName := firstStep.Name
	pulumiName := tfbridge.TerraformToPulumiNameV2(tfName, schemaMap, schemaInfos)

	fieldSchema, found := schemaMap.GetOk(tfName)
	if !found {
		return nil
	}
	fieldInfo := schemaInfos[tfName]
	return ctyPathToPropertyPathSchema(append(basePath, pulumiName), ctyPath[1:], fieldSchema, fieldInfo)
}

func ctyPathToPropertyPathSchema(
	basePath resource.PropertyPath,
	ctyPath cty.Path,
	schema shim.Schema,
	schemaInfo *tfbridge.SchemaInfo,
) resource.PropertyPath {
	if len(ctyPath) == 0 {
		return basePath
	}

	if schemaInfo == nil {
		schemaInfo = &tfbridge.SchemaInfo{}
	}

	// Detect single-nested blocks (object types) - TypeMap with Resource elem but no MaxItems constraint
	if res, isRes := schema.Elem().(shim.Resource); isRes && schema.Type() == shim.TypeMap {
		return ctyPathToPropertyPathInner(basePath, ctyPath, res.Schema(), schemaInfo.Fields)
	}

	switch schema.Type() {
	case shim.TypeList, shim.TypeSet, shim.TypeMap:
		if tfbridge.IsMaxItemsOne(schema, schemaInfo) {
			// For MaxItemsOne, Pulumi flattens the collection, so we skip the index step
			// and continue with the remaining path.
			if _, isIndex := ctyPath[0].(cty.IndexStep); isIndex {
				ctyPath = ctyPath[1:]
			}
		} else {
			indexStep, isIndex := ctyPath[0].(cty.IndexStep)
			contract.Assertf(isIndex, "Expected index step, got %T", ctyPath[0])
			index := ctyIndexToPropertyPathElement(indexStep)
			basePath = append(basePath, index)
			ctyPath = ctyPath[1:]
		}

		switch e := schema.Elem().(type) {
		case shim.Resource:
			elem := schemaInfo.Elem
			if elem == nil {
				elem = &tfbridge.SchemaInfo{}
			}
			return ctyPathToPropertyPathInner(basePath, ctyPath, e.Schema(), elem.Fields)
		case shim.Schema:
			return ctyPathToPropertyPathSchema(basePath, ctyPath, e, schemaInfo.Elem)
		case nil:
			// unknown element type
			return basePath
		}
	}

	// Cannot drill down further, but len(ctyPath) > 0.
	return basePath
}

// ctyIndexToPropertyPathElement converts a cty.IndexStep to a property path element.
// For list indices (numbers), it returns an int. For map keys (strings), it returns a string.
func ctyIndexToPropertyPathElement(step cty.IndexStep) interface{} {
	key := step.Key
	switch key.Type() {
	case cty.Number:
		bf := key.AsBigFloat()
		i64, _ := bf.Int64()
		return int(i64)
	case cty.String:
		return key.AsString()
	default:
		contract.Assertf(false, "Unexpected index type: %s", key.Type())
		return nil
	}
}

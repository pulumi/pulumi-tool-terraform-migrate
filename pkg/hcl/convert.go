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

package hcl

import (
	"math/big"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
)

// CtyMapToPulumiPropertyMap converts a map of cty values to a Pulumi PropertyMap.
func CtyMapToPulumiPropertyMap(values map[string]cty.Value) resource.PropertyMap {
	props := resource.PropertyMap{}
	for k, v := range values {
		props[resource.PropertyKey(k)] = CtyValueToPulumiPropertyValue(v)
	}
	return props
}

// CtyValueToPulumiPropertyValue converts a single cty.Value to a Pulumi PropertyValue.
func CtyValueToPulumiPropertyValue(val cty.Value) resource.PropertyValue {
	if val.IsNull() {
		return resource.NewNullProperty()
	}
	if !val.IsKnown() {
		return resource.MakeComputed(resource.NewStringProperty(""))
	}

	ty := val.Type()
	switch {
	case ty == cty.String:
		return resource.NewStringProperty(val.AsString())
	case ty == cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return resource.NewNumberProperty(float64(i))
		}
		f, _ := bf.Float64()
		return resource.NewNumberProperty(f)
	case ty == cty.Bool:
		return resource.NewBoolProperty(val.True())
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		var items []resource.PropertyValue
		for it := val.ElementIterator(); it.Next(); {
			_, v := it.Element()
			items = append(items, CtyValueToPulumiPropertyValue(v))
		}
		return resource.NewArrayProperty(items)
	case ty.IsMapType() || ty.IsObjectType():
		props := resource.PropertyMap{}
		for it := val.ElementIterator(); it.Next(); {
			k, v := it.Element()
			props[resource.PropertyKey(k.AsString())] = CtyValueToPulumiPropertyValue(v)
		}
		return resource.NewObjectProperty(props)
	default:
		// Fallback: convert to string representation
		return resource.NewStringProperty(val.GoString())
	}
}

// PulumiPropertyMapToCtyMap converts a Pulumi PropertyMap back to a map of cty values.
// This is useful for building HCL eval contexts from existing Pulumi state.
func PulumiPropertyMapToCtyMap(props resource.PropertyMap) map[string]cty.Value {
	values := map[string]cty.Value{}
	for k, v := range props {
		values[string(k)] = pulumiPropertyValueToCty(v)
	}
	return values
}

func pulumiPropertyValueToCty(v resource.PropertyValue) cty.Value {
	switch {
	case v.IsNull():
		return cty.NilVal
	case v.IsString():
		return cty.StringVal(v.StringValue())
	case v.IsNumber():
		return cty.NumberVal(new(big.Float).SetFloat64(v.NumberValue()))
	case v.IsBool():
		return cty.BoolVal(v.BoolValue())
	case v.IsArray():
		if len(v.ArrayValue()) == 0 {
			return cty.EmptyTupleVal
		}
		var vals []cty.Value
		for _, item := range v.ArrayValue() {
			vals = append(vals, pulumiPropertyValueToCty(item))
		}
		return cty.TupleVal(vals)
	case v.IsObject():
		if len(v.ObjectValue()) == 0 {
			return cty.EmptyObjectVal
		}
		vals := map[string]cty.Value{}
		for k, item := range v.ObjectValue() {
			vals[string(k)] = pulumiPropertyValueToCty(item)
		}
		return cty.ObjectVal(vals)
	default:
		return cty.StringVal(v.String())
	}
}

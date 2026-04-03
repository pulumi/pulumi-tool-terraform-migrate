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
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestCtyMapToPulumiPropertyMap_Primitives(t *testing.T) {
	t.Parallel()
	input := map[string]cty.Value{
		"cidr":    cty.StringVal("10.0.0.0/16"),
		"name":    cty.StringVal("production"),
		"count":   cty.NumberIntVal(2),
		"enabled": cty.True,
	}
	result := CtyMapToPulumiPropertyMap(input)
	require.Equal(t, resource.NewStringProperty("10.0.0.0/16"), result["cidr"])
	require.Equal(t, resource.NewStringProperty("production"), result["name"])
	require.Equal(t, resource.NewNumberProperty(2), result["count"])
	require.Equal(t, resource.NewBoolProperty(true), result["enabled"])
}

func TestCtyMapToPulumiPropertyMap_Collections(t *testing.T) {
	t.Parallel()
	input := map[string]cty.Value{
		"tags": cty.MapVal(map[string]cty.Value{
			"env":  cty.StringVal("prod"),
			"team": cty.StringVal("infra"),
		}),
		"ids": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}),
	}
	result := CtyMapToPulumiPropertyMap(input)

	tags := result["tags"].ObjectValue()
	require.Equal(t, resource.NewStringProperty("prod"), tags["env"])
	require.Equal(t, resource.NewStringProperty("infra"), tags["team"])

	ids := result["ids"].ArrayValue()
	require.Len(t, ids, 2)
	require.Equal(t, resource.NewStringProperty("a"), ids[0])
	require.Equal(t, resource.NewStringProperty("b"), ids[1])
}

func TestCtyMapToPulumiPropertyMap_Null(t *testing.T) {
	t.Parallel()
	input := map[string]cty.Value{
		"empty": cty.NullVal(cty.String),
	}
	result := CtyMapToPulumiPropertyMap(input)
	require.True(t, result["empty"].IsNull())
}

func TestCtyMapToPulumiPropertyMap_Empty(t *testing.T) {
	t.Parallel()
	result := CtyMapToPulumiPropertyMap(map[string]cty.Value{})
	require.Len(t, result, 0)
}

func TestPulumiPropertyMapToCtyMap_Roundtrip(t *testing.T) {
	t.Parallel()
	original := map[string]cty.Value{
		"name":  cty.StringVal("test"),
		"count": cty.NumberIntVal(5),
		"flag":  cty.True,
	}
	props := CtyMapToPulumiPropertyMap(original)
	roundtripped := PulumiPropertyMapToCtyMap(props)

	require.Equal(t, "test", roundtripped["name"].AsString())
	require.True(t, roundtripped["flag"].True())
}

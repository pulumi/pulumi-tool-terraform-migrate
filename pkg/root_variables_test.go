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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func TestCollectTFVarsConfig(t *testing.T) {
	t.Parallel()

	// Uses testdata/tf_tfvars_resolution which has:
	//   main.tf: variable "env" { type = string }
	//   terraform.tfvars: env = "staging"
	tfDir := "testdata/tf_tfvars_resolution"
	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	entries := collectTFVarsConfig(config, tfDir)

	require.Len(t, entries, 1)
	assert.Equal(t, "env", entries[0].ConfigKey)
	assert.Equal(t, "staging", entries[0].Value)
	assert.False(t, entries[0].Secret)
}

func TestCollectTFVarsConfig_NoTfvars(t *testing.T) {
	t.Parallel()

	// A directory with no .tfvars files should return empty entries.
	// Use the tfvars_resolution dir's config but point tfDir at a temp dir.
	tfDir := "testdata/tf_tfvars_resolution"
	config, err := LoadConfig(tfDir)
	require.NoError(t, err)

	// Point at a dir with no tfvars files.
	entries := collectTFVarsConfig(config, t.TempDir())
	assert.Empty(t, entries)
}

func TestCtyValueToString_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "hello", ctyValueToString(cty.StringVal("hello")))
}

func TestCtyValueToString_List(t *testing.T) {
	t.Parallel()
	val := cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")})
	assert.Equal(t, `["a","b"]`, ctyValueToString(val))
}

func TestCtyValueToString_Number(t *testing.T) {
	t.Parallel()
	val := cty.NumberIntVal(42)
	assert.Equal(t, "42", ctyValueToString(val))
}

func TestCtyValueToString_Bool(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "true", ctyValueToString(cty.True))
}

func TestCtyValueToString_Null(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", ctyValueToString(cty.NullVal(cty.String)))
}

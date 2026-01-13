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

package tofu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zclconf/go-cty/cty"
)

func TestSensitiveObjToCtyPath(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		input    map[string]interface{}
		expected []cty.Path
	}

	testCases := []testCase{
		{
			name:     "empty object",
			input:    map[string]interface{}{},
			expected: []cty.Path{},
		},
		{
			name: "single sensitive value",
			input: map[string]interface{}{
				"password": true,
			},
			expected: []cty.Path{
				{cty.GetAttrStep{Name: "password"}},
			},
		},
		{
			name: "non-sensitive value (false)",
			input: map[string]interface{}{
				"username": false,
			},
			expected: []cty.Path{},
		},
		{
			name: "multiple sensitive values",
			input: map[string]interface{}{
				"password":   true,
				"api_key":    true,
				"secret_key": true,
			},
			expected: []cty.Path{
				{cty.GetAttrStep{Name: "password"}},
				{cty.GetAttrStep{Name: "api_key"}},
				{cty.GetAttrStep{Name: "secret_key"}},
			},
		},
		{
			name: "nested map with sensitive values",
			input: map[string]interface{}{
				"credentials": map[string]interface{}{
					"username": false,
					"password": true,
				},
			},
			expected: []cty.Path{
				{
					cty.GetAttrStep{Name: "credentials"},
					cty.GetAttrStep{Name: "password"},
				},
			},
		},
		{
			name: "deeply nested map",
			input: map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": map[string]interface{}{
							"secret": true,
						},
					},
				},
			},
			expected: []cty.Path{
				{
					cty.GetAttrStep{Name: "level1"},
					cty.GetAttrStep{Name: "level2"},
					cty.GetAttrStep{Name: "level3"},
					cty.GetAttrStep{Name: "secret"},
				},
			},
		},
		{
			name: "list with sensitive boolean values",
			input: map[string]interface{}{
				"tokens": []interface{}{true, false, true},
			},
			expected: []cty.Path{
				{
					cty.GetAttrStep{Name: "tokens"},
					cty.IndexStep{Key: cty.NumberIntVal(0)},
				},
				{
					cty.GetAttrStep{Name: "tokens"},
					cty.IndexStep{Key: cty.NumberIntVal(2)},
				},
			},
		},
		{
			name: "list with nested map containing sensitive values",
			input: map[string]interface{}{
				"users": []interface{}{
					map[string]interface{}{
						"name":     false,
						"password": true,
					},
					map[string]interface{}{
						"name":     false,
						"password": true,
					},
				},
			},
			expected: []cty.Path{
				{
					cty.GetAttrStep{Name: "users"},
					cty.IndexStep{Key: cty.NumberIntVal(0)},
					cty.GetAttrStep{Name: "password"},
				},
				{
					cty.GetAttrStep{Name: "users"},
					cty.IndexStep{Key: cty.NumberIntVal(1)},
					cty.GetAttrStep{Name: "password"},
				},
			},
		},
		{
			name: "nested list within list",
			input: map[string]interface{}{
				"matrix": []interface{}{
					[]interface{}{true, false},
					[]interface{}{false, true},
				},
			},
			expected: []cty.Path{
				{
					cty.GetAttrStep{Name: "matrix"},
					cty.IndexStep{Key: cty.NumberIntVal(0)},
					cty.IndexStep{Key: cty.NumberIntVal(0)},
				},
				{
					cty.GetAttrStep{Name: "matrix"},
					cty.IndexStep{Key: cty.NumberIntVal(1)},
					cty.IndexStep{Key: cty.NumberIntVal(1)},
				},
			},
		},
		{
			name: "complex mixed structure",
			input: map[string]interface{}{
				"public_key":  false,
				"private_key": true,
				"config": map[string]interface{}{
					"endpoint": false,
					"auth": map[string]interface{}{
						"token": true,
					},
				},
				"secrets": []interface{}{
					map[string]interface{}{
						"name":  false,
						"value": true,
					},
				},
			},
			expected: []cty.Path{
				{cty.GetAttrStep{Name: "private_key"}},
				{
					cty.GetAttrStep{Name: "config"},
					cty.GetAttrStep{Name: "auth"},
					cty.GetAttrStep{Name: "token"},
				},
				{
					cty.GetAttrStep{Name: "secrets"},
					cty.IndexStep{Key: cty.NumberIntVal(0)},
					cty.GetAttrStep{Name: "value"},
				},
			},
		},
		{
			name: "empty nested structures",
			input: map[string]interface{}{
				"empty_map":  map[string]interface{}{},
				"empty_list": []interface{}{},
			},
			expected: []cty.Path{},
		},
		{
			name: "list containing empty map",
			input: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{},
					map[string]interface{}{
						"secret": true,
					},
				},
			},
			expected: []cty.Path{
				{
					cty.GetAttrStep{Name: "items"},
					cty.IndexStep{Key: cty.NumberIntVal(1)},
					cty.GetAttrStep{Name: "secret"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := SensitiveObjToCtyPath(tc.input)

			assert.Equal(t, len(tc.expected), len(result), "number of paths should match")

			for _, expectedPath := range tc.expected {
				assert.Contains(t, result, expectedPath)
			}
		})
	}
}

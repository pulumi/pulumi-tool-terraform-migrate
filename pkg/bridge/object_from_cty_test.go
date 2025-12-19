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

package bridge

import (
	"reflect"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestObjectFromCty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  cty.Value
		want map[string]interface{}
	}{
		{
			name: "string",
			val:  cty.ObjectVal(map[string]cty.Value{"x": cty.StringVal("y")}),
			want: map[string]interface{}{"x": "y"},
		},
		{
			name: "list",
			val:  cty.ObjectVal(map[string]cty.Value{"x": cty.ListVal([]cty.Value{cty.StringVal("y")})}),
			want: map[string]interface{}{"x": []interface{}{"y"}},
		},
		{
			name: "set",
			val:  cty.ObjectVal(map[string]cty.Value{"x": cty.SetVal([]cty.Value{cty.StringVal("y")})}),
			want: map[string]interface{}{"x": []interface{}{"y"}},
		},
		{
			name: "map",
			val:  cty.ObjectVal(map[string]cty.Value{"x": cty.MapVal(map[string]cty.Value{"y": cty.StringVal("z")})}),
			want: map[string]interface{}{"x": map[string]interface{}{"y": "z"}},
		},
		{
			name: "object",
			val:  cty.ObjectVal(map[string]cty.Value{"x": cty.ObjectVal(map[string]cty.Value{"y": cty.StringVal("z")})}),
			want: map[string]interface{}{"x": map[string]interface{}{"y": "z"}},
		},
		{
			name: "tuple",
			val:  cty.ObjectVal(map[string]cty.Value{"x": cty.TupleVal([]cty.Value{cty.StringVal("y"), cty.StringVal("z")})}),
			want: map[string]interface{}{"x": []interface{}{"y", "z"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ObjectFromCty(test.val)
			if err != nil {
				t.Fatalf("failed to convert cty.Value to map[string]interface{}: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("expected %v, got %v", test.want, got)
			}
		})
	}
}

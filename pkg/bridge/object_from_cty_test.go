package pkg

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
			got, err := objectFromCty(test.val)
			if err != nil {
				t.Fatalf("failed to convert cty.Value to map[string]interface{}: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("expected %v, got %v", test.want, got)
			}
		})
	}
}

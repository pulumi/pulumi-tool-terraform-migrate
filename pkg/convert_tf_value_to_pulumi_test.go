package pkg

import (
	"reflect"
	"testing"

	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	schemashim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim/schema"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
)

func TestConvertTFValueToPulumiValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		val            cty.Value
		res            shim.Resource
		pulumiResource *info.Resource
		want           resource.PropertyMap
	}{
		{
			name: "string",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.StringVal("y")}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeString,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": "y"}),
		},
		{
			name: "list",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.ListVal([]cty.Value{cty.StringVal("y")})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeList,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"props": []interface{}{"y"}}),
		},
		{
			name: "set",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.SetVal([]cty.Value{cty.StringVal("y")})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeSet,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"props": []interface{}{"y"}}),
		},
		{
			name: "map",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.MapVal(map[string]cty.Value{"y": cty.StringVal("z")})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeMap,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": map[string]interface{}{"y": "z"}}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			props, err := convertTFValueToPulumiValue(test.val, test.res, test.pulumiResource)
			if err != nil {
				t.Fatalf("failed to convert cty.Value to map[string]interface{}: %v", err)
			}
			if !reflect.DeepEqual(props, test.want) {
				t.Errorf("expected %v, got %v", test.want, props)
			}
		})
	}
}

package pkg

import (
	"testing"

	"github.com/hexops/autogold/v2"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	schemashim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/valueshim"
	"github.com/zclconf/go-cty/cty"
)

func TestConvertTFValueToPulumiValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		val            cty.Value
		res            shim.Resource
		pulumiResource *info.Resource
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
				SchemaType: valueshim.FromCtyType(cty.Object(map[string]cty.Type{"prop": cty.String})),
			}).Shim(),
			pulumiResource: &info.Resource{},
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
				SchemaType: valueshim.FromCtyType(cty.Object(map[string]cty.Type{"prop": cty.List(cty.String)})),
			}).Shim(),
			pulumiResource: &info.Resource{},
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
				SchemaType: valueshim.FromCtyType(cty.Object(map[string]cty.Type{"prop": cty.Set(cty.String)})),
			}).Shim(),
			pulumiResource: &info.Resource{},
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
				SchemaType: valueshim.FromCtyType(cty.Object(map[string]cty.Type{"prop": cty.Map(cty.String)})),
			}).Shim(),
			pulumiResource: &info.Resource{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			props, err := convertTFValueToPulumiValue(test.val, test.res, test.pulumiResource)
			if err != nil {
				t.Fatalf("failed to convert cty.Value to map[string]interface{}: %v", err)
			}
			autogold.ExpectFile(t, props)
		})
	}
}

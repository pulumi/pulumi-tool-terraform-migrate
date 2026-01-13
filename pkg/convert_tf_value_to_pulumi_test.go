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
		sensitivePaths []cty.Path
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
			name: "number int",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.NumberIntVal(42)}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeInt,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": 42}),
		},
		{
			name: "number float",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.NumberFloatVal(3.14)}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeFloat,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": 3.14}),
		},
		{
			name: "boolean",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.BoolVal(true)}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeBool,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": true}),
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
		{
			name: "sensitive schema property",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.StringVal("y")}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeString,
						Sensitive: true,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{{cty.GetAttrStep{Name: "prop"}}},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": resource.MakeSecret(resource.NewStringProperty("y"))}),
		},
		{
			name: "sensitive path property",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.StringVal("y")}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeString,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{{cty.GetAttrStep{Name: "prop"}}},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": resource.MakeSecret(resource.NewStringProperty("y"))}),
		},
		{
			name: "nested sensitive value map",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.ListVal([]cty.Value{cty.ObjectVal(map[string]cty.Value{"subprop": cty.StringVal("y")})})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeList,
						Elem: (&schemashim.Resource{
							Schema: schemashim.SchemaMap{
								"subprop": (&schemashim.Schema{
									Type: shim.TypeString,
								}).Shim(),
							},
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{{cty.GetAttrStep{Name: "prop"}, cty.IndexStep{Key: cty.NumberIntVal(0)}, cty.GetAttrStep{Name: "subprop"}}},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"props": []interface{}{map[string]interface{}{"subprop": resource.MakeSecret(resource.NewStringProperty("y"))}}}),
		},
		{
			name: "nested sensitive value with list",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.ListVal([]cty.Value{
				cty.ObjectVal(map[string]cty.Value{"subprop": cty.StringVal("y")}),
				cty.ObjectVal(map[string]cty.Value{"subprop": cty.StringVal("z")}),
			})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeList,
						Elem: (&schemashim.Resource{
							Schema: schemashim.SchemaMap{
								"subprop": (&schemashim.Schema{
									Type:      shim.TypeString,
								}).Shim(),
							},
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{{cty.GetAttrStep{Name: "prop"}, cty.IndexStep{Key: cty.NumberIntVal(1)}, cty.GetAttrStep{Name: "subprop"}}},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"props": []interface{}{map[string]interface{}{"subprop": resource.NewStringProperty("y")}, map[string]interface{}{"subprop": resource.MakeSecret(resource.NewStringProperty("z"))}}}),
		},
		{
			name: "max items one",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.ObjectVal(map[string]cty.Value{"subprop": cty.StringVal("y")})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type:     shim.TypeList,
						MaxItems: 1,
						Elem: (&schemashim.Resource{
							Schema: schemashim.SchemaMap{
								"subprop": (&schemashim.Schema{
									Type:      shim.TypeString,
								}).Shim(),
							},
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{{cty.GetAttrStep{Name: "prop"}, cty.IndexStep{Key: cty.NumberIntVal(0)}, cty.GetAttrStep{Name: "subprop"}}},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": map[string]interface{}{"subprop": resource.MakeSecret(resource.NewStringProperty("y"))}}),
		},
		{
			name: "multiple properties",
			val: cty.ObjectVal(map[string]cty.Value{
				"name":    cty.StringVal("test"),
				"count":   cty.NumberIntVal(5),
				"enabled": cty.BoolVal(true),
			}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"name": (&schemashim.Schema{
						Type: shim.TypeString,
					}).Shim(),
					"count": (&schemashim.Schema{
						Type: shim.TypeInt,
					}).Shim(),
					"enabled": (&schemashim.Schema{
						Type: shim.TypeBool,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"name": "test", "count": 5, "enabled": true}),
		},
		{
			name: "list with multiple strings",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b"), cty.StringVal("c")})}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeList,
						Elem: (&schemashim.Schema{
							Type: shim.TypeString,
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"props": []interface{}{"a", "b", "c"}}),
		},
		{
			name: "empty map",
			val:  cty.ObjectVal(map[string]cty.Value{"prop": cty.MapValEmpty(cty.String)}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"prop": (&schemashim.Schema{
						Type: shim.TypeMap,
						Elem: (&schemashim.Schema{
							Type: shim.TypeString,
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"prop": map[string]interface{}{}}),
		},
		{
			name: "multiple sensitive paths",
			val: cty.ObjectVal(map[string]cty.Value{
				"password": cty.StringVal("secret123"),
				"token":    cty.StringVal("abc-token"),
				"name":     cty.StringVal("public"),
			}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"password": (&schemashim.Schema{
						Type: shim.TypeString,
					}).Shim(),
					"token": (&schemashim.Schema{
						Type: shim.TypeString,
					}).Shim(),
					"name": (&schemashim.Schema{
						Type: shim.TypeString,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{
				{cty.GetAttrStep{Name: "password"}},
				{cty.GetAttrStep{Name: "token"}},
			},
			want: resource.NewPropertyMapFromMap(map[string]interface{}{
				"password": resource.MakeSecret(resource.NewStringProperty("secret123")),
				"token":    resource.MakeSecret(resource.NewStringProperty("abc-token")),
				"name":     "public",
			}),
		},
		{
			name: "deeply nested structure",
			val: cty.ObjectVal(map[string]cty.Value{
				"level1": cty.ListVal([]cty.Value{
					cty.ObjectVal(map[string]cty.Value{
						"level2": cty.ListVal([]cty.Value{
							cty.ObjectVal(map[string]cty.Value{
								"level3": cty.StringVal("deep"),
							}),
						}),
					}),
				}),
			}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"level1": (&schemashim.Schema{
						Type: shim.TypeList,
						Elem: (&schemashim.Resource{
							Schema: schemashim.SchemaMap{
								"level2": (&schemashim.Schema{
									Type: shim.TypeList,
									Elem: (&schemashim.Resource{
										Schema: schemashim.SchemaMap{
											"level3": (&schemashim.Schema{
												Type: shim.TypeString,
											}).Shim(),
										},
									}).Shim(),
								}).Shim(),
							},
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{
				{cty.GetAttrStep{Name: "level1"}, cty.IndexStep{Key: cty.NumberIntVal(0)}, cty.GetAttrStep{Name: "level2"}, cty.IndexStep{Key: cty.NumberIntVal(0)}, cty.GetAttrStep{Name: "level3"}},
			},
			want: resource.NewPropertyMapFromMap(map[string]interface{}{
				"level1s": []interface{}{
					map[string]interface{}{
						"level2s": []interface{}{
							map[string]interface{}{
								"level3": resource.MakeSecret(resource.NewStringProperty("deep")),
							},
						},
					},
				},
			}),
		},
		{
			name: "map with multiple entries",
			val: cty.ObjectVal(map[string]cty.Value{
				"tags": cty.MapVal(map[string]cty.Value{
					"env":     cty.StringVal("production"),
					"team":    cty.StringVal("platform"),
					"project": cty.StringVal("migrate"),
				}),
			}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"tags": (&schemashim.Schema{
						Type: shim.TypeMap,
						Elem: (&schemashim.Schema{
							Type: shim.TypeString,
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want: resource.NewPropertyMapFromMap(map[string]interface{}{
				"tags": map[string]interface{}{
					"env":     "production",
					"team":    "platform",
					"project": "migrate",
				},
			}),
		},
		{
			name: "set with nested object",
			val: cty.ObjectVal(map[string]cty.Value{
				"ingress": cty.SetVal([]cty.Value{
					cty.ObjectVal(map[string]cty.Value{
						"port":     cty.NumberIntVal(443),
						"protocol": cty.StringVal("tcp"),
					}),
				}),
			}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"ingress": (&schemashim.Schema{
						Type: shim.TypeSet,
						Elem: (&schemashim.Resource{
							Schema: schemashim.SchemaMap{
								"port": (&schemashim.Schema{
									Type: shim.TypeInt,
								}).Shim(),
								"protocol": (&schemashim.Schema{
									Type: shim.TypeString,
								}).Shim(),
							},
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			want: resource.NewPropertyMapFromMap(map[string]interface{}{
				"ingresses": []interface{}{
					map[string]interface{}{"port": 443, "protocol": "tcp"},
				},
			}),
		},
		{
			name: "sensitive number",
			val:  cty.ObjectVal(map[string]cty.Value{"secret_count": cty.NumberIntVal(42)}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"secret_count": (&schemashim.Schema{
						Type: shim.TypeInt,
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{{cty.GetAttrStep{Name: "secret_count"}}},
			want:           resource.NewPropertyMapFromMap(map[string]interface{}{"secretCount": resource.MakeSecret(resource.NewNumberProperty(42))}),
		},
		{
			name: "sensitive map value",
			val: cty.ObjectVal(map[string]cty.Value{
				"secrets": cty.MapVal(map[string]cty.Value{
					"api_key": cty.StringVal("key123"),
					"token":   cty.StringVal("tok456"),
				}),
			}),
			res: (&schemashim.Resource{
				Schema: schemashim.SchemaMap{
					"secrets": (&schemashim.Schema{
						Type: shim.TypeMap,
						Elem: (&schemashim.Schema{
							Type: shim.TypeString,
						}).Shim(),
					}).Shim(),
				},
			}).Shim(),
			pulumiResource: &info.Resource{},
			sensitivePaths: []cty.Path{
				{cty.GetAttrStep{Name: "secrets"}, cty.IndexStep{Key: cty.StringVal("api_key")}},
			},
			want: resource.NewPropertyMapFromMap(map[string]interface{}{
				"secrets": map[string]interface{}{
					"api_key": resource.MakeSecret(resource.NewStringProperty("key123")),
					"token":   "tok456",
				},
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			props, err := convertTFValueToPulumiValue(test.val, test.res, test.pulumiResource, test.sensitivePaths)
			if err != nil {
				t.Fatalf("failed to convert cty.Value to map[string]interface{}: %v", err)
			}
			if !reflect.DeepEqual(props, test.want) {
				t.Errorf("expected %v, got %v", test.want, props)
			}
		})
	}
}

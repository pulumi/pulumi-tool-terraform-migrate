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
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	sdkv2 "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim/sdk-v2"
)

func TestImpliedType(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, res schema.Resource) {
		impliedType := res.CoreConfigSchema().ImpliedType()

		marshalledRes := info.MarshalResourceShim(sdkv2.NewResource(&res))

		unmarshalledRes := marshalledRes.Unmarshal()
		unmarshalledImpliedType := ImpliedType(unmarshalledRes.Schema(), false)

		if !unmarshalledImpliedType.Equals(htype2ctype(impliedType)) {
			t.Errorf("expected implied type to be: \n%v\ngot: \n%v", impliedType.GoString(), unmarshalledImpliedType.GoString())
		}
	}

	t.Run("primitive types", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeString},
				"y": {Type: schema.TypeInt, Optional: true},
				"z": {Type: schema.TypeBool},
				"w": {Type: schema.TypeFloat},
			},
		}

		check(t, res)
	})

	t.Run("collection attributes", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}},
				"y": {Type: schema.TypeSet, Elem: &schema.Schema{Type: schema.TypeString}},
				"z": {Type: schema.TypeMap, Elem: &schema.Schema{Type: schema.TypeString}},
			},
		}

		check(t, res)
	})

	t.Run("collection blocks", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeList, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"y": {Type: schema.TypeString},
				}}},
				"z": {Type: schema.TypeSet, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"y": {Type: schema.TypeString},
				}}},
			},
		}

		check(t, res)
	})

	t.Run("nested blocks", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeList, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"y": {Type: schema.TypeSet, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
						"z": {Type: schema.TypeString},
					}}},
				}}},
			},
		}

		check(t, res)
	})
}

func TestImpliedTypeTimeouts(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, res schema.Resource) {
		impliedType := res.CoreConfigSchema().ImpliedType()

		marshalledRes := info.MarshalResourceShim(sdkv2.NewResource(&res))

		unmarshalledRes := marshalledRes.Unmarshal()
		unmarshalledImpliedType := ImpliedType(unmarshalledRes.Schema(), true)

		if !unmarshalledImpliedType.Equals(htype2ctype(impliedType)) {
			t.Errorf("expected implied type to be: \n%v\ngot: \n%v", impliedType.GoString(), unmarshalledImpliedType.GoString())
		}
	}

	t.Run("primitive types", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Timeouts: &schema.ResourceTimeout{
				Create:  schema.DefaultTimeout(10 * time.Second),
				Read:    schema.DefaultTimeout(10 * time.Second),
				Update:  schema.DefaultTimeout(10 * time.Second),
				Delete:  schema.DefaultTimeout(10 * time.Second),
				Default: schema.DefaultTimeout(10 * time.Second),
			},
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeString},
				"y": {Type: schema.TypeInt, Optional: true},
				"z": {Type: schema.TypeBool},
				"w": {Type: schema.TypeFloat},
			},
		}

		check(t, res)
	})

	t.Run("collection attributes", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Timeouts: &schema.ResourceTimeout{
				Create:  schema.DefaultTimeout(10 * time.Second),
				Read:    schema.DefaultTimeout(10 * time.Second),
				Update:  schema.DefaultTimeout(10 * time.Second),
				Delete:  schema.DefaultTimeout(10 * time.Second),
				Default: schema.DefaultTimeout(10 * time.Second),
			},
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeList, Elem: &schema.Schema{Type: schema.TypeString}},
				"y": {Type: schema.TypeSet, Elem: &schema.Schema{Type: schema.TypeString}},
				"z": {Type: schema.TypeMap, Elem: &schema.Schema{Type: schema.TypeString}},
			},
		}

		check(t, res)
	})

	t.Run("collection blocks", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Timeouts: &schema.ResourceTimeout{
				Create:  schema.DefaultTimeout(10 * time.Second),
				Read:    schema.DefaultTimeout(10 * time.Second),
				Update:  schema.DefaultTimeout(10 * time.Second),
				Delete:  schema.DefaultTimeout(10 * time.Second),
				Default: schema.DefaultTimeout(10 * time.Second),
			},
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeList, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"y": {Type: schema.TypeString},
				}}},
				"z": {Type: schema.TypeSet, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"y": {Type: schema.TypeString},
				}}},
			},
		}

		check(t, res)
	})

	t.Run("nested blocks", func(t *testing.T) {
		t.Parallel()

		res := schema.Resource{
			Timeouts: &schema.ResourceTimeout{
				Create:  schema.DefaultTimeout(10 * time.Second),
				Read:    schema.DefaultTimeout(10 * time.Second),
				Update:  schema.DefaultTimeout(10 * time.Second),
				Delete:  schema.DefaultTimeout(10 * time.Second),
				Default: schema.DefaultTimeout(10 * time.Second),
			},
			Schema: map[string]*schema.Schema{
				"x": {Type: schema.TypeList, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
					"y": {Type: schema.TypeSet, Elem: &schema.Resource{Schema: map[string]*schema.Schema{
						"z": {Type: schema.TypeString},
					}}},
				}}},
			},
		}

		check(t, res)
	})
}

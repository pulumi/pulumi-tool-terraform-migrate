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
	"encoding/json"
	"fmt"
	"os"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func StateToCtyValue(resource *tfjson.StateResource, ty cty.Type) (cty.Value, error) {
	// TODO[pulumi/pulumi-service#35117]: add support for sensitive values
	attrs := resource.AttributeValues

	// Filter out attributes not present in the target type schema.
	// This handles deprecated/removed attributes that may linger in older state files
	// but are no longer recognized by the current provider schema version.
	if ty.IsObjectType() && attrs != nil {
		filtered := make(map[string]interface{}, len(attrs))
		for k, v := range attrs {
			if ty.HasAttribute(k) {
				filtered[k] = v
			} else {
				fmt.Fprintf(os.Stderr, "Warning: skipping deprecated attribute %q on %s (not in current provider schema)\n",
					k, resource.Address)
			}
		}
		attrs = filtered
	}

	data, err := json.Marshal(attrs)
	if err != nil {
		return cty.Value{}, err
	}

	return ctyjson.Unmarshal(data, ty)
}

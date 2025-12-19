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

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

func StateToCtyValue(resource *tfjson.StateResource, ty cty.Type) (cty.Value, error) {
	// TODO[pulumi/pulumi-service#35117]: add support for sensitive values
	data, err := json.Marshal(resource.AttributeValues)
	if err != nil {
		return cty.Value{}, err
	}

	return ctyjson.Unmarshal(data, ty)
}

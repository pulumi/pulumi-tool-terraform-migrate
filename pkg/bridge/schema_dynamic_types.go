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
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
)

func schemaHasDynamicTypes(sch shim.Schema) bool {
	if sch.Type() == shim.TypeDynamic {
		return true
	}

	if sch.Type() == shim.TypeList || sch.Type() == shim.TypeSet || sch.Type() == shim.TypeMap {
		_, isSchemaElem := sch.Elem().(shim.Schema)
		if isSchemaElem {
			return schemaHasDynamicTypes(sch.Elem().(shim.Schema))
		}

		_, isResElem := sch.Elem().(shim.Resource)
		if isResElem {
			return resourceHasDynamicTypes(sch.Elem().(shim.Resource))
		}

		// unknown collection element type - best we can do is dynamic
		return true
	}

	return false
}

func resourceHasDynamicTypes(resource shim.Resource) bool {
	hasDynamicTypes := false
	resource.Schema().Range(func(key string, value shim.Schema) bool {
		if schemaHasDynamicTypes(value) {
			hasDynamicTypes = true
			return false
		}
		return true
	})
	return hasDynamicTypes
}

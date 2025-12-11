package pkg

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

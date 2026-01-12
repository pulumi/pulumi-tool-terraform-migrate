package tofu

import (
	"github.com/zclconf/go-cty/cty"
)

// SensitiveObjToCtyPath converts from the sensitive values object format stored in the Terraform state to a list of CTY paths for each sensitive value.
//
// The sensitive values object format is of this form:
//
//	{
//	  "path1": true,
//	  "path2": {
//	    "subpath1": true,
//	    "subpath2": true,
//	  },
//	  "path3": [
//	    {
//	      "subpath1": true,
//	      "subpath2": true,
//	    },
//	  ],
//	}
//
// Each value is either a boolean, a map or a list. A boolean value indicates if the value is sensitive or not and should be masked. A map or a list value indicates that the values is a map or list which might contain sensitive values.
func SensitiveObjToCtyPath(obj map[string]interface{}) []cty.Path {
	return sensitiveObjToCtyPathMap(cty.Path{}, obj)
}

func sensitiveObjToCtyPathMap(currentPath cty.Path, obj map[string]interface{}) []cty.Path {
	paths := []cty.Path{}
	for key, value := range obj {
		if value, ok := value.(bool); ok && value {
			paths = append(paths, currentPath.GetAttr(key))
		}
		if value, ok := value.(map[string]interface{}); ok {
			mapPaths := sensitiveObjToCtyPathMap(currentPath.GetAttr(key), value)
			paths = append(paths, mapPaths...)
		}
		if value, ok := value.([]interface{}); ok {
			subpaths := sensitiveListToCtyPathList(currentPath.GetAttr(key), value)
			paths = append(paths, subpaths...)
		}
	}
	return paths
}

func sensitiveListToCtyPathList(currentPath cty.Path, obj []interface{}) []cty.Path {
	paths := []cty.Path{}
	for idx, value := range obj {
		if value, ok := value.(bool); ok && value {
			paths = append(paths, currentPath.IndexInt(idx))
		}
		if value, ok := value.(map[string]interface{}); ok {
			subpaths := sensitiveObjToCtyPathMap(currentPath.IndexInt(idx), value)
			paths = append(paths, subpaths...)
		}
		if value, ok := value.([]interface{}); ok {
			subpaths := sensitiveListToCtyPathList(currentPath.IndexInt(idx), value)
			paths = append(paths, subpaths...)
		}
	}
	return paths
}

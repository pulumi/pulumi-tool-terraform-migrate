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

package pkg

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/zclconf/go-cty/cty"
)

type terraformState struct {
	stateValue cty.Value
	meta       map[string]interface{}
}

var _ tfbridge.TerraformState = terraformState{}

func (t terraformState) Meta() map[string]interface{} {
	return t.meta
}

func (t terraformState) Object(schemaMap shim.SchemaMap) (map[string]interface{}, error) {
	res, err := objectFromCty(t.stateValue)
	if err != nil {
		return nil, err
	}
	// grpc servers add a "timeouts" key to compensate for infinite diffs; this is not needed in
	// the Pulumi projection.
	delete(res, schema.TimeoutsConfigKey)
	return res, nil
}

type setChecker struct{}

func (s setChecker) IsSet(ctx context.Context, v interface{}) ([]interface{}, bool) {
	return nil, false
}

func convertTFValueToPulumiValue(
	tfValue cty.Value, res shim.Resource, pulumiResource *info.Resource,
) (resource.PropertyMap, error) {
	instanceState := terraformState{
		stateValue: tfValue,
		// TODO: meta handling
		meta: nil,
	}

	// TODO: schema upgrades - what if the schema version is different?
	props, err := tfbridge.MakeTerraformResult(context.TODO(), setChecker{}, instanceState, res.Schema(), pulumiResource.Fields, nil, true)

	// TODO: fix raw state deltas
	// if err := tfbridge.RawStateInjectDelta(context.TODO(), res.Schema(), pulumiResource.Fields, props, res.SchemaType(), instanceState); err != nil {
	// 	return nil, err
	// }
	return props, err
}

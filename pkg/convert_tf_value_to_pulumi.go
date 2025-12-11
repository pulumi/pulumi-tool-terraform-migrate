package pkg

import (
	"context"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge"
	"github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfbridge/info"
	shim "github.com/pulumi/pulumi-terraform-bridge/v3/pkg/tfshim"
	"github.com/pulumi/pulumi-terraform-migrate/pkg/bridge"
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

// copied from https://github.com/pulumi/pulumi-terraform-bridge/blob/main/pkg/tfshim/sdk-v2/provider2.go#L139
func (t terraformState) Object(schemaMap shim.SchemaMap) (map[string]interface{}, error) {
	res, err := bridge.ObjectFromCty(t.stateValue)
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
		// TODO[pulumi/service#35118]: meta handling
		meta: nil,
	}

	// This assumes that the schema version of the resource state is exactly the same as the one in the provider.
	// TODO: add an assert for this.
	props, err := tfbridge.MakeTerraformResult(context.TODO(), setChecker{}, instanceState, res.Schema(), pulumiResource.Fields, nil, true)

	// TODO: fix raw state deltas
	// schemaType := bridge.ImpliedType(res.Schema(), false)
	// if err := tfbridge.RawStateInjectDelta(context.TODO(), res.Schema(), pulumiResource.Fields, props, valueshim.FromCtyType(schemaType), instanceState); err != nil {
	// 	return nil, err
	// }
	return props, err
}

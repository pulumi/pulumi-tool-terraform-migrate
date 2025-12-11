package pkg

import (
	hcty "github.com/hashicorp/go-cty/cty"
	"github.com/pulumi/pulumi/sdk/v3/go/common/util/contract"
	"github.com/zclconf/go-cty/cty"
)

// From https://github.com/pulumi/pulumi-terraform-bridge/blob/7f2e4c032f434dda686f173ec54e00eed99817fb/pkg/tfshim/sdk-v2/proposed_new.go#L37
func htype2ctype(t hcty.Type) cty.Type {
	// Used t.HasDynamicTypes() as an example of how to pattern-match types.
	switch {
	case t == hcty.NilType:
		return cty.NilType
	case t == hcty.DynamicPseudoType:
		return cty.DynamicPseudoType
	case t.IsPrimitiveType():
		switch {
		case t.Equals(hcty.Bool):
			return cty.Bool
		case t.Equals(hcty.String):
			return cty.String
		case t.Equals(hcty.Number):
			return cty.Number
		default:
			contract.Failf("Match failure on hcty.Type with t.IsPrimitiveType()")
		}
	case t.IsListType():
		return cty.List(htype2ctype(*t.ListElementType()))
	case t.IsMapType():
		return cty.Map(htype2ctype(*t.MapElementType()))
	case t.IsSetType():
		return cty.Set(htype2ctype(*t.SetElementType()))
	case t.IsObjectType():
		attrTypes := t.AttributeTypes()
		if len(attrTypes) == 0 {
			return cty.EmptyObject
		}
		converted := map[string]cty.Type{}
		for a, at := range attrTypes {
			converted[a] = htype2ctype(at)
		}
		return cty.Object(converted)
	case t.IsTupleType():
		elemTypes := t.TupleElementTypes()
		if len(elemTypes) == 0 {
			return cty.EmptyTuple
		}
		converted := []cty.Type{}
		for _, et := range elemTypes {
			converted = append(converted, htype2ctype(et))
		}
		return cty.Tuple(converted)
	case t.IsCapsuleType():
		contract.Assertf(false, "Capsule types are not yet supported")
	}
	contract.Assertf(false, "Match failure on hcty.Type: %v", t.GoString())
	return cty.NilType
}

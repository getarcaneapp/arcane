package convert

import (
	"github.com/zclconf/go-cty/cty"
)

func conversionToCapsule(inTy, outTy cty.Type, fn func(inTy cty.Type) func(cty.Value, cty.Path) (any, error)) conversion {
	rawConv := fn(inTy)
	if rawConv == nil {
		return nil
	}

	return func(in cty.Value, path cty.Path) (cty.Value, error) {
		rawV, err := rawConv(in, path)
		if err != nil {
			return cty.NilVal, err
		}
		return cty.CapsuleVal(outTy, rawV), nil
	}
}

func conversionFromCapsule(inTy, outTy cty.Type, fn func(outTy cty.Type) func(any, cty.Path) (cty.Value, error)) conversion {
	rawConv := fn(outTy)
	if rawConv == nil {
		return nil
	}

	return func(in cty.Value, path cty.Path) (cty.Value, error) {
		return rawConv(in.EncapsulatedValue(), path)
	}
}

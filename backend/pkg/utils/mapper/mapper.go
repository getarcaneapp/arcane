package mapper

import (
	"fmt"
	"net/netip"

	"emperror.dev/errors"
	"github.com/jinzhu/copier"
)

var typeConverters = []copier.TypeConverter{
	{
		SrcType: netip.Prefix{},
		DstType: "",
		Fn: func(src any) (any, error) {
			prefix, ok := src.(netip.Prefix)
			if !ok {
				return nil, fmt.Errorf("expected netip.Prefix, got %T", src)
			}
			if !prefix.IsValid() {
				return "", nil
			}

			return prefix.String(), nil
		},
	},
	{
		SrcType: netip.Addr{},
		DstType: "",
		Fn: func(src any) (any, error) {
			addr, ok := src.(netip.Addr)
			if !ok {
				return nil, fmt.Errorf("expected netip.Addr, got %T", src)
			}
			if !addr.IsValid() {
				return "", nil
			}

			return addr.String(), nil
		},
	},
}

func MapSlice[S any, D any](source []S) ([]D, error) {
	dest := make([]D, len(source))
	for i := range source {
		if err := MapStruct(source[i], &dest[i]); err != nil {
			return nil, errors.WrapIff(err, "failed to map field %d", i)
		}
	}
	return dest, nil
}

func MapOne[S any, D any](source S) (D, error) {
	var dest D
	if err := MapStruct(source, &dest); err != nil {
		return dest, err
	}
	return dest, nil
}

func MapStruct(source any, destination any) error {
	return copier.CopyWithOption(destination, source, copier.Option{
		DeepCopy:   true,
		Converters: typeConverters,
	})
}

// MapStructList maps a list of source structs to a list of destination structs
func MapStructList[S any, D any](source []S, destination *[]D) (err error) {
	*destination = make([]D, len(source))

	for i, item := range source {
		err = MapStruct(item, &((*destination)[i]))
		if err != nil {
			return errors.WrapIff(err, "failed to map field %d", i)
		}
	}
	return nil
}

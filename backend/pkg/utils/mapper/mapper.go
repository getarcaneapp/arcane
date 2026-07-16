package mapper

import (
	"fmt"

	"github.com/jinzhu/copier"
)

func MapSlice[S any, D any](source []S) ([]D, error) {
	dest := make([]D, len(source))
	for i := range source {
		if err := MapStruct(source[i], &dest[i]); err != nil {
			return nil, fmt.Errorf("failed to map field %d: %w", i, err)
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
		DeepCopy: true,
	})
}

// MapFunc maps a slice with an explicit per-item conversion function.
func MapFunc[S any, D any](source []S, convert func(S) D) []D {
	dest := make([]D, len(source))
	for i := range source {
		dest[i] = convert(source[i])
	}
	return dest
}

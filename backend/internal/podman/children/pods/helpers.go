package pods

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/go-units"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/podman"
)

func namespace(value string) *podman.PodNamespace {
	mode, rest, _ := strings.Cut(strings.TrimSpace(value), ":")
	switch mode {
	case "":
		return nil
	case "ns":
		return &podman.PodNamespace{NSMode: "path", Value: rest}
	default:
		return &podman.PodNamespace{NSMode: mode, Value: rest}
	}
}

func byteSize(field, value string) (*int64, error) {
	switch value = strings.TrimSpace(value); value {
	case "":
		return nil, nil
	case "-1":
		return new(int64(-1)), nil
	}
	size, err := units.RAMInBytes(value)
	if err != nil {
		return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid %s %q: %w", field, value, err))
	}
	return &size, nil
}

func deviceLimits[T any](field string, limits []types.PodDeviceLimit, build func(major, minor int64, value string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(limits))
	for _, limit := range limits {
		majorText, minorText, ok := strings.Cut(strings.TrimSpace(limit.Device), ":")
		major, majorErr := strconv.ParseInt(majorText, 10, 64)
		minor, minorErr := strconv.ParseInt(minorText, 10, 64)
		if !ok || majorErr != nil || minorErr != nil {
			return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid %s device %q: use MAJOR:MINOR", field, limit.Device))
		}
		item, err := build(major, minor, strings.TrimSpace(limit.Value))
		if err != nil {
			return nil, common.Classify(common.ErrBadRequest, fmt.Errorf("invalid %s value %q: %w", field, limit.Value, err))
		}
		out = append(out, item)
	}
	return out, nil
}

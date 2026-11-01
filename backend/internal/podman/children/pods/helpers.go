package pods

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/docker/go-units"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/podman"
	"getarcane.dev/types/v3"
)

var (
	podNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
	cpusetPattern  = regexp.MustCompile(`^\d+(-\d+)?(,\d+(-\d+)?)*$`)
	sizePattern    = regexp.MustCompile(`(?i)^\d+(\.\d+)?\s*[bkmg]?b?$`)
	idMapPattern   = regexp.MustCompile(`^\d+:\d+:\d+$`)
	devicePattern  = regexp.MustCompile(`^\d+:\d+$`)
)

func validatePodRequest(req types.PodCreateRequest) error {
	if !podNamePattern.MatchString(strings.TrimSpace(req.Name)) {
		return common.Classify(common.ErrValidation, &types.FieldError{
			Field: "name",
			Err:   errors.New("pod name must start with a letter or number and contain only letters, numbers, underscores, periods, and hyphens"),
		})
	}
	for _, cpuset := range []struct {
		field string
		value string
	}{
		{field: "cpusetCpus", value: req.CPUSetCPUs},
		{field: "cpusetMems", value: req.CPUSetMems},
	} {
		if cpuset.value != "" && !cpusetPattern.MatchString(cpuset.value) {
			return common.Classify(common.ErrValidation, &types.FieldError{Field: cpuset.field, Err: fmt.Errorf("invalid cpuset %q: use comma-separated numbers or ranges such as 0-3,5", cpuset.value)})
		}
	}
	return nil
}

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
	if err != nil || !sizePattern.MatchString(value) {
		return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: field, Err: fmt.Errorf("invalid size %q: use a value such as 512m or 2g", value)})
	}
	return &size, nil
}

func deviceLimits[T any](field string, limits []types.PodDeviceLimit, build func(major, minor int64, value string) (T, error)) ([]T, error) {
	out := make([]T, 0, len(limits))
	for _, limit := range limits {
		majorText, minorText, _ := strings.Cut(strings.TrimSpace(limit.Device), ":")
		major, majorErr := strconv.ParseInt(majorText, 10, 64)
		minor, minorErr := strconv.ParseInt(minorText, 10, 64)
		if !devicePattern.MatchString(strings.TrimSpace(limit.Device)) || majorErr != nil || minorErr != nil {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: field, Err: fmt.Errorf("invalid device %q: use MAJOR:MINOR", limit.Device)})
		}
		item, err := build(major, minor, strings.TrimSpace(limit.Value))
		if err != nil {
			return nil, common.Classify(common.ErrValidation, &types.FieldError{Field: field, Err: fmt.Errorf("invalid value %q for device %q: %w", limit.Value, limit.Device, err)})
		}
		out = append(out, item)
	}
	return out, nil
}

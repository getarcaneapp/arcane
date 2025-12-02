package buildflags

import (
	"strings"

	"github.com/distribution/reference"
	"github.com/pkg/errors"
)

func ParseContextNames(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}

		kv := strings.SplitN(value, "=", 2)
		if len(kv) != 2 {
			return nil, errors.Errorf("invalid context value: %s, expected key=value", value)
		}
		named, err := reference.ParseNormalizedNamed(kv[0])
		if err != nil {
			return nil, errors.Wrapf(err, "invalid context name %s", kv[0])
		}
		name := strings.TrimSuffix(reference.FamiliarString(named), ":latest")
		result[name] = kv[1]
	}
	return result, nil
}

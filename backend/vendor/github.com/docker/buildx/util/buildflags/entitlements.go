package buildflags

import (
	"github.com/moby/buildkit/util/entitlements"
)

func ParseEntitlements(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}

		if _, _, err := entitlements.Parse(v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

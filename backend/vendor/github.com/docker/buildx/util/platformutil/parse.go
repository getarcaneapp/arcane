package platformutil

import (
	"strings"

	"github.com/containerd/platforms"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

func Parse(platformsStr []string) ([]ocispecs.Platform, error) {
	if len(platformsStr) == 0 {
		return nil, nil
	}
	out := make([]ocispecs.Platform, 0, len(platformsStr))
	for _, s := range platformsStr {
		parts := strings.Split(s, ",")
		if len(parts) > 1 {
			p, err := Parse(parts)
			if err != nil {
				return nil, err
			}
			out = append(out, p...)
			continue
		}
		p, err := parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, platforms.Normalize(p))
	}
	return out, nil
}

func parse(in string) (ocispecs.Platform, error) {
	if strings.EqualFold(in, "local") {
		return platforms.DefaultSpec(), nil
	}
	return platforms.Parse(in)
}

func Dedupe(in []ocispecs.Platform) []ocispecs.Platform {
	m := map[string]struct{}{}
	out := make([]ocispecs.Platform, 0, len(in))
	for _, p := range in {
		p := platforms.Normalize(p)
		key := platforms.Format(p)
		if _, ok := m[key]; ok {
			continue
		}
		m[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

func FormatInGroups(gg ...[]ocispecs.Platform) []string {
	m := map[string]struct{}{}
	out := make([]string, 0, len(gg))
	for i, g := range gg {
		for _, p := range g {
			p := platforms.Normalize(p)
			key := platforms.Format(p)
			if _, ok := m[key]; ok {
				continue
			}
			m[key] = struct{}{}
			v := platforms.Format(p)
			if i == 0 {
				v += "*"
			}
			out = append(out, v)
		}
	}
	return out
}

func Format(in []ocispecs.Platform) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, platforms.Format(p))
	}
	return out
}

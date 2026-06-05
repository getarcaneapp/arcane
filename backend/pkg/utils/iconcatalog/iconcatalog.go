// Package iconcatalog resolves Arcane icon metadata into catalog URLs.
package iconcatalog

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	CatalogSelfhst        = "selfhst"
	CatalogDashboardIcons = "dashboard-icons"
	DefaultCatalog        = CatalogSelfhst
)

type IconSet struct {
	Icon  string
	Light string
	Dark  string
}

type ResolvedIconSet struct {
	IconLightURL string
	IconDarkURL  string
}

func (s IconSet) IsEmpty() bool {
	return strings.TrimSpace(s.Icon) == "" &&
		strings.TrimSpace(s.Light) == "" &&
		strings.TrimSpace(s.Dark) == ""
}

func (s IconSet) HasVariant() bool {
	return strings.TrimSpace(s.Light) != "" || strings.TrimSpace(s.Dark) != ""
}

func FirstNonEmpty(sets ...IconSet) IconSet {
	merged := IconSet{}
	fallback := ""

	for _, set := range sets {
		if fallback == "" {
			fallback = strings.TrimSpace(set.Icon)
		}
		if merged.Light == "" {
			merged.Light = strings.TrimSpace(set.Light)
		}
		if merged.Dark == "" {
			merged.Dark = strings.TrimSpace(set.Dark)
		}
	}

	if strings.TrimSpace(merged.Light) != "" || strings.TrimSpace(merged.Dark) != "" {
		if fallback != "" {
			if merged.Light == "" {
				merged.Light = fallback
			}
			if merged.Dark == "" {
				merged.Dark = fallback
			}
		}
		return merged
	}
	if fallback != "" {
		return IconSet{Icon: fallback}
	}
	return IconSet{}
}

func Resolve(catalog string, set IconSet) ResolvedIconSet {
	selectedCatalog := normalizeCatalogInternal(catalog)
	if strings.TrimSpace(set.Light) == "" && strings.TrimSpace(set.Dark) == "" {
		generic := resolveValueInternal(selectedCatalog, set.Icon, "")
		return ResolvedIconSet{
			IconLightURL: generic,
			IconDarkURL:  generic,
		}
	}

	light := strings.TrimSpace(set.Light)
	if light == "" {
		light = strings.TrimSpace(set.Icon)
	}
	dark := strings.TrimSpace(set.Dark)
	if dark == "" {
		dark = strings.TrimSpace(set.Icon)
	}

	return ResolvedIconSet{
		IconLightURL: resolveValueInternal(selectedCatalog, light, "light"),
		IconDarkURL:  resolveValueInternal(selectedCatalog, dark, "dark"),
	}
}

func normalizeCatalogInternal(catalog string) string {
	switch strings.ToLower(strings.TrimSpace(catalog)) {
	case CatalogDashboardIcons:
		return CatalogDashboardIcons
	default:
		return DefaultCatalog
	}
}

func resolveValueInternal(catalog, raw, variant string) string {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "data:") {
		return ""
	}
	if isAbsoluteHTTPURLInternal(value) {
		return value
	}

	slug := url.PathEscape(value)
	switch catalog {
	case CatalogDashboardIcons:
		return fmt.Sprintf("https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/svg/%s.svg", slug)
	default:
		if variant != "" {
			slug += "-" + variant
		}
		return fmt.Sprintf("https://cdn.jsdelivr.net/gh/selfhst/icons@main/svg/%s.svg", slug)
	}
}

func isAbsoluteHTTPURLInternal(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.IsAbs() && (u.Scheme == "http" || u.Scheme == "https")
}

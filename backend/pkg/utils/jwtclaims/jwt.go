package jwtclaims

import (
	"fmt"
	"maps"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/samber/mo"
)

// GetStringClaim extracts a string claim from a map
func GetStringClaim(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case string:
			return t
		case fmt.Stringer:
			return t.String()
		}
	}
	return ""
}

// GetBoolClaim extracts a boolean claim from a map
func GetBoolClaim(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case bool:
			return t
		case string:
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "1", "true", "yes", "y", "on":
				return true
			}
		case float64:
			return t != 0
		case int, int32, int64:
			return fmt.Sprintf("%v", t) != "0"
		}
	}
	return false
}

// StringSliceFromValue flattens a claim value into a slice of strings.
// Accepts string (trimmed, single element), []string, []any (string elements
// only), or nil. Unlike GetStringSliceClaim it never splits a single string
// into multiple values, so it is safe for claims whose values may legally
// contain separators (e.g. aud URIs).
func StringSliceFromValue(v any) []string {
	switch t := v.(type) {
	case []string:
		return stringSliceFromStringsInternal(t)
	case []any:
		return stringSliceFromInterfacesInternal(t)
	case string:
		return stringSliceFromStringsInternal([]string{t})
	}
	return nil
}

// GetStringSliceClaim extracts a string slice claim from a map. A single
// string value is split on commas or spaces (group/role claims are commonly
// delivered that way).
func GetStringSliceClaim(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	if t, ok := v.(string); ok {
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		// Support comma or space separated strings
		if strings.Contains(s, ",") {
			parts := strings.Split(s, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				if ps := strings.TrimSpace(p); ps != "" {
					out = append(out, ps)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		return strings.Fields(s)
	}
	return StringSliceFromValue(v)
}

func stringSliceFromStringsInternal[S ~string](items []S) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(string(item)); s != "" {
			out = append(out, s)
		}
	}
	return utils.UniqueNonEmptyStrings(out)
}

func stringSliceFromInterfacesInternal[T any](items []T) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := any(item).(string); ok {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return utils.UniqueNonEmptyStrings(out)
}

// ParseJWTClaims parses unverified JWT metadata for pre-verification routing.
func ParseJWTClaims(idToken string) map[string]any {
	token, err := jwt.ParseInsecure([]byte(idToken), jwt.WithStrictStringClaims(true))
	if err != nil {
		return nil
	}

	claims := maps.Collect(token.Claims())
	if audience, ok := token.Audience(); ok {
		claims[jwt.AudienceKey] = audience
	}
	return claims
}

// GetByPath extracts a value from a nested map using a dot-separated path
func GetByPath(m map[string]any, path string) mo.Option[any] {
	if m == nil {
		return mo.None[any]()
	}
	keys := strings.Split(path, ".")
	var cur any = m
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return mo.None[any]()
		}
		v, ok := obj[k]
		if !ok {
			return mo.None[any]()
		}
		cur = v
	}
	return mo.Some(cur)
}

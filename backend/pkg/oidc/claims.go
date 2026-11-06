package oidc

import (
	"fmt"
	"maps"
	"strings"

	"github.com/lestrrat-go/jwx/v4/jwt"
	"github.com/samber/mo"

	"kit.getarcane.app"
)

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

func GetStringSliceClaim(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	if t, localOk := v.(string); localOk {
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		if out := kit.TrimNonEmpty(strings.Split(s, ",")); strings.Contains(s, ",") && len(out) > 0 {
			return out
		}
		return strings.Fields(s)
	}
	return kit.Unique(kit.TrimNonEmpty(kit.Collect(v, func(item any) string { return kit.As(item, "") })))
}

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

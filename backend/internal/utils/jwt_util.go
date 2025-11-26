package utils

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	case string:
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
	return nil
}

func CheckOrGenerateJwtSecret(jwtSecret string) []byte {
	var secretBytes []byte
	if jwtSecret != "" {
		secretBytes = []byte(jwtSecret)
		return secretBytes
	} else {
		secretBytes = make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			panic(fmt.Errorf("failed to generate random JWT secret: %w", err))
		}
	}
	return nil
}

func ParseJWTClaims(idToken string) map[string]any {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func GetByPath(m map[string]any, path string) (any, bool) {
	if m == nil {
		return nil, false
	}
	keys := strings.Split(path, ".")
	var cur any = m
	for _, k := range keys {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := obj[k]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func EvalMatch(v any, want []string) bool {
	if len(want) == 0 {
		if b, ok := v.(bool); ok {
			return b
		}
		return false
	}
	wantSet := map[string]struct{}{}
	for _, s := range want {
		wantSet[strings.ToLower(s)] = struct{}{}
	}
	switch x := v.(type) {
	case string:
		_, ok := wantSet[strings.ToLower(x)]
		return ok
	case []any:
		for _, it := range x {
			if s, ok := it.(string); ok {
				if _, ok2 := wantSet[strings.ToLower(s)]; ok2 {
					return true
				}
			}
		}
		return false
	case bool:
		_, ok := wantSet[strings.ToLower(fmt.Sprintf("%v", x))]
		return ok
	default:
		return false
	}
}

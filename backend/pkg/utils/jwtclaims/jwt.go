package jwtclaims

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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

// GetStringSliceClaim extracts a string slice claim from a map
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

const (
	// KnownInsecureJWTSecret is the placeholder shipped in config.go's struct
	// tag; it must never sign real tokens. Keep in sync with the `default:` tag
	// on Config.JWTSecret.
	KnownInsecureJWTSecret = "default-jwt-secret-change-me" // #nosec G101: public placeholder config default, intentionally rejected for production signing
	// MinJWTSecretLength matches the 32-byte floor enforced for ENCRYPTION_KEY.
	MinJWTSecretLength = 32
)

// CheckOrGenerateJwtSecret returns the HMAC signing key for JWTs.
//
// When requireExplicit is true (production manager), a real secret is mandatory:
// an empty, default, or too-short JWT_SECRET panics at startup — mirroring the
// ENCRYPTION_KEY guard in go.getarcane.app/sys/crypto. Otherwise (development / agent mode)
// a random per-boot key is generated when none (or only the public default) is
// configured, so the public default never becomes a live signing key.
func CheckOrGenerateJwtSecret(jwtSecret string, requireExplicit bool) []byte {
	isDefault := jwtSecret == "" || jwtSecret == KnownInsecureJWTSecret

	if requireExplicit {
		if isDefault {
			panic("JWT_SECRET is required in production. Set JWT_SECRET to a unique " +
				"random value of at least 32 characters (e.g. `openssl rand -base64 32`).")
		}
		if len(jwtSecret) < MinJWTSecretLength {
			panic(fmt.Sprintf("JWT_SECRET must be at least %d characters (got %d).",
				MinJWTSecretLength, len(jwtSecret)))
		}
		return []byte(jwtSecret)
	}

	if isDefault {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			panic(fmt.Errorf("failed to generate random JWT secret: %w", err))
		}
		return secretBytes
	}
	return []byte(jwtSecret)
}

// ParseJWTClaims decodes and unmarshals the payload part of a JWT
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

// GetByPath extracts a value from a nested map using a dot-separated path
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

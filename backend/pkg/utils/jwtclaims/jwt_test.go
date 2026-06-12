package jwtclaims

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckOrGenerateJwtSecret(t *testing.T) {
	// Production (requireExplicit) must reject empty, the public default, and short secrets.
	assert.Panics(t, func() { CheckOrGenerateJwtSecret("", true) })
	assert.Panics(t, func() { CheckOrGenerateJwtSecret(KnownInsecureJWTSecret, true) })
	assert.Panics(t, func() { CheckOrGenerateJwtSecret("too-short", true) })

	// Production accepts a real secret verbatim.
	good := "a-sufficiently-long-jwt-secret-value-1234" // >= 32 chars
	assert.Equal(t, []byte(good), CheckOrGenerateJwtSecret(good, true))

	// Development / agent mode replaces the public default with a random key.
	got := CheckOrGenerateJwtSecret(KnownInsecureJWTSecret, false)
	assert.Len(t, got, 32)
	assert.NotEqual(t, []byte(KnownInsecureJWTSecret), got)
}

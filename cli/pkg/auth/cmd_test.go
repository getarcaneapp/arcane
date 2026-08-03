package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractDeviceAuthErrorCodeClassifiesMFARequired(t *testing.T) {
	// The device-token handler refuses MFA accounts with a huma error, so the
	// code arrives inside "detail"; the bare and "error" forms are covered too
	// because the extractor is the only classifier for this endpoint.
	require.Equal(t, "mfa_required", extractDeviceAuthErrorCode(`{"detail":"mfa_required"}`))
	require.Equal(t, "mfa_required", extractDeviceAuthErrorCode(`{"error":"mfa_required"}`))
	require.Equal(t, "mfa_required", extractDeviceAuthErrorCode("mfa_required"))
	require.Equal(t, "access_denied", extractDeviceAuthErrorCode(`{"detail":"access_denied"}`))
}

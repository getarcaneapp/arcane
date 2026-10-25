package cookie

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCookiesUnderBasePath(t *testing.T) {
	SetBasePath("/arcane")
	t.Cleanup(func() { SetBasePath("") })

	token := BuildTokenCookieStringFor(3600, "value", true)
	require.NotEmpty(t, token)
	assert.True(t, strings.HasPrefix(token[0], "token=value"), token[0])
	assert.Contains(t, token[0], "Path=/arcane/")
	assert.Contains(t, token[0], "Secure")

	refresh := BuildRefreshCookieStringFor(3600, "r", true)
	assert.True(t, strings.HasPrefix(refresh, RefreshCookieName+"="), refresh)
	assert.Contains(t, refresh, "Path=/arcane/api/auth")

	// Secure tokens use the unprefixed name under a base path; the reader still finds them.
	got, err := GetTokenCookieFromHeader("token=value")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	// A root-scoped __Host-token from an earlier deployment at "/" does not shadow it.
	got, err = GetTokenCookieFromHeader(TokenCookieName + "=stale; token=value")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	// Logging out also clears cookies left by a deployment served from the root.
	cleared := strings.Join(BuildClearTokenCookieStringsFor(true), "\n")
	assert.Contains(t, cleared, "Path=/arcane/")
	assert.Contains(t, cleared, TokenCookieName+"=; Path=/;")
}

func TestCookiesAtRootAreUnchanged(t *testing.T) {
	SetBasePath("")
	token := BuildTokenCookieStringFor(3600, "value", true)
	assert.True(t, strings.HasPrefix(token[0], TokenCookieName+"=value"), token[0])
	assert.Contains(t, token[0], "Path=/;")
	assert.Contains(t, BuildRefreshCookieStringFor(3600, "r", true), "Path=/api/auth")
}

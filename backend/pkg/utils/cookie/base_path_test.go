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
	assert.True(t, strings.HasPrefix(token[0], "__Secure-token=value"), token[0])
	assert.Contains(t, token[0], "Path=/arcane/")
	assert.Contains(t, token[0], "Secure")

	refresh := BuildRefreshCookieStringFor(3600, "r", true)
	assert.True(t, strings.HasPrefix(refresh, RefreshCookieName+"="), refresh)
	assert.Contains(t, refresh, "Path=/arcane/api/auth")

	got, err := GetTokenCookieFromHeader("__Secure-token=value")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	got, err = GetTokenCookieFromHeader("__Secure-token=value; " + TokenCookieName + "=stale; __Secure-token=other-app")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	cleared := strings.Join(BuildClearTokenCookieStringsFor(true), "\n")
	assert.Contains(t, cleared, "__Secure-token=; Path=/arcane/;")
	assert.NotContains(t, cleared, "Path=/;")
	assert.NotContains(t, strings.Join(BuildClearRefreshCookieStringsFor(true), "\n"), "Path=/api/auth;")
}

func TestCookiesAtRootAreUnchanged(t *testing.T) {
	SetBasePath("")
	token := BuildTokenCookieStringFor(3600, "value", true)
	assert.True(t, strings.HasPrefix(token[0], TokenCookieName+"=value"), token[0])
	assert.Contains(t, token[0], "Path=/;")
	assert.Contains(t, BuildRefreshCookieStringFor(3600, "r", true), "Path=/api/auth")
}

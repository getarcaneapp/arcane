package authz

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

func TestForwardedAuthzRoundTrip(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddGlobal(PermDiagnosticsRead, PermContainersRead)
	ps.AddEnv("env-1", PermContainersList, PermContainersStart)
	ps.AddEnv("env-2", PermImagesList)
	actor := types.Actor{
		ID:       "user-1",
		Username: "alice",
	}

	raw, err := EncodeForwardedAuthz(ps, "env-1", actor)
	require.NoError(t, err)

	decoded, gotActor, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
	require.NoError(t, err)
	require.Equal(t, actor, gotActor)
	require.False(t, decoded.Sudo)
	require.False(t, decoded.IsGlobalAdmin())
	require.Equal(t, map[string]struct{}{PermDiagnosticsRead: {}}, decoded.Global)

	local := types.LocalDockerEnvironmentID
	require.Equal(t, []string{local}, slices.Collect(maps.Keys(decoded.PerEnv)))
	require.Contains(t, decoded.PerEnv[local], PermContainersList)
	for _, permission := range []string{PermContainersRead, PermContainersList, PermContainersStart} {
		require.True(t, decoded.Allows(permission, local), permission)
	}
	require.True(t, decoded.Allows(PermDiagnosticsRead, local))
	require.False(t, decoded.Allows(PermImagesList, local))
	require.False(t, decoded.Allows(PermContainersList, "env-1"))
}

func TestForwardedAuthzPayload(t *testing.T) {
	ps := NewPermissionSet()
	ps.AddEnv("env-1", PermContainersList, PermVolumesList)

	raw, err := EncodeForwardedAuthz(ps, "env-1", types.Actor{ID: "user-1"})
	require.NoError(t, err)
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	require.NoError(t, err)
	var forwarded ForwardedAuthz
	require.NoError(t, json.Unmarshal(payload, &forwarded))

	require.Equal(t, forwardedAuthzVersion, forwarded.V)
	require.NotZero(t, forwarded.IssuedAt)
	require.Equal(t, []string{PermContainersList, PermVolumesList}, forwarded.Perms)
}

func TestForwardedAuthzAdminIsNotSudo(t *testing.T) {
	for name, ps := range map[string]*PermissionSet{
		"sudo": SudoPermissionSet(),
		"admin": func() *PermissionSet {
			admin := NewPermissionSet()
			admin.AddGlobal(PermAll)
			return admin
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := EncodeForwardedAuthz(ps, "env-1", types.Actor{ID: "user-1"})
			require.NoError(t, err)
			payload, err := base64.RawURLEncoding.DecodeString(raw)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload, &fields))
			require.JSONEq(t, `true`, string(fields["adm"]))
			require.NotContains(t, fields, "p")

			decoded, _, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
			require.NoError(t, err)
			require.False(t, decoded.Sudo)
			require.True(t, decoded.IsGlobalAdmin())
			require.Contains(t, decoded.Global, PermAll)
		})
	}
}

func TestForwardedAuthzOversize(t *testing.T) {
	_, _, err := DecodeForwardedAuthz(strings.Repeat("a", forwardedAuthzMaxSize+1), types.LocalDockerEnvironmentID)
	require.ErrorIs(t, err, ErrForwardedAuthzTooLarge)
}

func TestDecodeForwardedAuthzRejectsMalformed(t *testing.T) {
	encode := func(payload string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(payload))
	}
	tests := []struct {
		name  string
		raw   string
		envID string
	}{
		{name: "bad version", raw: encode(`{"v":2,"act":{"id":"user-1"}}`), envID: "0"},
		{name: "missing version", raw: encode(`{"act":{"id":"user-1"}}`), envID: "0"},
		{name: "bad base64", raw: "!!!", envID: "0"},
		{name: "bad json", raw: encode(`{"v":`), envID: "0"},
		{name: "missing local environment", raw: encode(`{"v":1,"act":{"id":"user-1"}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := DecodeForwardedAuthz(tt.raw, tt.envID)
			require.ErrorIs(t, err, ErrForwardedAuthzInvalid)
		})
	}
}

func TestDecodeForwardedAuthzKeepsOrgLevelPermissionsGlobal(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"act":{"id":"user-1"},` +
		`"p":["users:delete","containers:list","made:up"]}`))

	decoded, _, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
	require.NoError(t, err)
	require.True(t, decoded.Allows(PermContainersList, types.LocalDockerEnvironmentID))
	require.NotContains(t, decoded.PerEnv[types.LocalDockerEnvironmentID], PermUsersDelete)
	require.NotContains(t, decoded.PerEnv[types.LocalDockerEnvironmentID], "made:up")
	require.Equal(t, map[string]struct{}{PermUsersDelete: {}}, decoded.Global)
}

func TestEncodeForwardedAuthzRequiresPermissionSet(t *testing.T) {
	_, err := EncodeForwardedAuthz(nil, "env-1", types.Actor{})
	require.ErrorIs(t, err, ErrForwardedAuthzInvalid)
}

package authz

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"
)

func TestForwardedAuthzRoundTrip(t *testing.T) {
	web := types.ResourceSelector{Labels: map[string]string{"team": "web"}}
	ps := &PermissionSet{SubjectID: "manager-subject"}
	ps.AddGlobal(PermDiagnosticsRead, PermContainersRead)
	ps.AddEnv("env-1", PermContainersList, PermContainersStart)
	ps.AddEnv("env-2", PermImagesList)
	ps.AddScopedEnv("env-1", web, PermContainersStop, PermContainersRestart)
	actor := types.Actor{
		ID:       "user-1",
		Username: "alice",
	}

	raw, err := EncodeForwardedAuthz(ps, "env-1", actor)
	require.NoError(t, err)
	require.False(t, strings.HasPrefix(raw, forwardedAuthzGzipPrefix))

	decoded, gotActor, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
	require.NoError(t, err)
	require.Equal(t, actor, gotActor)
	require.Equal(t, "user-1", decoded.SubjectID)
	require.False(t, decoded.Sudo)
	require.False(t, decoded.IsGlobalAdmin())
	require.Equal(t, map[string]struct{}{PermDiagnosticsRead: {}}, decoded.Global)

	local := types.LocalDockerEnvironmentID
	require.Equal(t, []string{local}, slices.Collect(maps.Keys(decoded.PerEnv)))
	require.Contains(t, decoded.PerEnv[local], PermContainersList)
	for _, permission := range []string{PermContainersRead, PermContainersList, PermContainersStart} {
		require.True(t, decoded.Scope(permission, local).IsUnrestricted(), permission)
	}
	require.True(t, decoded.Allows(PermDiagnosticsRead, local))
	require.False(t, decoded.Allows(PermImagesList, local))
	require.False(t, decoded.Allows(PermContainersList, "env-1"))

	stop := decoded.Scope(PermContainersStop, local)
	require.False(t, stop.IsUnrestricted())
	require.True(t, stop.Matches(ResourceAttributes{Labels: map[string]string{"team": "web"}}))
	require.False(t, stop.Matches(ResourceAttributes{Labels: map[string]string{"team": "db"}}))
	require.Equal(t, decoded.Scope(PermContainersStop, local), decoded.Scope(PermContainersRestart, local))
}

func TestForwardedAuthzGroupsGrantsBySelectorSet(t *testing.T) {
	web := types.ResourceSelector{Labels: map[string]string{"team": "web"}}
	ps := NewPermissionSet()
	ps.AddEnv("env-1", PermContainersList, PermVolumesList)
	ps.AddScopedEnv("env-1", web, PermContainersStart, PermContainersStop)

	raw, err := EncodeForwardedAuthz(ps, "env-1", types.Actor{ID: "user-1"})
	require.NoError(t, err)
	payload, err := decodeForwardedPayload(raw)
	require.NoError(t, err)
	var forwarded ForwardedAuthz
	require.NoError(t, json.Unmarshal(payload, &forwarded))

	require.Equal(t, forwardedAuthzVersion, forwarded.V)
	require.NotZero(t, forwarded.IssuedAt)
	require.Equal(t, []ForwardedGrant{
		{P: []string{PermContainersList, PermVolumesList}},
		{
			P: []string{PermContainersStart, PermContainersStop},
			S: []types.ResourceSelector{web},
		},
	}, forwarded.Grants)
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
			payload, err := decodeForwardedPayload(raw)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(payload, &fields))
			require.JSONEq(t, `true`, string(fields["adm"]))
			require.NotContains(t, fields, "g")

			decoded, _, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
			require.NoError(t, err)
			require.False(t, decoded.Sudo)
			require.True(t, decoded.IsGlobalAdmin())
			require.Contains(t, decoded.Global, PermAll)
		})
	}
}

func TestForwardedAuthzGzipThreshold(t *testing.T) {
	names := make([]string, 400)
	for i := range names {
		names[i] = fmt.Sprintf("service-%04d", i)
	}
	ps := NewPermissionSet()
	ps.AddScopedEnv("env-1", types.ResourceSelector{Names: names}, PermContainersStart)

	raw, err := EncodeForwardedAuthz(ps, "env-1", types.Actor{ID: "user-1"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(raw, forwardedAuthzGzipPrefix))
	require.LessOrEqual(t, len(raw), forwardedAuthzMaxSize)

	decoded, _, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
	require.NoError(t, err)
	require.True(t, decoded.Scope(PermContainersStart, types.LocalDockerEnvironmentID).Matches(ResourceAttributes{Names: []string{"service-0399"}}))
}

func TestForwardedAuthzOversize(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	names := make([]string, 1200)
	for i := range names {
		names[i] = fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64())
	}
	ps := NewPermissionSet()
	ps.AddScopedEnv("env-1", types.ResourceSelector{Names: names}, PermContainersStart)

	_, err := EncodeForwardedAuthz(ps, "env-1", types.Actor{ID: "user-1"})
	require.ErrorIs(t, err, ErrForwardedAuthzTooLarge)

	_, _, err = DecodeForwardedAuthz(strings.Repeat("a", forwardedAuthzMaxSize+1), types.LocalDockerEnvironmentID)
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
		{name: "bad gzip", raw: forwardedAuthzGzipPrefix + encode("not gzip"), envID: "0"},
		{name: "invalid selector", raw: encode(`{"v":1,"act":{"id":"user-1"},"g":[{"p":["containers:start"],"s":[{}]}]}`), envID: "0"},
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
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"act":{"id":"user-1"},"g":[` +
		`{"p":["users:delete","containers:list","made:up"]},` +
		`{"p":["settings:read","containers:start"],"s":[{"names":["web"]}]}]}`))

	decoded, _, err := DecodeForwardedAuthz(raw, types.LocalDockerEnvironmentID)
	require.NoError(t, err)
	require.True(t, decoded.Allows(PermContainersList, types.LocalDockerEnvironmentID))
	require.NotContains(t, decoded.PerEnv[types.LocalDockerEnvironmentID], PermUsersDelete)
	require.NotContains(t, decoded.PerEnv[types.LocalDockerEnvironmentID], "made:up")
	require.Equal(t, map[string]struct{}{PermUsersDelete: {}}, decoded.Global)
	require.False(t, decoded.Allows(PermSettingsRead, types.LocalDockerEnvironmentID))
	require.True(t, decoded.Scope(PermContainersStart, types.LocalDockerEnvironmentID).Matches(ResourceAttributes{Names: []string{"web"}}))
}

func TestEncodeForwardedAuthzRequiresPermissionSet(t *testing.T) {
	_, err := EncodeForwardedAuthz(nil, "env-1", types.Actor{})
	require.ErrorIs(t, err, ErrForwardedAuthzInvalid)
}

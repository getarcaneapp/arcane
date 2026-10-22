package authz

import (
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func TestCanDefineRole(t *testing.T) {
	admin := NewPermissionSet()
	admin.AddGlobal(PermAll)

	global := NewPermissionSet()
	global.AddGlobal(PermContainersStart, PermRolesCreate)

	scopedGlobal := NewPermissionSet()
	scopedGlobal.AddScopedGlobal(types.ResourceSelector{Labels: map[string]string{"team": "web"}}, PermContainersStart)

	envOnly := NewPermissionSet()
	envOnly.AddEnv("env-1", PermContainersStart)

	tests := []struct {
		name    string
		caller  *PermissionSet
		perms   []string
		wantErr bool
	}{
		{name: "admin defines anything", caller: admin, perms: []string{PermContainersStart, PermUsersDelete}},
		{name: "admin defines wildcard", caller: admin, perms: []string{PermAll}},
		{name: "sudo defines wildcard", caller: SudoPermissionSet(), perms: []string{PermAll}},
		{name: "global holder", caller: global, perms: []string{PermContainersStart}},
		{name: "global holder lacking perm", caller: global, perms: []string{PermContainersStop}, wantErr: true},
		{name: "non-admin defines wildcard", caller: global, perms: []string{PermAll}, wantErr: true},
		{name: "scoped global holder", caller: scopedGlobal, perms: []string{PermContainersStart}, wantErr: true},
		{name: "env-only holder", caller: envOnly, perms: []string{PermContainersStart}, wantErr: true},
		{name: "nil caller", perms: []string{PermContainersStart}, wantErr: true},
		{name: "nil caller empty role", perms: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanDefineRole(tt.caller, tt.perms)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, common.ErrRolePermissionEscalation)
			require.ErrorIs(t, err, common.ErrForbidden)
		})
	}
}

func TestCanBind(t *testing.T) {
	web := types.ResourceSelector{Labels: map[string]string{"team": "web"}}
	webFront := types.ResourceSelector{Labels: map[string]string{"team": "web", "tier": "front"}}
	db := types.ResourceSelector{Labels: map[string]string{"team": "db"}}

	admin := NewPermissionSet()
	admin.AddGlobal(PermAll)

	envHolder := NewPermissionSet()
	envHolder.AddEnv("env-1", PermContainersStart, PermContainersList)

	scopedHolder := NewPermissionSet()
	scopedHolder.AddScopedEnv("env-1", web, PermContainersStart)

	ownerHolder := NewPermissionSet()
	ownerHolder.AddScopedGlobal(types.ResourceSelector{OwnerOnly: true}, PermContainersStart)

	tests := []struct {
		name    string
		caller  *PermissionSet
		perms   []string
		envID   string
		sel     *types.ResourceSelector
		wantErr bool
	}{
		{name: "admin binds admin", caller: admin, perms: []string{PermAll}},
		{name: "env holder binds wildcard", caller: envHolder, perms: []string{PermAll}, envID: "env-1", wantErr: true},
		{name: "env holder unscoped in env", caller: envHolder, perms: []string{PermContainersStart}, envID: "env-1"},
		{name: "env holder in other env", caller: envHolder, perms: []string{PermContainersStart}, envID: "env-2", wantErr: true},
		{name: "env holder globally", caller: envHolder, perms: []string{PermContainersStart}, wantErr: true},
		{name: "env holder scoped in env", caller: envHolder, perms: []string{PermContainersStart}, envID: "env-1", sel: &db},
		{name: "scoped holder unscoped", caller: scopedHolder, perms: []string{PermContainersStart}, envID: "env-1", wantErr: true},
		{name: "scoped holder same selector", caller: scopedHolder, perms: []string{PermContainersStart}, envID: "env-1", sel: &web},
		{name: "scoped holder narrower selector", caller: scopedHolder, perms: []string{PermContainersStart}, envID: "env-1", sel: &webFront},
		{name: "scoped holder broader selector", caller: scopedHolder, perms: []string{PermContainersStart}, envID: "env-1", sel: &types.ResourceSelector{Projects: []string{"shop"}}, wantErr: true},
		{name: "scoped holder other selector", caller: scopedHolder, perms: []string{PermContainersStart}, envID: "env-1", sel: &db, wantErr: true},
		{name: "scoped holder dropped permissions are ignored", caller: scopedHolder, perms: []string{PermContainersStart, PermUsersDelete}, envID: "env-1", sel: &web},
		{name: "owner-only holder cannot delegate", caller: ownerHolder, perms: []string{PermContainersStart}, envID: "env-1", sel: &types.ResourceSelector{OwnerOnly: true}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanBind(tt.caller, tt.perms, tt.envID, tt.sel)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, common.ErrRolePermissionEscalation)
		})
	}
}

func TestEffectiveBindingPermissions(t *testing.T) {
	perms := []string{PermAll, PermContainersStart, PermProjectsDeploy, PermImagesPrune, PermUsersList}

	granted, dropped := EffectiveBindingPermissions(perms, nil)
	require.Equal(t, perms, granted)
	require.Empty(t, dropped)

	granted, dropped = EffectiveBindingPermissions(perms, &types.ResourceSelector{Names: []string{"web-*"}})
	require.Equal(t, []string{PermContainersStart, PermProjectsDeploy}, granted)
	require.Equal(t, []string{PermAll, PermImagesPrune, PermUsersList}, dropped)

	granted, dropped = EffectiveBindingPermissions(perms, &types.ResourceSelector{Labels: map[string]string{"team": "web"}})
	require.Equal(t, []string{PermContainersStart}, granted)
	require.Equal(t, []string{PermAll, PermProjectsDeploy, PermImagesPrune, PermUsersList}, dropped)
}

func TestSelectorIsSubset(t *testing.T) {
	tests := []struct {
		name   string
		narrow types.ResourceSelector
		broad  types.ResourceSelector
		want   bool
	}{
		{name: "equal labels", narrow: types.ResourceSelector{Labels: map[string]string{"a": "1"}}, broad: types.ResourceSelector{Labels: map[string]string{"a": "1"}}, want: true},
		{name: "superset labels", narrow: types.ResourceSelector{Labels: map[string]string{"a": "1", "b": "2"}}, broad: types.ResourceSelector{Labels: map[string]string{"a": "1"}}, want: true},
		{name: "missing label", narrow: types.ResourceSelector{Labels: map[string]string{"b": "2"}}, broad: types.ResourceSelector{Labels: map[string]string{"a": "1"}}},
		{name: "project subset", narrow: types.ResourceSelector{Projects: []string{"a"}}, broad: types.ResourceSelector{Projects: []string{"a", "b"}}, want: true},
		{name: "project superset", narrow: types.ResourceSelector{Projects: []string{"a", "c"}}, broad: types.ResourceSelector{Projects: []string{"a", "b"}}},
		{name: "projects required", narrow: types.ResourceSelector{Names: []string{"web"}}, broad: types.ResourceSelector{Projects: []string{"a"}}},
		{name: "equal name glob", narrow: types.ResourceSelector{Names: []string{"web-*"}}, broad: types.ResourceSelector{Names: []string{"web-*"}}, want: true},
		{name: "name prefix", narrow: types.ResourceSelector{Names: []string{"web-api-*", "web-1"}}, broad: types.ResourceSelector{Names: []string{"web-*"}}, want: true},
		{name: "name outside prefix", narrow: types.ResourceSelector{Names: []string{"db-*"}}, broad: types.ResourceSelector{Names: []string{"web-*"}}},
		{name: "name prefix crosses path", narrow: types.ResourceSelector{Names: []string{"ghcr.io/org/*"}}, broad: types.ResourceSelector{Names: []string{"ghcr.io/*"}}},
		{name: "broad pattern with meta", narrow: types.ResourceSelector{Names: []string{"wxb-1"}}, broad: types.ResourceSelector{Names: []string{"w?b-*"}}},
		{name: "narrow adds owner", narrow: types.ResourceSelector{Labels: map[string]string{"a": "1"}, OwnerOnly: true}, broad: types.ResourceSelector{Labels: map[string]string{"a": "1"}}, want: true},
		{name: "broad owner never delegates", narrow: types.ResourceSelector{OwnerOnly: true}, broad: types.ResourceSelector{OwnerOnly: true}},
		{name: "empty broad", narrow: types.ResourceSelector{Names: []string{"web"}}, broad: types.ResourceSelector{}},
		{name: "empty narrow", narrow: types.ResourceSelector{}, broad: types.ResourceSelector{Names: []string{"*"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, SelectorIsSubset(tt.narrow, tt.broad))
		})
	}
}

package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
)

func TestCanDefineRole(t *testing.T) {
	admin := NewPermissionSet()
	admin.AddGlobal(PermAll)

	global := NewPermissionSet()
	global.AddGlobal(PermContainersStart, PermRolesCreate)

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
	admin := NewPermissionSet()
	admin.AddGlobal(PermAll)

	envHolder := NewPermissionSet()
	envHolder.AddEnv("env-1", PermContainersStart, PermContainersList)

	tests := []struct {
		name    string
		caller  *PermissionSet
		perms   []string
		envID   string
		wantErr bool
	}{
		{name: "admin binds admin", caller: admin, perms: []string{PermAll}},
		{name: "env holder binds wildcard", caller: envHolder, perms: []string{PermAll}, envID: "env-1", wantErr: true},
		{name: "env holder unscoped in env", caller: envHolder, perms: []string{PermContainersStart}, envID: "env-1"},
		{name: "env holder in other env", caller: envHolder, perms: []string{PermContainersStart}, envID: "env-2", wantErr: true},
		{name: "env holder globally", caller: envHolder, perms: []string{PermContainersStart}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CanBind(tt.caller, tt.perms, tt.envID)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, common.ErrRolePermissionEscalation)
		})
	}
}

package binding

import (
	"context"
	"errors"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

func builtInRoleLookup(_ context.Context, roleID string) (types.Role, error) {
	definition, ok := authz.BuiltInRole(roleID)
	if !ok {
		return types.Role{}, common.ErrRoleNotFound
	}
	return types.Role{
		ID:          definition.ID,
		Name:        definition.Name,
		Permissions: definition.Permissions,
		BuiltIn:     true,
	}, nil
}

func setupBindingService(t *testing.T, userBindings func(context.Context, string) ([]types.RoleBindingSummary, error)) (*BindingService, *gorm.DB, *[]string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		"CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT)",
		"CREATE TABLE groups (id TEXT PRIMARY KEY, name TEXT)",
		"CREATE TABLE environments (id TEXT PRIMARY KEY, name TEXT)",
		"CREATE TABLE federated_credentials (id TEXT PRIMARY KEY, identity_user_id TEXT)",
		"CREATE TABLE roles (id TEXT PRIMARY KEY, name TEXT)",
		"CREATE TABLE role_bindings (id TEXT PRIMARY KEY, user_id TEXT, group_id TEXT, role_id TEXT NOT NULL, environment_id TEXT, " +
			"selector TEXT, selector_hash TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT 'manual', created_by TEXT, created_at DATETIME, updated_at DATETIME)",
		"INSERT INTO users (id, username) VALUES ('alice', 'alice'), ('bob', 'bob')",
		"INSERT INTO groups (id, name) VALUES ('ops', 'Operators')",
		"INSERT INTO environments (id, name) VALUES ('env-1', 'Production')",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	require.NoError(t, db.AutoMigrate(&audit.AuditLog{}))
	invalidated := []string{}
	service := NewBindingService(
		&database.DB{DB: db},
		RoleLookup{
			Role: builtInRoleLookup,
			LockRole: func(ctx context.Context, _ *gorm.DB, roleID string) (types.Role, error) {
				return builtInRoleLookup(ctx, roleID)
			},
			UserBindings: userBindings,
		},
		Guard{
			Lock: func(*gorm.DB) error {
				return nil
			},
			AssertAdmin: func(_ context.Context, tx *gorm.DB) error {
				var admins int64
				if countErr := tx.Table("role_bindings").
					Where("role_id = ? AND environment_id IS NULL AND selector_hash = ''", authz.BuiltInRoleAdmin).
					Count(&admins).Error; countErr != nil {
					return countErr
				}
				if admins == 0 {
					return common.ErrNoGlobalAdminRemains
				}
				return nil
			},
			Authorize: func(ctx context.Context, _ *gorm.DB, caller *authz.PermissionSet, roleID string, environmentID *string, selector *types.ResourceSelector) error {
				role, lookupErr := builtInRoleLookup(ctx, roleID)
				if lookupErr != nil {
					return lookupErr
				}
				return authz.CanBind(caller, role.Permissions, kit.FromPtr(environmentID), selector)
			},
			Invalidate: func(_ context.Context, userID, groupID *string) {
				invalidated = append(invalidated, kit.FromPtr(kit.Ternary(userID != nil, userID, groupID)))
			},
		},
	)
	return service, db, &invalidated
}

func noUserBindings(context.Context, string) ([]types.RoleBindingSummary, error) {
	return nil, errors.New("not used")
}

func TestCreateAndDeleteBindingRequireCallerCoverage(t *testing.T) {
	ctx := t.Context()
	service, _, invalidated := setupBindingService(t, noUserBindings)
	editor, ok := authz.BuiltInRole(authz.BuiltInRoleEditor)
	require.True(t, ok)
	envEditor := authz.NewPermissionSet()
	envEditor.AddEnv("env-1", editor.Permissions...)

	created, dropped, err := service.Create(ctx, envEditor, "alice", types.CreateRoleBinding{
		SubjectType:   SubjectUser,
		SubjectID:     "bob",
		RoleID:        authz.BuiltInRoleEditor,
		EnvironmentID: new("env-1"),
	})
	require.NoError(t, err)
	require.Empty(t, dropped)
	require.Equal(t, "bob", created.SubjectName)
	require.Equal(t, "Editor", created.RoleName)
	require.Equal(t, "Production", created.EnvironmentName)
	require.Equal(t, "alice", kit.FromPtr(created.CreatedBy))
	require.Equal(t, []string{"bob"}, *invalidated)

	_, _, err = service.Create(ctx, envEditor, "alice", types.CreateRoleBinding{
		SubjectType:   SubjectUser,
		SubjectID:     "bob",
		RoleID:        authz.BuiltInRoleEditor,
		EnvironmentID: new("env-1"),
	})
	require.ErrorIs(t, err, common.ErrRoleBindingExists)

	for _, input := range []types.CreateRoleBinding{
		{SubjectType: SubjectUser, SubjectID: "bob", RoleID: authz.BuiltInRoleEditor},
		{SubjectType: SubjectGroup, SubjectID: "ops", RoleID: authz.BuiltInRoleAdmin, EnvironmentID: new("env-1")},
	} {
		_, _, err = service.Create(ctx, envEditor, "alice", input)
		require.ErrorIs(t, err, common.ErrRolePermissionEscalation)
	}

	for _, input := range []types.CreateRoleBinding{
		{SubjectType: SubjectUser, SubjectID: "bob", RoleID: "role_missing"},
		{SubjectType: SubjectUser, SubjectID: "carol", RoleID: authz.BuiltInRoleViewer},
		{SubjectType: SubjectGroup, SubjectID: "ops", RoleID: authz.BuiltInRoleViewer, EnvironmentID: new("env-missing")},
		{SubjectType: "team", SubjectID: "ops", RoleID: authz.BuiltInRoleViewer},
	} {
		_, _, err = service.Create(ctx, authz.SudoPermissionSet(), "alice", input)
		require.ErrorIs(t, err, common.ErrInvalidRoleBinding)
	}

	viewer, ok := authz.BuiltInRole(authz.BuiltInRoleViewer)
	require.True(t, ok)
	narrow := authz.NewPermissionSet()
	narrow.AddEnv("env-1", viewer.Permissions...)
	require.ErrorIs(t, service.Delete(ctx, narrow, created.ID), common.ErrRolePermissionEscalation)

	require.NoError(t, service.Delete(ctx, envEditor, created.ID))
	_, err = service.Get(ctx, created.ID)
	require.ErrorIs(t, err, common.ErrRoleBindingNotFound)
	require.Equal(t, []string{"bob", "bob"}, *invalidated)
}

func TestCreateBindingValidatesSelector(t *testing.T) {
	service, db, _ := setupBindingService(t, noUserBindings)

	_, _, err := service.Create(t.Context(), authz.SudoPermissionSet(), "alice", types.CreateRoleBinding{
		SubjectType: SubjectUser,
		SubjectID:   "bob",
		RoleID:      authz.BuiltInRoleDeployer,
		Selector:    &types.ResourceSelector{},
	})
	require.ErrorIs(t, err, authz.ErrInvalidSelector)
	var count int64
	require.NoError(t, db.Table("role_bindings").Count(&count).Error)
	require.Zero(t, count)

	selector := types.ResourceSelector{Labels: map[string]string{"team": "a"}}
	binding, _, err := service.Create(t.Context(), authz.SudoPermissionSet(), "alice", types.CreateRoleBinding{
		SubjectType: SubjectUser,
		SubjectID:   "bob",
		RoleID:      authz.BuiltInRoleDeployer,
		Selector:    &selector,
	})
	require.NoError(t, err)
	require.Equal(t, &selector, binding.Selector)
	require.NoError(t, db.Table("role_bindings").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestDeleteBindingKeepsLastGlobalAdmin(t *testing.T) {
	ctx := t.Context()
	service, _, _ := setupBindingService(t, noUserBindings)
	admins := map[string]string{}
	for _, userID := range []string{"alice", "bob"} {
		created, _, err := service.Create(ctx, authz.SudoPermissionSet(), "", types.CreateRoleBinding{
			SubjectType: SubjectUser,
			SubjectID:   userID,
			RoleID:      authz.BuiltInRoleAdmin,
		})
		require.NoError(t, err)
		require.Nil(t, created.CreatedBy)
		admins[userID] = created.ID
	}

	require.NoError(t, service.Delete(ctx, authz.SudoPermissionSet(), admins["bob"]))
	require.ErrorIs(t, service.Delete(ctx, authz.SudoPermissionSet(), admins["alice"]), common.ErrNoGlobalAdminRemains)
	_, err := service.Get(ctx, admins["alice"])
	require.NoError(t, err)
}

func TestListBindingsFiltersBySubjectRoleAndEnvironment(t *testing.T) {
	ctx := t.Context()
	service, _, _ := setupBindingService(t, noUserBindings)
	for _, input := range []types.CreateRoleBinding{
		{SubjectType: SubjectUser, SubjectID: "alice", RoleID: authz.BuiltInRoleViewer},
		{SubjectType: SubjectUser, SubjectID: "bob", RoleID: authz.BuiltInRoleEditor, EnvironmentID: new("env-1")},
		{SubjectType: SubjectGroup, SubjectID: "ops", RoleID: authz.BuiltInRoleEditor, EnvironmentID: new("env-1")},
	} {
		_, _, err := service.Create(ctx, authz.SudoPermissionSet(), "alice", input)
		require.NoError(t, err)
	}

	all, page, err := service.List(ctx, pagination.QueryParams{Limit: -1}, types.RoleBindingFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.EqualValues(t, 3, page.TotalItems)

	groups, _, err := service.List(ctx, pagination.QueryParams{Limit: -1}, types.RoleBindingFilter{SubjectType: SubjectGroup})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, "Operators", groups[0].SubjectName)
	require.Equal(t, SubjectGroup, groups[0].SubjectType)

	editors, _, err := service.List(ctx, pagination.QueryParams{Limit: -1}, types.RoleBindingFilter{
		RoleID:        authz.BuiltInRoleEditor,
		EnvironmentID: "env-1",
	})
	require.NoError(t, err)
	require.Len(t, editors, 2)

	bob, _, err := service.List(ctx, pagination.QueryParams{Limit: -1}, types.RoleBindingFilter{SubjectID: "bob"})
	require.NoError(t, err)
	require.Len(t, bob, 1)
	require.Equal(t, authz.BuiltInRoleEditor, bob[0].RoleID)
}

func TestEffectiveAccessGroupsPermissionsByScopeWithProvenance(t *testing.T) {
	selector := types.ResourceSelector{Labels: map[string]string{"team": "a"}}
	summaries := []types.RoleBindingSummary{
		{ID: "viewer", RoleID: authz.BuiltInRoleViewer, RoleName: "Viewer", Source: "manual"},
		{ID: "deployer", RoleID: authz.BuiltInRoleDeployer, RoleName: "Deployer", Selector: &selector, Source: "manual"},
		{ID: "ops-editor", RoleID: authz.BuiltInRoleEditor, RoleName: "Editor", EnvironmentID: new("env-1"), Source: "manual", GroupID: new("ops"), GroupName: new("Operators")},
	}
	service, _, _ := setupBindingService(t, func(_ context.Context, userID string) ([]types.RoleBindingSummary, error) {
		require.Equal(t, "bob", userID)
		return summaries, nil
	})

	access, err := service.EffectiveAccess(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(t, "bob", access.UserID)
	require.False(t, access.IsGlobalAdmin)
	require.Len(t, access.Scopes, 2)
	require.Nil(t, access.Scopes[0].EnvironmentID)
	require.Equal(t, "env-1", kit.FromPtr(access.Scopes[1].EnvironmentID))

	byPermission := func(scope types.EffectiveScope) map[string]types.EffectivePermission {
		out := map[string]types.EffectivePermission{}
		for _, permission := range scope.Permissions {
			out[permission.Permission] = permission
		}
		return out
	}
	global := byPermission(access.Scopes[0])
	require.True(t, global[authz.PermContainersList].Unrestricted)
	require.Nil(t, global[authz.PermContainersList].Selectors)
	require.Len(t, global[authz.PermContainersList].Via, 2)
	require.False(t, global[authz.PermContainersStart].Unrestricted)
	require.Equal(t, []types.ResourceSelector{selector}, global[authz.PermContainersStart].Selectors)
	require.Equal(t, "deployer", global[authz.PermContainersStart].Via[0].ID)
	require.NotContains(t, global, authz.PermProjectsDeploy)

	production := byPermission(access.Scopes[1])
	require.True(t, production[authz.PermContainersStart].Unrestricted)
	require.Equal(t, "Operators", kit.FromPtr(production[authz.PermContainersStart].Via[0].GroupName))

	summaries = append(summaries, types.RoleBindingSummary{ID: "admin", RoleID: authz.BuiltInRoleAdmin, RoleName: "Admin", Source: "manual"})
	access, err = service.EffectiveAccess(t.Context(), "bob")
	require.NoError(t, err)
	require.True(t, access.IsGlobalAdmin)
}

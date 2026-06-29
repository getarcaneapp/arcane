package services

import (
	"context"
	"slices"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/stretchr/testify/require"
)

func TestBackfillPermsForKeyDeduplicatesGlobalAndEnvironmentPermissions(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	admin := createTestUser(t, userSvc, "admin", "admin")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	user := createTestUser(t, userSvc, "api-key-owner", "api-key-owner")
	envID := "env-1"
	createTestEnvironment(t, roleSvc.db, envID, "http://localhost:3552", nil)

	require.NoError(t, roleSvc.SetUserAssignments(ctx, user.ID, []models.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleViewer, EnvironmentID: nil},
		{RoleID: authz.BuiltInRoleEditor, EnvironmentID: &envID},
	}))

	perms, err := roleSvc.backfillPermsForKeyInternal(ctx, roleSvc.db.WithContext(ctx), models.ApiKey{
		UserID:        &user.ID,
		EnvironmentID: &envID,
	})
	require.NoError(t, err)
	require.Contains(t, perms, authz.PermContainersList)
	require.Equal(t, 1, countPermissionInternal(perms, authz.PermContainersList))
}

func countPermissionInternal(perms []string, permission string) int {
	return len(slices.DeleteFunc(slices.Clone(perms), func(p string) bool {
		return p != permission
	}))
}

func TestValidatePermissionsAgainstCallerRejectsEscalation(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	caller := authz.NewPermissionSet()
	caller.AddGlobal(authz.PermRolesRead, authz.PermRolesList)

	err := roleSvc.ValidatePermissionsAgainstCaller(caller, []string{
		authz.PermRolesRead,
		authz.PermUsersDelete,
	})
	require.Error(t, err)
	require.True(t, common.IsRolePermissionEscalationError(err))

	require.NoError(t, roleSvc.ValidatePermissionsAgainstCaller(caller, []string{authz.PermRolesRead}))
	require.NoError(t, roleSvc.ValidatePermissionsAgainstCaller(authz.SudoPermissionSet(), []string{authz.PermUsersDelete}))
}

func TestValidatePermissionsAgainstCallerRejectsEnvOnlyGrantForGlobalRole(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	caller := authz.NewPermissionSet()
	caller.AddEnv("env-1", authz.PermContainersStart)

	err := roleSvc.ValidatePermissionsAgainstCaller(caller, []string{authz.PermContainersStart})
	require.Error(t, err)
	require.True(t, common.IsRolePermissionEscalationError(err))
}

func TestValidatePermissionsAgainstCallerRejectsUnknownPermissionBeforeEscalation(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	// A sudo caller would otherwise short-circuit past the escalation loop;
	// unknown perms must still surface as UnknownPermissionError (→ 400),
	// not as an opaque escalation 403 or a silent pass.
	err := roleSvc.ValidatePermissionsAgainstCaller(authz.SudoPermissionSet(), []string{"containrs:start"})
	require.Error(t, err)
	require.True(t, common.IsUnknownPermissionError(err))
	require.False(t, common.IsRolePermissionEscalationError(err))
}

func TestBackfillLegacyRoleAssignmentsIsNoOpWhenColumnAbsent(t *testing.T) {
	ctx := context.Background()
	_, roleSvc := setupUserAndRoleServices(t)
	// setupUserAndRoleServices runs migrations through to current, so
	// users.roles never exists in the fresh test schema.
	require.False(t, roleSvc.db.Migrator().HasColumn("users", "roles"))
	require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
	// Idempotent — second call is also fine.
	require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
}

func TestSetUserAssignmentsRejectsUnknownRole(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "victim", "victim")

	err := roleSvc.SetUserAssignments(ctx, user.ID, []models.UserRoleAssignment{
		{RoleID: "role_does_not_exist"},
	})
	require.Error(t, err)
	require.True(t, common.IsInvalidRoleAssignmentError(err))
}

func TestReplaceOidcAssignmentsRejectsUnknownRole(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "oidc-user", "oidc-user")

	err := roleSvc.ReplaceOidcAssignments(ctx, user.ID, []models.UserRoleAssignment{
		{RoleID: "role_does_not_exist"},
	})
	require.Error(t, err)
	require.True(t, common.IsInvalidRoleAssignmentError(err))
}

func TestReplaceOidcAssignmentsRejectsUnknownEnvironment(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "oidc-user-env", "oidc-user-env")
	missingEnv := "env_does_not_exist"

	// A valid role scoped to a non-existent environment must fail existence
	// validation (mirrors SetUserAssignments) rather than attempting an insert.
	err := roleSvc.ReplaceOidcAssignments(ctx, user.ID, []models.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleViewer, EnvironmentID: &missingEnv},
	})
	require.Error(t, err)
	require.True(t, common.IsInvalidRoleAssignmentError(err))
}

func TestEffectiveGlobalAdminCountIncludesCustomAllPermissionsRole(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "custom-admin", "custom-admin")
	customRole, err := roleSvc.CreateRole(ctx, "Custom Admin", nil, authz.AllPermissions())
	require.NoError(t, err)

	require.NoError(t, roleSvc.SetUserAssignments(ctx, user.ID, []models.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	count, err := roleSvc.CountGlobalAdminsExcludingUser(ctx, "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, roleSvc.AssertGlobalAdminExists(ctx))

	err = roleSvc.SetUserAssignments(ctx, user.ID, nil)
	require.Error(t, err)
	require.True(t, common.IsNoGlobalAdminRemainsError(err))
}

func TestEffectiveGlobalAdminCountIgnoresEnvScopedAndServiceAccounts(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	customRole, err := roleSvc.CreateRole(ctx, "Custom Admin", nil, authz.AllPermissions())
	require.NoError(t, err)
	envID := "env-1"
	createTestEnvironment(t, roleSvc.db, envID, "http://localhost:3552", nil)

	globalAdmin := createTestUser(t, userSvc, "global-admin", "global-admin")
	envScopedAdmin := createTestUser(t, userSvc, "env-scoped-admin", "env-scoped-admin")
	serviceAdmin := &models.User{
		BaseModel:        models.BaseModel{ID: "service-admin"},
		Username:         "service-admin",
		IsServiceAccount: true,
	}
	require.NoError(t, roleSvc.db.WithContext(ctx).Create(serviceAdmin).Error)

	require.NoError(t, roleSvc.SetUserAssignments(ctx, globalAdmin.ID, []models.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))
	require.NoError(t, roleSvc.SetUserAssignments(ctx, envScopedAdmin.ID, []models.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: &envID},
	}))
	require.NoError(t, roleSvc.SetUserAssignments(ctx, serviceAdmin.ID, []models.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	count, err := roleSvc.CountGlobalAdminsExcludingUser(ctx, "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

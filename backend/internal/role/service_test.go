package role

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"context"
	"slices"
	"testing"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidatePermissionsAgainstCallerRejectsEscalation(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	caller := authz.NewPermissionSet()
	caller.AddGlobal(authz.PermRolesRead, authz.PermRolesList)

	err := roleSvc.ValidatePermissionsAgainstCaller(caller, []string{
		authz.PermRolesRead,
		authz.PermUsersDelete,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrRolePermissionEscalation))

	require.NoError(t, roleSvc.ValidatePermissionsAgainstCaller(caller, []string{authz.PermRolesRead}))
	require.NoError(t, roleSvc.ValidatePermissionsAgainstCaller(authz.SudoPermissionSet(), []string{authz.PermUsersDelete}))
}

func TestValidatePermissionsAgainstCallerRejectsEnvOnlyGrantForGlobalRole(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	caller := authz.NewPermissionSet()
	caller.AddEnv("env-1", authz.PermContainersStart)

	err := roleSvc.ValidatePermissionsAgainstCaller(caller, []string{authz.PermContainersStart})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrRolePermissionEscalation))
}

func TestValidatePermissionsAgainstCallerRejectsUnknownPermissionBeforeEscalation(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	// A sudo caller would otherwise short-circuit past the escalation loop;
	// unknown perms must still surface as UnknownPermissionError (→ 400),
	// not as an opaque escalation 403 or a silent pass.
	err := roleSvc.ValidatePermissionsAgainstCaller(authz.SudoPermissionSet(), []string{"containrs:start"})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrUnknownPermission))
	require.False(t, errors.Is(err, common.ErrRolePermissionEscalation))
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

func TestEnsureBuiltInRolesMigratesVariablePermissionsWithoutBackfillingCustomGrants(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)

	customRole, err := roleSvc.CreateRole(ctx, "Template Reader", nil, []string{authz.PermTemplatesRead})
	require.NoError(t, err)
	owner := createTestUser(t, userSvc, "variable-migration-owner", "variable-migration-owner")
	scopedKey := testApiKeyRow{
		Name:      "Custom scoped key",
		KeyHash:   "variable-migration-hash",
		KeyPrefix: "arc_vars",
		Kind:      "scoped",
		UserID:    &owner.ID,
	}
	require.NoError(t, roleSvc.db.WithContext(ctx).Create(&scopedKey).Error)
	require.NoError(t, roleSvc.db.WithContext(ctx).Create(&ApiKeyPermission{
		ApiKeyID:   scopedKey.ID,
		Permission: authz.PermTemplatesRead,
	}).Error)

	oldEditorPermissions := slices.DeleteFunc(authz.BuiltInEditorPermissions(), func(permission string) bool {
		return slices.Contains([]string{
			authz.PermVariablesRead,
			authz.PermVariablesCreate,
			authz.PermVariablesUpdate,
			authz.PermVariablesDelete,
			authz.PermVariablesSync,
		}, permission)
	})
	require.NoError(t, roleSvc.db.WithContext(ctx).Model(&Role{}).
		Where("id = ?", authz.BuiltInRoleEditor).
		Update("permissions", database.StringSlice(oldEditorPermissions)).Error)

	require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))

	allVariablePermissions := []string{
		authz.PermVariablesRead,
		authz.PermVariablesCreate,
		authz.PermVariablesUpdate,
		authz.PermVariablesDelete,
		authz.PermVariablesSync,
	}
	for _, roleID := range []string{authz.BuiltInRoleAdmin, authz.BuiltInRoleEditor, authz.BuiltInRoleNoShellEditor} {
		role, getErr := roleSvc.GetRole(ctx, roleID)
		require.NoError(t, getErr)
		for _, permission := range allVariablePermissions {
			require.Contains(t, []string(role.Permissions), permission, "role %s", roleID)
		}
	}
	for _, roleID := range []string{authz.BuiltInRoleViewer, authz.BuiltInRoleDeployer} {
		role, getErr := roleSvc.GetRole(ctx, roleID)
		require.NoError(t, getErr)
		require.Contains(t, []string(role.Permissions), authz.PermVariablesRead)
		for _, permission := range allVariablePermissions[1:] {
			require.NotContains(t, []string(role.Permissions), permission, "role %s", roleID)
		}
	}
	monitor, err := roleSvc.GetRole(ctx, authz.BuiltInRoleMonitor)
	require.NoError(t, err)
	for _, permission := range allVariablePermissions {
		require.NotContains(t, []string(monitor.Permissions), permission)
	}

	preservedCustomRole, err := roleSvc.GetRole(ctx, customRole.ID)
	require.NoError(t, err)
	require.Equal(t, []string{authz.PermTemplatesRead}, []string(preservedCustomRole.Permissions))

	var keyPermissions []ApiKeyPermission
	require.NoError(t, roleSvc.db.WithContext(ctx).Where("api_key_id = ?", scopedKey.ID).Find(&keyPermissions).Error)
	require.Len(t, keyPermissions, 1)
	require.Equal(t, authz.PermTemplatesRead, keyPermissions[0].Permission)
}

func TestSetUserAssignmentsRejectsUnknownRole(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "victim", "victim")

	err := roleSvc.SetUserAssignments(ctx, user.ID, []UserRoleAssignment{
		{RoleID: "role_does_not_exist"},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrInvalidRoleAssignment))
}

func TestReplaceOidcAssignmentsRejectsUnknownRole(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "oidc-user", "oidc-user")

	err := roleSvc.ReplaceOidcAssignments(ctx, user.ID, []UserRoleAssignment{
		{RoleID: "role_does_not_exist"},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrInvalidRoleAssignment))
}

func TestReplaceOidcAssignmentsRejectsUnknownEnvironment(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "oidc-user-env", "oidc-user-env")
	missingEnv := "env_does_not_exist"

	// A valid role scoped to a non-existent environment must fail existence
	// validation (mirrors SetUserAssignments) rather than attempting an insert.
	err := roleSvc.ReplaceOidcAssignments(ctx, user.ID, []UserRoleAssignment{
		{RoleID: authz.BuiltInRoleViewer, EnvironmentID: &missingEnv},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrInvalidRoleAssignment))
}

func TestEffectiveGlobalAdminCountIncludesCustomAllPermissionsRole(t *testing.T) {
	ctx := context.Background()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	user := createTestUser(t, userSvc, "custom-admin", "custom-admin")
	customRole, err := roleSvc.CreateRole(ctx, "Custom Admin", nil, authz.AllPermissions())
	require.NoError(t, err)

	require.NoError(t, roleSvc.SetUserAssignments(ctx, user.ID, []UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	count, err := roleSvc.CountGlobalAdminsExcludingUser(ctx, "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, roleSvc.AssertGlobalAdminExists(ctx))

	err = roleSvc.SetUserAssignments(ctx, user.ID, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, common.ErrNoGlobalAdminRemains))
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
	serviceAdmin := &common.User{
		BaseModel:        database.BaseModel{ID: "service-admin"},
		Username:         "service-admin",
		IsServiceAccount: true,
	}
	require.NoError(t, roleSvc.db.WithContext(ctx).Create(serviceAdmin).Error)

	require.NoError(t, roleSvc.SetUserAssignments(ctx, globalAdmin.ID, []UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))
	require.NoError(t, roleSvc.SetUserAssignments(ctx, envScopedAdmin.ID, []UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: &envID},
	}))
	require.NoError(t, roleSvc.SetUserAssignments(ctx, serviceAdmin.ID, []UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	count, err := roleSvc.CountGlobalAdminsExcludingUser(ctx, "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func setupAuthServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&common.User{},
		&session.UserSession{},
		&testEnvironmentRow{},
		&Role{},
		&UserRoleAssignment{},
		&testApiKeyRow{},
		&ApiKeyPermission{},
		&OidcRoleMapping{},
	))
	return &database.DB{DB: db}
}

func setupUserAndRoleServices(t *testing.T) (*database.DB, *RoleService) {
	t.Helper()
	db := setupAuthServiceTestDB(t)
	roleService := NewRoleService(db)
	require.NoError(t, roleService.EnsureBuiltInRoles(context.Background()))
	return db, roleService
}

func createTestUser(t *testing.T, db *database.DB, id, username string) *common.User {
	t.Helper()
	created := &common.User{BaseModel: database.BaseModel{ID: id}, Username: username}
	require.NoError(t, db.WithContext(context.Background()).Create(created).Error)
	return created
}

func grantGlobalAdmin(t *testing.T, roleService *RoleService, userID string) {
	t.Helper()
	require.NoError(t, roleService.SetUserAssignments(context.Background(), userID, []UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin},
	}))
}

func createTestEnvironment(t *testing.T, db *database.DB, id, apiURL string, accessToken *string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.WithContext(context.Background()).Create(&testEnvironmentRow{
		BaseModel:   database.BaseModel{ID: id, CreatedAt: now, UpdatedAt: &now},
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      "online",
		Enabled:     true,
		AccessToken: accessToken,
	}).Error)
}

// Minimal stand-ins for environment.Environment and apikey.ApiKey: both of
// those packages import role, so this in-package test cannot import them.
type testEnvironmentRow struct {
	database.BaseModel
	Name        string
	ApiUrl      string `gorm:"column:api_url"`
	Status      string
	Enabled     bool
	AccessToken *string `gorm:"column:access_token"`
}

func (testEnvironmentRow) TableName() string { return "environments" }

type testApiKeyRow struct {
	database.BaseModel
	Name          string
	KeyHash       string `gorm:"column:key_hash"`
	KeyPrefix     string `gorm:"column:key_prefix"`
	Kind          string
	UserID        *string `gorm:"column:user_id"`
	EnvironmentID *string `gorm:"column:environment_id"`
	ManagedBy     *string `gorm:"column:managed_by"`
}

func (testApiKeyRow) TableName() string { return "api_keys" }

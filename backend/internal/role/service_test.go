package role

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"context"
	"path/filepath"
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

func TestBackfillLegacyRoleAssignments(t *testing.T) {
	ctx := context.Background()

	t.Run("no-op without legacy column", func(t *testing.T) {
		_, roleSvc := setupUserAndRoleServices(t)
		require.False(t, roleSvc.db.Migrator().HasColumn("users", "roles"))
		require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
		require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
	})

	t.Run("converts legacy roles once", func(t *testing.T) {
		db, err := database.Initialize(ctx, "file:"+filepath.Join(t.TempDir(), "arcane.db"), database.MigrationOptions{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		require.True(t, db.Migrator().HasColumn("users", "roles"))
		roleSvc := NewRoleService(db)
		require.NoError(t, roleSvc.EnsureBuiltInRoles(ctx))
		require.NoError(t, db.Exec("INSERT INTO environments (id, name, api_url) VALUES (?, ?, ?)", "env-1", "env-1", "http://env-1").Error)

		for id, roles := range map[string]string{
			"seed-admin":    `[]`,
			"legacy-admin":  `["user", " ADMIN "]`,
			"legacy-user":   `["user"]`,
			"legacy-empty":  `[]`,
			"legacy-null":   `null`,
			"legacy-blank":  `[" "]`,
			"legacy-broken": `not json`,
			"scoped-manual": `["admin"]`,
			"global-oidc":   `["admin"]`,
		} {
			require.NoError(t, db.Exec("INSERT INTO users (id, username, password_hash, roles) VALUES (?, ?, ?, ?)", id, id, "unused", roles).Error)
		}
		require.NoError(t, db.Create(&UserRoleAssignment{UserID: "seed-admin", RoleID: authz.BuiltInRoleAdmin, Source: RoleAssignmentSourceManual}).Error)
		envID := "env-1"
		require.NoError(t, roleSvc.SetUserAssignments(ctx, "scoped-manual", []UserRoleAssignment{{RoleID: authz.BuiltInRoleViewer, EnvironmentID: &envID}}))
		require.NoError(t, roleSvc.ReplaceOidcAssignments(ctx, "global-oidc", []UserRoleAssignment{{RoleID: authz.BuiltInRoleViewer}}))

		require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
		require.Equal(t, []string{authz.BuiltInRoleAdmin}, assignedRoleIDs(t, roleSvc, "legacy-admin"))
		for _, id := range []string{"legacy-user", "legacy-empty", "legacy-null", "legacy-blank", "legacy-broken"} {
			require.Equal(t, []string{authz.BuiltInRoleViewer}, assignedRoleIDs(t, roleSvc, id), id)
		}
		scoped, err := roleSvc.ListUserAssignments(ctx, "scoped-manual")
		require.NoError(t, err)
		require.Len(t, scoped, 1)
		require.Equal(t, &envID, scoped[0].EnvironmentID)
		oidc, err := roleSvc.ListUserAssignments(ctx, "global-oidc")
		require.NoError(t, err)
		require.Len(t, oidc, 1)
		require.Equal(t, RoleAssignmentSourceOidc, oidc[0].Source)
		ps, err := roleSvc.ResolveUserPermissionsInDB(ctx, db.DB, "scoped-manual")
		require.NoError(t, err)
		require.True(t, ps.Allows(authz.PermContainersList, "env-1"))
		require.False(t, ps.Allows(authz.PermContainersList, ""))

		require.NoError(t, roleSvc.SetUserAssignments(ctx, "legacy-user", nil))
		require.NoError(t, db.Exec("INSERT INTO users (id, username, password_hash, roles) VALUES (?, ?, ?, ?)", "later-admin", "later-admin", "unused", `["admin"]`).Error)
		require.NoError(t, NewRoleService(db).BackfillLegacyRoleAssignments(ctx))
		require.Empty(t, assignedRoleIDs(t, roleSvc, "legacy-user"))
		require.Empty(t, assignedRoleIDs(t, roleSvc, "later-admin"))
	})
}

func assignedRoleIDs(t *testing.T, roleSvc *RoleService, userID string) []string {
	t.Helper()
	assignments, err := roleSvc.ListUserAssignments(context.Background(), userID)
	require.NoError(t, err)
	ids := make([]string, 0, len(assignments))
	for _, a := range assignments {
		ids = append(ids, a.RoleID)
	}
	return ids
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
		ID:               "service-admin",
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
	created := &common.User{ID: id, Username: username}
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
		ID: id, CreatedAt: now, UpdatedAt: &now,
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

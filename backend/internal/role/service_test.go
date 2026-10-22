package role_test

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/session"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
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
	require.ErrorIs(t, err, common.ErrRolePermissionEscalation)

	require.NoError(t, roleSvc.ValidatePermissionsAgainstCaller(caller, []string{authz.PermRolesRead}))
	require.NoError(t, roleSvc.ValidatePermissionsAgainstCaller(authz.SudoPermissionSet(), []string{authz.PermUsersDelete}))
}

func TestValidatePermissionsAgainstCallerRejectsEnvOnlyGrantForGlobalRole(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	caller := authz.NewPermissionSet()
	caller.AddEnv("env-1", authz.PermContainersStart)

	err := roleSvc.ValidatePermissionsAgainstCaller(caller, []string{authz.PermContainersStart})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrRolePermissionEscalation)
}

func TestValidatePermissionsAgainstCallerRejectsUnknownPermissionBeforeEscalation(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	// A sudo caller would otherwise short-circuit past the escalation loop;
	// unknown perms must still surface as UnknownPermissionError (→ 400),
	// not as an opaque escalation 403 or a silent pass.
	err := roleSvc.ValidatePermissionsAgainstCaller(authz.SudoPermissionSet(), []string{"containrs:start"})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrUnknownPermission)
	require.NotErrorIs(t, err, common.ErrRolePermissionEscalation)
}

func TestBackfillLegacyRoleAssignments(t *testing.T) {
	ctx := t.Context()

	t.Run("no-op without legacy column", func(t *testing.T) {
		db, roleSvc := setupUserAndRoleServices(t)
		require.False(t, db.Migrator().HasColumn("users", "roles"))
		require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
		require.NoError(t, roleSvc.BackfillLegacyRoleAssignments(ctx))
	})

	t.Run("converts legacy roles once", func(t *testing.T) {
		db, err := database.Initialize(ctx, "file:"+filepath.Join(t.TempDir(), "arcane.db"), database.MigrationOptions{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		require.True(t, db.Migrator().HasColumn("users", "roles"))
		roleSvc := role.NewRoleService(db)
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
		require.NoError(t, db.Create(&role.UserRoleAssignment{UserID: "seed-admin", RoleID: authz.BuiltInRoleAdmin, Source: role.RoleAssignmentSourceManual}).Error)
		envID := "env-1"
		require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, "scoped-manual", role.RoleAssignmentSourceManual, []role.UserRoleAssignment{{RoleID: authz.BuiltInRoleViewer, EnvironmentID: &envID}}))
		require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, "global-oidc", role.RoleAssignmentSourceOidc, []role.UserRoleAssignment{{RoleID: authz.BuiltInRoleViewer}}))

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
		require.Equal(t, role.RoleAssignmentSourceOidc, oidc[0].Source)
		ps, err := roleSvc.ResolveUserPermissionsInDB(ctx, db.DB, "scoped-manual")
		require.NoError(t, err)
		require.True(t, ps.Allows(authz.PermContainersList, "env-1"))
		require.False(t, ps.Allows(authz.PermContainersList, ""))

		require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, "legacy-user", role.RoleAssignmentSourceManual, nil))
		require.NoError(t, db.Exec("INSERT INTO users (id, username, password_hash, roles) VALUES (?, ?, ?, ?)", "later-admin", "later-admin", "unused", `["admin"]`).Error)
		require.NoError(t, role.NewRoleService(db).BackfillLegacyRoleAssignments(ctx))
		require.Empty(t, assignedRoleIDs(t, roleSvc, "legacy-user"))
		require.Empty(t, assignedRoleIDs(t, roleSvc, "later-admin"))
	})
}

func assignedRoleIDs(t *testing.T, roleSvc *role.RoleService, userID string) []string {
	t.Helper()
	assignments, err := roleSvc.ListUserAssignments(t.Context(), userID)
	require.NoError(t, err)
	ids := make([]string, 0, len(assignments))
	for _, a := range assignments {
		ids = append(ids, a.RoleID)
	}
	return ids
}

func TestEnsureBuiltInRolesMigratesVariablePermissionsWithoutBackfillingCustomGrants(t *testing.T) {
	ctx := t.Context()
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
	require.NoError(t, userSvc.WithContext(ctx).Create(&scopedKey).Error)
	require.NoError(t, userSvc.WithContext(ctx).Create(&role.ApiKeyPermission{
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
	require.NoError(t, userSvc.WithContext(ctx).Model(&role.Role{}).
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
		builtIn, getErr := roleSvc.GetRole(ctx, roleID)
		require.NoError(t, getErr)
		for _, permission := range allVariablePermissions {
			require.Contains(t, []string(builtIn.Permissions), permission, "role %s", roleID)
		}
	}
	for _, roleID := range []string{authz.BuiltInRoleViewer, authz.BuiltInRoleDeployer} {
		builtIn, getErr := roleSvc.GetRole(ctx, roleID)
		require.NoError(t, getErr)
		require.Contains(t, []string(builtIn.Permissions), authz.PermVariablesRead)
		for _, permission := range allVariablePermissions[1:] {
			require.NotContains(t, []string(builtIn.Permissions), permission, "role %s", roleID)
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

	var keyPermissions []role.ApiKeyPermission
	require.NoError(t, userSvc.WithContext(ctx).Where("api_key_id = ?", scopedKey.ID).Find(&keyPermissions).Error)
	require.Len(t, keyPermissions, 1)
	require.Equal(t, authz.PermTemplatesRead, keyPermissions[0].Permission)
}

func TestSetUserAssignmentsRejectsUnknownRole(t *testing.T) {
	ctx := t.Context()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	victim := createTestUser(t, userSvc, "victim", "victim")

	err := roleSvc.ReplaceUserAssignments(ctx, victim.ID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: "role_does_not_exist"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrInvalidRoleAssignment)
}

func TestReplaceUserAssignmentsOidcRejectsUnknownRole(t *testing.T) {
	ctx := t.Context()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	oidcUser := createTestUser(t, userSvc, "oidc-user", "oidc-user")

	err := roleSvc.ReplaceUserAssignments(ctx, oidcUser.ID, role.RoleAssignmentSourceOidc, []role.UserRoleAssignment{
		{RoleID: "role_does_not_exist"},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrInvalidRoleAssignment)
}

func TestReplaceUserAssignmentsOidcRejectsUnknownEnvironment(t *testing.T) {
	ctx := t.Context()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	oidcUser := createTestUser(t, userSvc, "oidc-user-env", "oidc-user-env")
	missingEnv := "env_does_not_exist"

	// A valid role scoped to a non-existent environment must fail existence
	// validation (same as manual assignments) rather than attempting an insert.
	err := roleSvc.ReplaceUserAssignments(ctx, oidcUser.ID, role.RoleAssignmentSourceOidc, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleViewer, EnvironmentID: &missingEnv},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrInvalidRoleAssignment)
}

func TestEffectiveGlobalAdminCountIncludesCustomAllPermissionsRole(t *testing.T) {
	ctx := t.Context()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	customAdmin := createTestUser(t, userSvc, "custom-admin", "custom-admin")
	customRole, err := roleSvc.CreateRole(ctx, "Custom Admin", nil, authz.AllPermissions())
	require.NoError(t, err)

	require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, customAdmin.ID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	count, err := roleSvc.CountGlobalAdmins(ctx, userSvc.DB, "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, roleSvc.AssertGlobalAdminExists(ctx, userSvc.DB))

	err = roleSvc.ReplaceUserAssignments(ctx, customAdmin.ID, role.RoleAssignmentSourceManual, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, common.ErrNoGlobalAdminRemains)
}

func TestEffectiveGlobalAdminCountIgnoresEnvScopedAndServiceAccounts(t *testing.T) {
	ctx := t.Context()
	userSvc, roleSvc := setupUserAndRoleServices(t)
	customRole, err := roleSvc.CreateRole(ctx, "Custom Admin", nil, authz.AllPermissions())
	require.NoError(t, err)
	envID := "env-1"
	createTestEnvironment(t, userSvc, envID, "http://localhost:3552", nil)

	globalAdmin := createTestUser(t, userSvc, "global-admin", "global-admin")
	envScopedAdmin := createTestUser(t, userSvc, "env-scoped-admin", "env-scoped-admin")
	serviceAdmin := &user.User{
		ID:               "service-admin",
		Username:         "service-admin",
		IsServiceAccount: true,
	}
	require.NoError(t, userSvc.WithContext(ctx).Create(serviceAdmin).Error)

	require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, globalAdmin.ID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))
	require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, envScopedAdmin.ID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: &envID},
	}))
	require.NoError(t, roleSvc.ReplaceUserAssignments(ctx, serviceAdmin.ID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	count, err := roleSvc.CountGlobalAdmins(ctx, userSvc.DB, "")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func setupAuthServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&user.User{},
		&session.UserSession{},
		&testEnvironmentRow{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&testApiKeyRow{},
		&role.ApiKeyPermission{},
		&role.OidcRoleMapping{},
	))
	return &database.DB{DB: db}
}

func setupUserAndRoleServices(t *testing.T) (*database.DB, *role.RoleService) {
	t.Helper()
	db := setupAuthServiceTestDB(t)
	roleService := role.NewRoleService(db)
	require.NoError(t, roleService.EnsureBuiltInRoles(t.Context()))
	return db, roleService
}

func createTestUser(t *testing.T, db *database.DB, id, username string) *user.User {
	t.Helper()
	created := &user.User{ID: id, Username: username}
	require.NoError(t, db.WithContext(t.Context()).Create(created).Error)
	return created
}

func grantGlobalAdmin(t *testing.T, roleService *role.RoleService, userID string) {
	t.Helper()
	require.NoError(t, roleService.ReplaceUserAssignments(t.Context(), userID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin},
	}))
}

func createTestEnvironment(t *testing.T, db *database.DB, id, apiURL string, accessToken *string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.WithContext(t.Context()).Create(&testEnvironmentRow{
		ID: id, CreatedAt: now, UpdatedAt: &now,
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      "online",
		Enabled:     true,
		AccessToken: accessToken,
	}).Error)
}

// Minimal stand-ins for the environments and api_keys rows these tests need.
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
	ExpiresAt     *time.Time
}

func (testApiKeyRow) TableName() string { return "api_keys" }

func TestResolveExecutionPermissions(t *testing.T) {
	t.Run("personal keys use current user permissions and explicit scope", func(t *testing.T) {
		db, service := setupUserAndRoleServices(t)
		owner := createTestUser(t, db, "owner", "owner")
		grantGlobalAdmin(t, service, owner.ID)
		environmentID := "env-1"
		key := testApiKeyRow{ID: "personal", Kind: "personal", UserID: &owner.ID, EnvironmentID: &environmentID}
		require.NoError(t, db.Create(&key).Error)
		permissions, err := service.ResolveExecutionPermissions(t.Context(), owner.ID, key.ID)
		require.NoError(t, err)
		require.True(t, permissions.Allows(authz.PermJobsManage, environmentID))
		require.False(t, permissions.Allows(authz.PermJobsManage, "env-2"))
		require.False(t, permissions.IsGlobalAdmin())

		require.NoError(t, db.Where("user_id = ?", owner.ID).Delete(&role.UserRoleAssignment{}).Error)
		for _, keyID := range []string{"", key.ID} {
			permissions, err = service.ResolveExecutionPermissions(t.Context(), owner.ID, keyID)
			require.NoError(t, err)
			require.False(t, permissions.Allows(authz.PermJobsManage, environmentID))
		}
	})

	t.Run("scoped keys bypass cached grants", func(t *testing.T) {
		db, service := setupUserAndRoleServices(t)
		owner := createTestUser(t, db, "owner", "owner")
		grantGlobalAdmin(t, service, owner.ID)
		environmentID := "env-1"
		key := testApiKeyRow{ID: "scoped", Kind: "scoped", UserID: &owner.ID, EnvironmentID: &environmentID}
		require.NoError(t, db.Create(&key).Error)
		require.NoError(t, service.SetApiKeyPermissions(t.Context(), key.ID, []role.ApiKeyPermission{{Permission: authz.PermJobsManage}}))
		cached, err := service.ResolveApiKeyPermissions(t.Context(), key.ID)
		require.NoError(t, err)
		require.True(t, cached.Allows(authz.PermJobsManage, "env-2"))
		permissions, err := service.ResolveExecutionPermissions(t.Context(), owner.ID, key.ID)
		require.NoError(t, err)
		require.True(t, permissions.Allows(authz.PermJobsManage, environmentID))
		require.False(t, permissions.Allows(authz.PermJobsManage, "env-2"))

		require.NoError(t, db.Where("api_key_id = ?", key.ID).Delete(&role.ApiKeyPermission{}).Error)
		permissions, err = service.ResolveExecutionPermissions(t.Context(), owner.ID, key.ID)
		require.NoError(t, err)
		require.False(t, permissions.Allows(authz.PermJobsManage, environmentID))
	})

	t.Run("rejects invalid persisted identities", func(t *testing.T) {
		db, service := setupUserAndRoleServices(t)
		owner := createTestUser(t, db, "owner", "owner")
		otherOwner := "other"
		emptyEnvironment := ""
		expired := time.Now().Add(-time.Minute)
		for _, key := range []testApiKeyRow{
			{ID: "expired", Kind: "personal", UserID: &owner.ID, ExpiresAt: &expired},
			{ID: "other-owner", Kind: "personal", UserID: &otherOwner},
			{ID: "ownerless", Kind: "scoped"},
			{ID: "invalid-kind", Kind: "unknown", UserID: &owner.ID},
			{ID: "invalid-scope", Kind: "personal", UserID: &owner.ID, EnvironmentID: &emptyEnvironment},
		} {
			require.NoError(t, db.Create(&key).Error)
			_, err := service.ResolveExecutionPermissions(t.Context(), owner.ID, key.ID)
			require.Error(t, err, key.ID)
		}
		_, err := service.ResolveExecutionPermissions(t.Context(), owner.ID, "deleted-key")
		require.Error(t, err)
		require.NoError(t, db.Delete(owner).Error)
		_, err = service.ResolveExecutionPermissions(t.Context(), owner.ID, "")
		require.Error(t, err)
	})
}

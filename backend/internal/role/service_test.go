package role_test

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/audit"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/group"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/session"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

func TestListRolesMergesBuiltInsFromCodeWithCustomRoles(t *testing.T) {
	ctx := t.Context()
	db, roleSvc := setupUserAndRoleServices(t)
	require.NoError(t, db.Create(&role.Role{
		ID:          authz.BuiltInRoleEditor,
		Name:        "Editor",
		Permissions: database.StringSlice{},
		BuiltIn:     true,
	}).Error)
	custom, err := roleSvc.CreateRole(ctx, "Template Reader", nil, []string{authz.PermTemplatesRead})
	require.NoError(t, err)

	roles, page, err := roleSvc.ListRoles(ctx, pagination.QueryParams{Limit: -1})
	require.NoError(t, err)
	require.EqualValues(t, len(authz.BuiltInRoles())+1, page.TotalItems)
	byID := map[string]role.Role{}
	for _, r := range roles {
		byID[r.ID] = r
	}
	editor, ok := authz.BuiltInRole(authz.BuiltInRoleEditor)
	require.True(t, ok)
	require.Equal(t, editor.Permissions, []string(byID[authz.BuiltInRoleEditor].Permissions))
	require.True(t, byID[authz.BuiltInRoleEditor].BuiltIn)
	require.Equal(t, []string{authz.PermAll}, []string(byID[authz.BuiltInRoleAdmin].Permissions))
	require.Equal(t, []string{authz.PermTemplatesRead}, []string(byID[custom.ID].Permissions))
	require.False(t, byID[custom.ID].BuiltIn)

	got, err := roleSvc.GetRole(ctx, authz.BuiltInRoleEditor)
	require.NoError(t, err)
	require.Equal(t, editor.Permissions, []string(got.Permissions))

	_, err = roleSvc.UpdateRole(ctx, authz.SudoPermissionSet(), authz.BuiltInRoleAdmin, "Admin", nil, []string{authz.PermTemplatesRead})
	require.ErrorIs(t, err, common.ErrRoleBuiltIn)
	require.ErrorIs(t, roleSvc.DeleteRole(ctx, authz.SudoPermissionSet(), authz.BuiltInRoleViewer), common.ErrRoleBuiltIn)
	_, err = roleSvc.CreateRole(ctx, "Wildcard", nil, []string{authz.PermAll})
	require.ErrorIs(t, err, common.ErrUnknownPermission)
	_, err = roleSvc.UpdateRole(ctx, authz.SudoPermissionSet(), custom.ID, custom.Name, nil, []string{authz.PermAll})
	require.ErrorIs(t, err, common.ErrUnknownPermission)
}

func TestResolveUserPermissionsCombinesDirectAndGroupBindings(t *testing.T) {
	ctx := t.Context()
	db, roleSvc := setupUserAndRoleServices(t)
	member := createTestUser(t, db, "member", "member")
	createTestEnvironment(t, db, "env-1", "http://localhost:3552", nil)
	operators := group.Group{Name: "operators"}
	require.NoError(t, db.Create(&operators).Error)
	require.NoError(t, db.Create(&group.GroupMember{
		GroupID: operators.ID,
		UserID:  member.ID,
		Source:  group.MemberSourceManual,
	}).Error)
	require.NoError(t, db.Create(&role.RoleBinding{
		GroupID:       new(operators.ID),
		RoleID:        authz.BuiltInRoleEditor,
		EnvironmentID: new("env-1"),
		Source:        role.BindingSourceManual,
	}).Error)
	require.NoError(t, roleSvc.BindUser(ctx, member.ID, authz.BuiltInRoleViewer))

	ps, err := roleSvc.ResolvePermissions(ctx, member.ID)
	require.NoError(t, err)
	require.Equal(t, member.ID, ps.SubjectID)
	require.True(t, ps.Allows(authz.PermContainersList, ""))
	require.True(t, ps.Allows(authz.PermContainersStart, "env-1"))
	require.False(t, ps.Allows(authz.PermContainersStart, ""))
	require.False(t, ps.Allows(authz.PermContainersStart, "env-2"))

	require.NoError(t, db.Create(&role.RoleBinding{
		UserID:       new(member.ID),
		RoleID:       authz.BuiltInRoleDeployer,
		Selector:     &types.ResourceSelector{Labels: map[string]string{"team": "a"}},
		SelectorHash: "team-a",
		Source:       role.BindingSourceManual,
	}).Error)
	roleSvc.InvalidateUsers(member.ID)
	ps, err = roleSvc.ResolvePermissions(ctx, member.ID)
	require.NoError(t, err)
	scope := ps.Scope(authz.PermContainersStart, "env-2")
	require.False(t, scope.IsUnrestricted())
	require.True(t, scope.Matches(authz.ContainerAttributes([]string{"web"}, map[string]string{"team": "a"})))
	require.False(t, scope.Matches(authz.ContainerAttributes([]string{"web"}, map[string]string{"team": "b"})))
	require.False(t, ps.Allows(authz.PermProjectsDeploy, "env-2"))
}

func TestCountAdminsCountsDirectAndGroupAdminBindingsOnly(t *testing.T) {
	ctx := t.Context()
	db, roleSvc := setupUserAndRoleServices(t)
	createTestEnvironment(t, db, "env-1", "http://localhost:3552", nil)
	direct := createTestUser(t, db, "direct-admin", "direct-admin")
	grantGlobalAdmin(t, roleSvc, direct.ID)

	viaGroup := createTestUser(t, db, "group-admin", "group-admin")
	admins := group.Group{Name: "admins"}
	require.NoError(t, db.Create(&admins).Error)
	require.NoError(t, db.Create(&role.RoleBinding{
		GroupID: new(admins.ID),
		RoleID:  authz.BuiltInRoleAdmin,
		Source:  role.BindingSourceManual,
	}).Error)
	require.NoError(t, db.Create(&group.GroupMember{
		GroupID: admins.ID,
		UserID:  viaGroup.ID,
		Source:  group.MemberSourceOidc,
	}).Error)

	envAdmin := createTestUser(t, db, "env-admin", "env-admin")
	require.NoError(t, db.Create(&role.RoleBinding{
		UserID:        new(envAdmin.ID),
		RoleID:        authz.BuiltInRoleAdmin,
		EnvironmentID: new("env-1"),
		Source:        role.BindingSourceManual,
	}).Error)
	serviceAdmin := &user.User{
		ID:               "service-admin",
		Username:         "service-admin",
		IsServiceAccount: true,
	}
	require.NoError(t, db.Create(serviceAdmin).Error)
	grantGlobalAdmin(t, roleSvc, serviceAdmin.ID)
	allPermissions := createTestUser(t, db, "all-permissions", "all-permissions")
	customRole, err := roleSvc.CreateRole(ctx, "Everything", nil, authz.AllPermissions())
	require.NoError(t, err)
	require.NoError(t, roleSvc.BindUser(ctx, allPermissions.ID, customRole.ID))

	count, err := roleSvc.CountAdmins(ctx, db.DB)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	count, err = roleSvc.CountAdmins(ctx, db.DB, direct.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	groupAdminPerms, err := roleSvc.ResolvePermissions(ctx, viaGroup.ID)
	require.NoError(t, err)
	require.True(t, groupAdminPerms.IsGlobalAdmin())
	allPerms, err := roleSvc.ResolvePermissions(ctx, allPermissions.ID)
	require.NoError(t, err)
	require.False(t, allPerms.IsGlobalAdmin())

	require.NoError(t, db.Where("user_id = ?", direct.ID).Delete(&role.RoleBinding{}).Error)
	require.NoError(t, db.Where("user_id = ?", viaGroup.ID).Delete(&group.GroupMember{}).Error)
	require.ErrorIs(t, roleSvc.AssertGlobalAdminExists(ctx, db.DB), common.ErrNoGlobalAdminRemains)
}

func TestInvalidateUsersDropsOnlyTargetedUserAndKeyCaches(t *testing.T) {
	ctx := t.Context()
	db, roleSvc := setupUserAndRoleServices(t)
	keys := map[string]string{}
	for _, id := range []string{"first", "second"} {
		owner := createTestUser(t, db, id, id)
		require.NoError(t, roleSvc.BindUser(ctx, owner.ID, authz.BuiltInRoleViewer))
		key := testApiKeyRow{
			ID:     "key-" + id,
			Kind:   "scoped",
			UserID: &owner.ID,
		}
		require.NoError(t, db.Create(&key).Error)
		require.NoError(t, roleSvc.SetApiKeyPermissions(ctx, key.ID, []role.ApiKeyPermission{{Permission: authz.PermContainersList}}))
		keys[id] = key.ID
		_, err := roleSvc.ResolvePermissions(ctx, owner.ID)
		require.NoError(t, err)
		_, err = roleSvc.ResolveApiKeyPermissions(ctx, key.ID)
		require.NoError(t, err)
	}
	require.NoError(t, db.Where("1 = 1").Delete(&role.RoleBinding{}).Error)

	roleSvc.InvalidateUsers("first")

	first, err := roleSvc.ResolvePermissions(ctx, "first")
	require.NoError(t, err)
	require.False(t, first.Allows(authz.PermContainersList, ""))
	second, err := roleSvc.ResolvePermissions(ctx, "second")
	require.NoError(t, err)
	require.True(t, second.Allows(authz.PermContainersList, ""))
	firstKey, err := roleSvc.ResolveApiKeyPermissions(ctx, keys["first"])
	require.NoError(t, err)
	require.False(t, firstKey.Allows(authz.PermContainersList, ""))
	secondKey, err := roleSvc.ResolveApiKeyPermissions(ctx, keys["second"])
	require.NoError(t, err)
	require.True(t, secondKey.Allows(authz.PermContainersList, ""))
}

func TestValidateRoleDefinitionAgainstCallerRejectsScopedGlobalHolder(t *testing.T) {
	_, roleSvc := setupUserAndRoleServices(t)

	scoped := authz.NewPermissionSet()
	scoped.AddScopedGlobal(types.ResourceSelector{Labels: map[string]string{"team": "a"}}, authz.PermContainersStart)
	require.ErrorIs(t, roleSvc.ValidateRoleDefinitionAgainstCaller(scoped, []string{authz.PermContainersStart}), common.ErrRolePermissionEscalation)

	envOnly := authz.NewPermissionSet()
	envOnly.AddEnv("env-1", authz.PermContainersStart)
	require.ErrorIs(t, roleSvc.ValidateRoleDefinitionAgainstCaller(envOnly, []string{authz.PermContainersStart}), common.ErrRolePermissionEscalation)

	unrestricted := authz.NewPermissionSet()
	unrestricted.AddGlobal(authz.PermContainersStart)
	require.NoError(t, roleSvc.ValidateRoleDefinitionAgainstCaller(unrestricted, []string{authz.PermContainersStart}))

	err := roleSvc.ValidateRoleDefinitionAgainstCaller(authz.SudoPermissionSet(), []string{"containrs:start"})
	require.ErrorIs(t, err, common.ErrUnknownPermission)
	require.NotErrorIs(t, err, common.ErrRolePermissionEscalation)
	admin := authz.NewPermissionSet()
	admin.AddGlobal(authz.PermAll)
	require.ErrorIs(t, roleSvc.ValidateRoleDefinitionAgainstCaller(admin, []string{authz.PermAll}), common.ErrUnknownPermission)
}

func TestDeleteRoleRequiresCallerToCoverEveryBinding(t *testing.T) {
	ctx := t.Context()
	db, roleSvc := setupUserAndRoleServices(t)
	createTestEnvironment(t, db, "env-1", "http://localhost:3552", nil)
	holder := createTestUser(t, db, "holder", "holder")
	custom, err := roleSvc.CreateRole(ctx, "Starter", nil, []string{authz.PermContainersStart})
	require.NoError(t, err)
	require.NoError(t, db.Create(&role.RoleBinding{
		UserID:        new(holder.ID),
		RoleID:        custom.ID,
		EnvironmentID: new("env-1"),
		Source:        role.BindingSourceManual,
	}).Error)

	otherEnv := authz.NewPermissionSet()
	otherEnv.AddEnv("env-2", authz.PermContainersStart)
	require.ErrorIs(t, roleSvc.DeleteRole(ctx, otherEnv, custom.ID), common.ErrRolePermissionEscalation)

	sameEnv := authz.NewPermissionSet()
	sameEnv.AddEnv("env-1", authz.PermContainersStart)
	require.NoError(t, roleSvc.DeleteRole(ctx, sameEnv, custom.ID))
	_, err = roleSvc.GetRole(ctx, custom.ID)
	require.ErrorIs(t, err, common.ErrRoleNotFound)
}

func setupAuthServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&audit.AuditLog{},
		&settings.SettingVariable{},
		&user.User{},
		&session.UserSession{},
		&testEnvironmentRow{},
		&role.Role{},
		&role.RoleBinding{},
		&group.Group{},
		&group.GroupMember{},
		&testApiKeyRow{},
		&role.ApiKeyPermission{},
		&testFederatedCredentialRow{},
	))
	return &database.DB{DB: db}
}

func setupUserAndRoleServices(t *testing.T) (*database.DB, *role.RoleService) {
	t.Helper()
	db := setupAuthServiceTestDB(t)
	return db, role.NewRoleService(db)
}

func createTestUser(t *testing.T, db *database.DB, id, username string) *user.User {
	t.Helper()
	created := &user.User{ID: id, Username: username}
	require.NoError(t, db.WithContext(t.Context()).Create(created).Error)
	return created
}

func grantGlobalAdmin(t *testing.T, roleService *role.RoleService, userID string) {
	t.Helper()
	require.NoError(t, roleService.BindUser(t.Context(), userID, authz.BuiltInRoleAdmin))
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

// Minimal stand-ins for the environments, api_keys, and federated_credentials rows these tests need.
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

type testFederatedCredentialRow struct {
	database.BaseModel
	RoleID string `gorm:"column:role_id"`
}

func (testFederatedCredentialRow) TableName() string { return "federated_credentials" }

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

		require.NoError(t, db.Where("user_id = ?", owner.ID).Delete(&role.RoleBinding{}).Error)
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

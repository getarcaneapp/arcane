package services

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/stretchr/testify/require"
)

// setupUserAndRoleServices wires both services together the way bootstrap
// does, so the legacy-admin-guard tests exercise the real RBAC path.
func setupUserAndRoleServices(t *testing.T) (*UserService, *RoleService) {
	t.Helper()
	db := setupAuthServiceTestDB(t)
	role := NewRoleService(db)
	require.NoError(t, role.EnsureBuiltInRoles(context.Background()))
	userRecord := NewUserService(db).WithRoleService(role)
	return userRecord, role
}

func createTestUser(t *testing.T, svc *UserService, id, username string) *models.User {
	t.Helper()
	created, err := svc.CreateUser(context.Background(), &models.User{
		BaseModel: models.BaseModel{ID: id},
		Username:  username,
	})
	require.NoError(t, err)
	return created
}

// grantGlobalAdmin assigns the built-in Admin role globally to the user.
func grantGlobalAdmin(t *testing.T, role *RoleService, userID string) {
	t.Helper()
	require.NoError(t, role.SetUserAssignments(context.Background(), userID, []models.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin, EnvironmentID: nil},
	}))
}

func TestDeleteUserRejectsDeletingOnlyAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)

	err := userSvc.DeleteUser(ctx, admin.ID, nil)
	require.ErrorIs(t, err, ErrCannotRemoveLastAdmin)

	stillThere, err := userSvc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, admin.ID, stillThere.ID)
}

func TestSetPasswordUpdatesHashAndClearsPasswordChangeRequirement(t *testing.T) {
	userSvc, _ := setupUserAndRoleServices(t)
	ctx := context.Background()

	oldHash, err := userSvc.HashPassword("old-password")
	require.NoError(t, err)
	user, err := userSvc.CreateUser(ctx, &models.User{
		BaseModel:              models.BaseModel{ID: "password-user"},
		Username:               "password-user",
		PasswordHash:           oldHash,
		RequiresPasswordChange: true,
	})
	require.NoError(t, err)

	_, err = userSvc.SetPassword(ctx, user, "new-password")
	require.NoError(t, err)

	updated, err := userSvc.GetUserByID(ctx, user.ID)
	require.NoError(t, err)
	require.NoError(t, userSvc.ValidatePassword(updated.PasswordHash, "new-password"))
	require.Error(t, userSvc.ValidatePassword(updated.PasswordHash, "old-password"))
	require.False(t, updated.RequiresPasswordChange)
}

func TestDeleteUserAllowsDeletingNonAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	nonAdmin := createTestUser(t, userSvc, "user-1", "user")

	err := userSvc.DeleteUser(ctx, nonAdmin.ID, nil)
	require.NoError(t, err)

	_, err = userSvc.GetUserByID(ctx, nonAdmin.ID)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestDeleteUserAllowsDeletingAdminWhenAnotherAdminExists(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	adminToDelete := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, adminToDelete.ID)
	backup := createTestUser(t, userSvc, "admin-2", "backup")
	grantGlobalAdmin(t, roleSvc, backup.ID)

	err := userSvc.DeleteUser(ctx, adminToDelete.ID, nil)
	require.NoError(t, err)

	_, err = userSvc.GetUserByID(ctx, adminToDelete.ID)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestListUsersPaginatedSetsCanDeleteFromGlobalAdminCount(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	lastAdmin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, lastAdmin.ID)
	nonAdmin := createTestUser(t, userSvc, "user-1", "user")

	users, _, err := userSvc.ListUsersPaginated(ctx, pagination.QueryParams{
		Params:     pagination.Params{Start: 0, Limit: 20},
		SortParams: pagination.SortParams{Sort: "Username", Order: "asc"},
		Filters:    map[string]string{},
	})
	require.NoError(t, err)
	require.Len(t, users, 2)

	canDeleteByID := make(map[string]bool, len(users))
	for _, userRecord := range users {
		canDeleteByID[userRecord.ID] = userRecord.CanDelete
	}

	require.False(t, canDeleteByID[lastAdmin.ID])
	require.True(t, canDeleteByID[nonAdmin.ID])
}

func TestDeleteUserRejectsDeletingOnlyCustomAllPermissionsAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	customAdmin := createTestUser(t, userSvc, "custom-admin", "custom-admin")
	customRole, err := roleSvc.CreateRole(ctx, "Custom Admin", nil, authz.AllPermissions())
	require.NoError(t, err)
	require.NoError(t, roleSvc.SetUserAssignments(ctx, customAdmin.ID, []models.UserRoleAssignment{
		{RoleID: customRole.ID, EnvironmentID: nil},
	}))

	err = userSvc.DeleteUser(ctx, customAdmin.ID, nil)
	require.ErrorIs(t, err, ErrCannotRemoveLastAdmin)

	users, _, err := userSvc.ListUsersPaginated(ctx, pagination.QueryParams{
		Params:     pagination.Params{Start: 0, Limit: 20},
		SortParams: pagination.SortParams{Sort: "Username", Order: "asc"},
		Filters:    map[string]string{},
	})
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.False(t, users[0].CanDelete)
	require.True(t, users[0].IsGlobalAdmin)
}

func TestUpdateUserPersistsFontSizeAndMapsToDto(t *testing.T) {
	userSvc, _ := setupUserAndRoleServices(t)
	ctx := context.Background()

	u := createTestUser(t, userSvc, "user-1", "fontuser")
	require.Nil(t, u.FontSize, "new users default to no explicit font size")

	u.FontSize = new(16)
	_, err := userSvc.UpdateUser(ctx, u, nil)
	require.NoError(t, err)

	reloaded, err := userSvc.GetUserByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.FontSize)
	require.Equal(t, 16, *reloaded.FontSize)

	dto, err := userSvc.ToUserResponseDto(ctx, *reloaded)
	require.NoError(t, err)
	require.NotNil(t, dto.FontSize)
	require.Equal(t, 16, *dto.FontSize)
}

func TestUserDtoExposesLastLogin(t *testing.T) {
	userSvc, _ := setupUserAndRoleServices(t)
	ctx := context.Background()

	u := createTestUser(t, userSvc, "user-1", "last-login-user")

	dto, err := userSvc.ToUserResponseDto(ctx, *u)
	require.NoError(t, err)
	require.Nil(t, dto.LastLogin, "a user who has never signed in has no last login")

	loginAt := time.Date(2026, 8, 2, 15, 4, 5, 0, time.UTC)
	u.LastLogin = &loginAt
	_, err = userSvc.UpdateUser(ctx, u, nil)
	require.NoError(t, err)

	reloaded, err := userSvc.GetUserByID(ctx, u.ID)
	require.NoError(t, err)
	dto, err = userSvc.ToUserResponseDto(ctx, *reloaded)
	require.NoError(t, err)
	require.NotNil(t, dto.LastLogin)
	require.Equal(t, "2026-08-02T15:04:05Z", *dto.LastLogin)
}

func TestUpdateUserPersistsTimeFormatAndMapsToDto(t *testing.T) {
	userSvc, _ := setupUserAndRoleServices(t)
	ctx := context.Background()

	u := createTestUser(t, userSvc, "user-1", "time-format-user")
	require.Equal(t, user.TimeFormatAuto, u.TimeFormat)

	for _, timeFormat := range []user.TimeFormat{
		user.TimeFormatAuto,
		user.TimeFormat12Hour,
		user.TimeFormat24Hour,
	} {
		t.Run(string(timeFormat), func(t *testing.T) {
			u.TimeFormat = timeFormat
			_, err := userSvc.UpdateUser(ctx, u, nil)
			require.NoError(t, err)

			reloaded, err := userSvc.GetUserByID(ctx, u.ID)
			require.NoError(t, err)
			require.Equal(t, timeFormat, reloaded.TimeFormat)

			dto, err := userSvc.ToUserResponseDto(ctx, *reloaded)
			require.NoError(t, err)
			require.Equal(t, timeFormat, dto.TimeFormat)
		})
	}
}

func TestUpdateUserRejectsNonAdminActorEditingAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	manager := createTestUser(t, userSvc, "mgr-1", "manager")
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersUpdate})
	require.NoError(t, err)
	require.NoError(t, roleSvc.SetUserAssignments(ctx, manager.ID, []models.UserRoleAssignment{
		{RoleID: managerRole.ID, EnvironmentID: nil},
	}))
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	admin.PasswordHash = "attacker-controlled-hash"
	_, err = userSvc.UpdateUser(ctx, admin, managerPerms)
	require.ErrorIs(t, err, ErrInsufficientPrivilege)

	reloaded, err := userSvc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.NotEqual(t, "attacker-controlled-hash", reloaded.PasswordHash)
}

func TestUpdateUserAllowsGlobalAdminActorEditingAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	other := createTestUser(t, userSvc, "admin-2", "backup")
	grantGlobalAdmin(t, roleSvc, other.ID)
	otherPerms, err := roleSvc.ResolvePermissions(ctx, other)
	require.NoError(t, err)

	admin.DisplayName = new("Renamed Admin")
	actorCtx := context.WithValue(ctx, models.CurrentUserContextKey{}, other)
	_, err = userSvc.UpdateUser(actorCtx, admin, otherPerms)
	require.NoError(t, err)
}

func TestUpdateUserAllowsNonAdminActorEditingNonAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	manager := createTestUser(t, userSvc, "mgr-1", "manager")
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersUpdate})
	require.NoError(t, err)
	require.NoError(t, roleSvc.SetUserAssignments(ctx, manager.ID, []models.UserRoleAssignment{
		{RoleID: managerRole.ID, EnvironmentID: nil},
	}))
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	target := createTestUser(t, userSvc, "user-1", "plain-user")
	target.DisplayName = new("New Name")
	_, err = userSvc.UpdateUser(ctx, target, managerPerms)
	require.NoError(t, err)
}

func TestUpdateUserAllowsSelfEditByNonAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	adminPerms, err := roleSvc.ResolvePermissions(ctx, admin)
	require.NoError(t, err)

	admin.DisplayName = new("Self Rename")
	actorCtx := context.WithValue(ctx, models.CurrentUserContextKey{}, admin)
	_, err = userSvc.UpdateUser(actorCtx, admin, adminPerms)
	require.NoError(t, err)
}

func TestDeleteUserRejectsNonAdminActorDeletingAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	backup := createTestUser(t, userSvc, "admin-2", "backup")
	grantGlobalAdmin(t, roleSvc, backup.ID)
	manager := createTestUser(t, userSvc, "mgr-1", "manager")
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersDelete})
	require.NoError(t, err)
	require.NoError(t, roleSvc.SetUserAssignments(ctx, manager.ID, []models.UserRoleAssignment{
		{RoleID: managerRole.ID, EnvironmentID: nil},
	}))
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	// Two admins exist, so the last-admin guard would not fire — the
	// actor-vs-target privilege check is what must block this delete.
	err = userSvc.DeleteUser(ctx, admin.ID, managerPerms)
	require.ErrorIs(t, err, ErrInsufficientPrivilege)

	stillThere, err := userSvc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, admin.ID, stillThere.ID)
}

func TestDeleteUserAllowsGlobalAdminActorDeletingAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	other := createTestUser(t, userSvc, "admin-2", "backup")
	grantGlobalAdmin(t, roleSvc, other.ID)
	otherPerms, err := roleSvc.ResolvePermissions(ctx, other)
	require.NoError(t, err)

	actorCtx := context.WithValue(ctx, models.CurrentUserContextKey{}, other)
	require.NoError(t, userSvc.DeleteUser(actorCtx, admin.ID, otherPerms))
}

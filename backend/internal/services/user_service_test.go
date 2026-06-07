package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// setupUserAndRoleServices wires both services together the way bootstrap
// does, so the legacy-admin-guard tests exercise the real RBAC path.
func setupUserAndRoleServices(t *testing.T) (*UserService, *RoleService) {
	t.Helper()
	db := setupAuthServiceTestDB(t)
	role := NewRoleService(db)
	require.NoError(t, role.EnsureBuiltInRoles(context.Background()))
	user := NewUserService(db).WithRoleService(role)
	return user, role
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

	err := userSvc.DeleteUser(ctx, admin.ID)
	require.ErrorIs(t, err, ErrCannotRemoveLastAdmin)

	stillThere, err := userSvc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, admin.ID, stillThere.ID)
}

func TestDeleteUserAllowsDeletingNonAdmin(t *testing.T) {
	userSvc, roleSvc := setupUserAndRoleServices(t)
	ctx := context.Background()

	admin := createTestUser(t, userSvc, "admin-1", "arcane")
	grantGlobalAdmin(t, roleSvc, admin.ID)
	nonAdmin := createTestUser(t, userSvc, "user-1", "user")

	err := userSvc.DeleteUser(ctx, nonAdmin.ID)
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

	err := userSvc.DeleteUser(ctx, adminToDelete.ID)
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
		SortParams: pagination.SortParams{Sort: "Username", Order: pagination.SortOrder("asc")},
		Filters:    map[string]string{},
	})
	require.NoError(t, err)
	require.Len(t, users, 2)

	canDeleteByID := make(map[string]bool, len(users))
	for _, user := range users {
		canDeleteByID[user.ID] = user.CanDelete
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

	err = userSvc.DeleteUser(ctx, customAdmin.ID)
	require.ErrorIs(t, err, ErrCannotRemoveLastAdmin)

	users, _, err := userSvc.ListUsersPaginated(ctx, pagination.QueryParams{
		Params:     pagination.Params{Start: 0, Limit: 20},
		SortParams: pagination.SortParams{Sort: "Username", Order: pagination.SortOrder("asc")},
		Filters:    map[string]string{},
	})
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.False(t, users[0].CanDelete)
	require.True(t, users[0].IsGlobalAdmin)
}

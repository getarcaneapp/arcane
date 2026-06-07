package services

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func createTestUser(t *testing.T, svc *UserService, id, username string, roles models.StringSlice) *models.User {
	t.Helper()

	user := &models.User{
		BaseModel: models.BaseModel{ID: id},
		Username:  username,
		Roles:     roles,
	}

	created, err := svc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	return created
}

func TestDeleteUserRejectsDeletingOnlyAdmin(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	admin := createTestUser(t, svc, "admin-1", "arcane", models.StringSlice{"Admin"})

	err := svc.DeleteUser(ctx, admin.ID)
	require.ErrorIs(t, err, ErrCannotRemoveLastAdmin)

	stillThere, err := svc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, admin.ID, stillThere.ID)
}

func TestDeleteUserAllowsDeletingNonAdmin(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	createTestUser(t, svc, "admin-1", "arcane", models.StringSlice{"admin"})
	nonAdmin := createTestUser(t, svc, "user-1", "user", models.StringSlice{"user"})

	err := svc.DeleteUser(ctx, nonAdmin.ID)
	require.NoError(t, err)

	_, err = svc.GetUserByID(ctx, nonAdmin.ID)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestDeleteUserAllowsDeletingAdminWhenAnotherAdminExists(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	adminToDelete := createTestUser(t, svc, "admin-1", "arcane", models.StringSlice{"Admin"})
	createTestUser(t, svc, "admin-2", "backup", models.StringSlice{"admin"})

	err := svc.DeleteUser(ctx, adminToDelete.ID)
	require.NoError(t, err)

	_, err = svc.GetUserByID(ctx, adminToDelete.ID)
	require.ErrorIs(t, err, ErrUserNotFound)
}

func TestUpdateUserRejectsRemovingAdminFromOnlyAdmin(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	admin := createTestUser(t, svc, "admin-1", "arcane", models.StringSlice{"ADMIN"})
	admin.Roles = models.StringSlice{"user"}

	_, err := svc.UpdateUser(ctx, admin)
	require.ErrorIs(t, err, ErrCannotRemoveLastAdmin)

	persisted, err := svc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, models.StringSlice{"ADMIN"}, persisted.Roles)
}

func TestUpdateUserAllowsRemovingAdminWhenAnotherAdminExists(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	admin := createTestUser(t, svc, "admin-1", "arcane", models.StringSlice{"admin"})
	createTestUser(t, svc, "admin-2", "backup", models.StringSlice{"ADMIN"})

	admin.Roles = models.StringSlice{"user"}

	updated, err := svc.UpdateUser(ctx, admin)
	require.NoError(t, err)
	require.Equal(t, models.StringSlice{"user"}, updated.Roles)

	persisted, err := svc.GetUserByID(ctx, admin.ID)
	require.NoError(t, err)
	require.Equal(t, models.StringSlice{"user"}, persisted.Roles)
}

func TestListUsersPaginatedSetsCanDeleteFromGlobalAdminCount(t *testing.T) {
	db := setupAuthServiceTestDB(t)
	svc := NewUserService(db)
	ctx := context.Background()

	lastAdmin := createTestUser(t, svc, "admin-1", "arcane", models.StringSlice{"admin"})
	nonAdmin := createTestUser(t, svc, "user-1", "user", models.StringSlice{"user"})

	users, _, err := svc.ListUsersPaginated(ctx, pagination.QueryParams{
		PaginationParams: pagination.PaginationParams{Start: 0, Limit: 20},
		SortParams:       pagination.SortParams{Sort: "Username", Order: pagination.SortOrder("asc")},
		Filters:          map[string]string{},
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

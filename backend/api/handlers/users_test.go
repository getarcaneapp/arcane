package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	sqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
)

func setupUserHandlerTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))

	return &database.DB{DB: db}
}

func createHandlerTestUser(t *testing.T, svc *services.UserService, id, username string, _ models.StringSlice) *models.User {
	t.Helper()

	user := &models.User{
		BaseModel: models.BaseModel{ID: id},
		Username:  username,
	}

	created, err := svc.CreateUser(context.Background(), user)
	require.NoError(t, err)

	return created
}

func adminContext() context.Context {
	return context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, authz.SudoPermissionSet())
}

func TestDeleteUserReturnsConflictForLastAdmin(t *testing.T) {
	db := setupUserHandlerTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Role{}, &models.UserRoleAssignment{}, &models.Environment{}))
	roleSvc := services.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(context.Background()))
	userSvc := services.NewUserService(db).WithRoleService(roleSvc)
	handler := &UserHandler{userService: userSvc}
	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{})
	require.NoError(t, roleSvc.SetUserAssignments(context.Background(), admin.ID, []models.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin, EnvironmentID: nil},
	}))

	_, err := handler.DeleteUser(adminContext(), &DeleteUserInput{UserID: admin.ID})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusConflict, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), services.ErrCannotRemoveLastAdmin.Error())
}

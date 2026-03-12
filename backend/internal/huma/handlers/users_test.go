package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	usertypes "github.com/getarcaneapp/arcane/types/user"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
)

func setupUserHandlerTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))

	return &database.DB{DB: db}
}

func createHandlerTestUser(t *testing.T, svc *services.UserService, id, username string, roles models.StringSlice) *models.User {
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

func adminContext() context.Context {
	return context.WithValue(context.Background(), humamw.ContextKeyUserIsAdmin, true)
}

func TestDeleteUserReturnsConflictForLastAdmin(t *testing.T) {
	db := setupUserHandlerTestDB(t)
	userSvc := services.NewUserService(db)
	handler := &UserHandler{userService: userSvc}
	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{"admin"})

	_, err := handler.DeleteUser(adminContext(), &DeleteUserInput{UserID: admin.ID})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusConflict, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), services.ErrCannotRemoveLastAdmin.Error())
}

func TestUpdateUserReturnsConflictWhenRemovingLastAdminRole(t *testing.T) {
	db := setupUserHandlerTestDB(t)
	userSvc := services.NewUserService(db)
	handler := &UserHandler{userService: userSvc}
	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{"ADMIN"})

	_, err := handler.UpdateUser(adminContext(), &UpdateUserInput{
		UserID: admin.ID,
		Body: usertypes.UpdateUser{
			Roles: []string{"user"},
		},
	})
	require.Error(t, err)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusConflict, statusErr.GetStatus())
	require.Contains(t, statusErr.Error(), services.ErrCannotRemoveLastAdmin.Error())
}

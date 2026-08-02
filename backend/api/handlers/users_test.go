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
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/types/v2/user"
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

// callerContext returns a context carrying the given user and their resolved
// PermissionSet, matching what the auth bridge attaches in production.
func callerContext(u *models.User, ps *authz.PermissionSet) context.Context {
	ctx := context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, ps)
	return context.WithValue(ctx, models.CurrentUserContextKey{}, u)
}

func grantRole(t *testing.T, roleSvc *services.RoleService, userID, roleID string) {
	t.Helper()
	require.NoError(t, roleSvc.SetUserAssignments(context.Background(), userID, []models.UserRoleAssignment{
		{RoleID: roleID, EnvironmentID: nil},
	}))
}

func setupPrivilegedUserHandler(t *testing.T) (*services.UserService, *services.RoleService, *UserHandler) {
	t.Helper()
	db := setupUserHandlerTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.Role{}, &models.UserRoleAssignment{}, &models.Environment{}))
	roleSvc := services.NewRoleService(db)
	require.NoError(t, roleSvc.EnsureBuiltInRoles(context.Background()))
	userSvc := services.NewUserService(db).WithRoleService(roleSvc)
	return userSvc, roleSvc, &UserHandler{userService: userSvc}
}

func TestUpdateUserRejectsPasswordResetOnAdminByDelegatedManager(t *testing.T) {
	userSvc, roleSvc, handler := setupPrivilegedUserHandler(t)
	ctx := context.Background()

	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{})
	grantRole(t, roleSvc, admin.ID, authz.BuiltInRoleAdmin)
	manager := createHandlerTestUser(t, userSvc, "mgr-1", "manager", models.StringSlice{})
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersUpdate})
	require.NoError(t, err)
	grantRole(t, roleSvc, manager.ID, managerRole.ID)
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	password := "attacker1234"
	_, err = handler.UpdateUser(callerContext(manager, managerPerms), &UpdateUserInput{
		UserID: admin.ID,
		Body:   user.UpdateUser{Password: &password},
	})
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusForbidden, statusErr.GetStatus())

	reloaded, getErr := userSvc.GetUserByID(ctx, admin.ID)
	require.NoError(t, getErr)
	require.NotEqual(t, password, reloaded.PasswordHash)
}

func TestUpdateUserRejectsProfileEditOnAdminByDelegatedManager(t *testing.T) {
	userSvc, roleSvc, handler := setupPrivilegedUserHandler(t)
	ctx := context.Background()

	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{})
	grantRole(t, roleSvc, admin.ID, authz.BuiltInRoleAdmin)
	manager := createHandlerTestUser(t, userSvc, "mgr-1", "manager", models.StringSlice{})
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersUpdate})
	require.NoError(t, err)
	grantRole(t, roleSvc, manager.ID, managerRole.ID)
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	displayName := "pwned"
	_, err = handler.UpdateUser(callerContext(manager, managerPerms), &UpdateUserInput{
		UserID: admin.ID,
		Body:   user.UpdateUser{DisplayName: &displayName},
	})
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusForbidden, statusErr.GetStatus())
}

func TestUpdateUserRejectsPasswordChangeOnOtherUserByNonAdmin(t *testing.T) {
	userSvc, roleSvc, handler := setupPrivilegedUserHandler(t)
	ctx := context.Background()

	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{})
	grantRole(t, roleSvc, admin.ID, authz.BuiltInRoleAdmin)
	victim := createHandlerTestUser(t, userSvc, "user-1", "victim", models.StringSlice{})
	manager := createHandlerTestUser(t, userSvc, "mgr-1", "manager", models.StringSlice{})
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersUpdate})
	require.NoError(t, err)
	grantRole(t, roleSvc, manager.ID, managerRole.ID)
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	password := "newpass123"
	_, err = handler.UpdateUser(callerContext(manager, managerPerms), &UpdateUserInput{
		UserID: victim.ID,
		Body:   user.UpdateUser{Password: &password},
	})
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusForbidden, statusErr.GetStatus())
}

func TestUpdateUserAllowsGlobalAdminEditingAdmin(t *testing.T) {
	userSvc, roleSvc, handler := setupPrivilegedUserHandler(t)
	ctx := context.Background()

	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{})
	grantRole(t, roleSvc, admin.ID, authz.BuiltInRoleAdmin)
	other := createHandlerTestUser(t, userSvc, "admin-2", "backup", models.StringSlice{})
	grantRole(t, roleSvc, other.ID, authz.BuiltInRoleAdmin)
	otherPerms, err := roleSvc.ResolvePermissions(ctx, other)
	require.NoError(t, err)

	displayName := "Renamed Admin"
	out, err := handler.UpdateUser(callerContext(other, otherPerms), &UpdateUserInput{
		UserID: admin.ID,
		Body:   user.UpdateUser{DisplayName: &displayName},
	})
	require.NoError(t, err)
	require.NotNil(t, out)
}

func TestDeleteUserRejectsDelegatedManagerDeletingAdmin(t *testing.T) {
	userSvc, roleSvc, handler := setupPrivilegedUserHandler(t)
	ctx := context.Background()

	admin := createHandlerTestUser(t, userSvc, "admin-1", "arcane", models.StringSlice{})
	grantRole(t, roleSvc, admin.ID, authz.BuiltInRoleAdmin)
	backup := createHandlerTestUser(t, userSvc, "admin-2", "backup", models.StringSlice{})
	grantRole(t, roleSvc, backup.ID, authz.BuiltInRoleAdmin)
	manager := createHandlerTestUser(t, userSvc, "mgr-1", "manager", models.StringSlice{})
	managerRole, err := roleSvc.CreateRole(ctx, "User Manager", nil, []string{authz.PermUsersDelete})
	require.NoError(t, err)
	grantRole(t, roleSvc, manager.ID, managerRole.ID)
	managerPerms, err := roleSvc.ResolvePermissions(ctx, manager)
	require.NoError(t, err)

	_, err = handler.DeleteUser(callerContext(manager, managerPerms), &DeleteUserInput{UserID: admin.ID})
	require.Error(t, err)
	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusForbidden, statusErr.GetStatus())

	_, getErr := userSvc.GetUserByID(ctx, admin.ID)
	require.NoError(t, getErr)
}

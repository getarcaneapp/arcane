package admin

import (
	"context"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
	"github.com/getarcaneapp/arcane/types/v2/auth"
)

func newResetPasswordTestDBInternal(t *testing.T) *database.DB {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(
		&models.User{},
		&models.UserSession{},
		&models.Role{},
		&models.UserRoleAssignment{},
	))

	db := &database.DB{DB: gormDB}
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

func createGlobalAdminInternal(t *testing.T, db *database.DB) (*models.User, *services.RoleService, *services.UserService) {
	t.Helper()
	ctx := context.Background()
	roleService := services.NewRoleService(db)
	require.NoError(t, roleService.EnsureBuiltInRoles(ctx))
	userService := services.NewUserService(db).WithRoleService(roleService)
	passwordHash, err := userService.HashPassword("old-password")
	require.NoError(t, err)
	adminUser, err := userService.CreateUser(ctx, &models.User{
		BaseModel:              models.BaseModel{ID: "admin-1"},
		Username:               "arcane",
		PasswordHash:           passwordHash,
		RequiresPasswordChange: true,
	})
	require.NoError(t, err)
	require.NoError(t, roleService.SetUserAssignments(ctx, adminUser.ID, []models.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin},
	}))

	return adminUser, roleService, userService
}

func TestResetPasswordInternalResetsPasswordAndRevokesSessions(t *testing.T) {
	db := newResetPasswordTestDBInternal(t)
	ctx := context.Background()
	adminUser, _, userService := createGlobalAdminInternal(t, db)
	sessionService := services.NewSessionService(db)
	firstSession, _, err := sessionService.CreateSession(ctx, adminUser.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	secondSession, _, err := sessionService.CreateSession(ctx, adminUser.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	require.NoError(t, resetPasswordInternal(ctx, db, "arcane", "new-password", validation.PasswordPolicyBasic))

	updatedUser, err := userService.GetUserByID(ctx, adminUser.ID)
	require.NoError(t, err)
	require.NoError(t, userService.ValidatePassword(updatedUser.PasswordHash, "new-password"))
	require.Error(t, userService.ValidatePassword(updatedUser.PasswordHash, "old-password"))
	require.False(t, updatedUser.RequiresPasswordChange)

	revokedFirst, err := sessionService.GetSessionByID(ctx, firstSession.ID)
	require.NoError(t, err)
	require.NotNil(t, revokedFirst.RevokedAt)
	revokedSecond, err := sessionService.GetSessionByID(ctx, secondSession.ID)
	require.NoError(t, err)
	require.NotNil(t, revokedSecond.RevokedAt)
}

func TestResetPasswordInternalRollsBackWhenSessionRevocationFails(t *testing.T) {
	db := newResetPasswordTestDBInternal(t)
	ctx := context.Background()
	adminUser, _, userService := createGlobalAdminInternal(t, db)
	sessionService := services.NewSessionService(db)
	session, _, err := sessionService.CreateSession(ctx, adminUser.ID, time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TRIGGER fail_user_session_revocation
		BEFORE UPDATE OF revoked_at ON user_sessions
		WHEN NEW.revoked_at IS NOT NULL
		BEGIN
			SELECT RAISE(ABORT, 'forced session revocation failure');
		END;
	`).Error)

	err = resetPasswordInternal(ctx, db, adminUser.Username, "new-password", validation.PasswordPolicyBasic)
	require.Error(t, err)
	require.ErrorContains(t, err, "forced session revocation failure")

	unchangedUser, err := userService.GetUserByID(ctx, adminUser.ID)
	require.NoError(t, err)
	require.NoError(t, userService.ValidatePassword(unchangedUser.PasswordHash, "old-password"))
	require.True(t, unchangedUser.RequiresPasswordChange)
	unchangedSession, err := sessionService.GetSessionByID(ctx, session.ID)
	require.NoError(t, err)
	require.Nil(t, unchangedSession.RevokedAt)
}

func TestResetPasswordInternalRejectsNonAdmin(t *testing.T) {
	db := newResetPasswordTestDBInternal(t)
	ctx := context.Background()
	_, _, userService := createGlobalAdminInternal(t, db)
	passwordHash, err := userService.HashPassword("old-password")
	require.NoError(t, err)
	nonAdmin, err := userService.CreateUser(ctx, &models.User{
		BaseModel:              models.BaseModel{ID: "user-1"},
		Username:               "operator",
		PasswordHash:           passwordHash,
		RequiresPasswordChange: true,
	})
	require.NoError(t, err)

	err = resetPasswordInternal(ctx, db, nonAdmin.Username, "new-password", validation.PasswordPolicyBasic)
	require.Error(t, err)
	require.ErrorContains(t, err, "effective global administrator")

	unchangedUser, err := userService.GetUserByID(ctx, nonAdmin.ID)
	require.NoError(t, err)
	require.NoError(t, userService.ValidatePassword(unchangedUser.PasswordHash, "old-password"))
	require.True(t, unchangedUser.RequiresPasswordChange)
}

func TestEnsurePasswordResetEnabledInternal(t *testing.T) {
	require.ErrorContains(t, ensurePasswordResetEnabledInternal(&config.Config{}), "ALLOW_CLI_PASSWORD_RESET=true")
	require.NoError(t, ensurePasswordResetEnabledInternal(&config.Config{AllowCLIPasswordReset: true}))
}

func TestValidatePasswordPairInternal(t *testing.T) {
	require.ErrorContains(t, validatePasswordPairInternal("short", "short", validation.PasswordPolicyBasic), "at least 8")
	require.ErrorContains(t, validatePasswordPairInternal("new-password", "different", validation.PasswordPolicyBasic), "do not match")
	require.NoError(t, validatePasswordPairInternal("new-password", "new-password", validation.PasswordPolicyBasic))
}

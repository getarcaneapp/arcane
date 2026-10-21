package admin

import (
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/validation"
)

func newResetPasswordTestDB(t *testing.T) *database.DB {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(
		&user.User{},
		&session.UserSession{},
		&role.Role{},
		&role.UserRoleAssignment{},
	))

	db := &database.DB{DB: gormDB}
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

func createGlobalAdmin(t *testing.T, db *database.DB) (*user.User, *user.UserService) {
	t.Helper()
	ctx := t.Context()
	roleService := role.NewRoleService(db)
	require.NoError(t, roleService.EnsureBuiltInRoles(ctx))
	userService := user.NewUserService(db, roleService, session.RevokeAllUserSessionsExceptInDB)
	passwordHash, err := userService.HashPassword(t.Context(), "old-password")
	require.NoError(t, err)
	adminUser, err := userService.CreateUser(ctx, &user.User{
		ID:                     "admin-1",
		Username:               "arcane",
		PasswordHash:           passwordHash,
		RequiresPasswordChange: true,
	})
	require.NoError(t, err)
	require.NoError(t, roleService.ReplaceUserAssignments(ctx, adminUser.ID, role.RoleAssignmentSourceManual, []role.UserRoleAssignment{
		{RoleID: authz.BuiltInRoleAdmin},
	}))

	return adminUser, userService
}

func TestResetPasswordInternalResetsPasswordAndRevokesSessions(t *testing.T) {
	db := newResetPasswordTestDB(t)
	ctx := t.Context()
	adminUser, userService := createGlobalAdmin(t, db)
	sessionService := session.NewSessionService(db)
	firstSession, _, err := sessionService.CreateSession(ctx, adminUser.ID, time.Now().Add(time.Hour), types.SessionMeta{})
	require.NoError(t, err)
	secondSession, _, err := sessionService.CreateSession(ctx, adminUser.ID, time.Now().Add(time.Hour), types.SessionMeta{})
	require.NoError(t, err)

	require.NoError(t, resetPasswordInternal(ctx, db, "arcane", "new-password", validation.PasswordPolicyBasic))

	updatedUser, err := userService.GetUserByID(ctx, adminUser.ID)
	require.NoError(t, err)
	require.NoError(t, userService.ValidatePassword(t.Context(), updatedUser.PasswordHash, "new-password"))
	require.Error(t, userService.ValidatePassword(t.Context(), updatedUser.PasswordHash, "old-password"))
	require.False(t, updatedUser.RequiresPasswordChange)

	revokedFirst, err := sessionService.GetSessionByID(ctx, firstSession.ID)
	require.NoError(t, err)
	require.NotNil(t, revokedFirst.RevokedAt)
	revokedSecond, err := sessionService.GetSessionByID(ctx, secondSession.ID)
	require.NoError(t, err)
	require.NotNil(t, revokedSecond.RevokedAt)
}

func TestResetPasswordInternalRollsBackWhenSessionRevocationFails(t *testing.T) {
	db := newResetPasswordTestDB(t)
	ctx := t.Context()
	adminUser, userService := createGlobalAdmin(t, db)
	sessionService := session.NewSessionService(db)
	localSession, _, err := sessionService.CreateSession(ctx, adminUser.ID, time.Now().Add(time.Hour), types.SessionMeta{})
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
	require.NoError(t, userService.ValidatePassword(t.Context(), unchangedUser.PasswordHash, "old-password"))
	require.True(t, unchangedUser.RequiresPasswordChange)
	unchangedSession, err := sessionService.GetSessionByID(ctx, localSession.ID)
	require.NoError(t, err)
	require.Nil(t, unchangedSession.RevokedAt)
}

func TestResetPasswordInternalRejectsNonAdmin(t *testing.T) {
	db := newResetPasswordTestDB(t)
	ctx := t.Context()
	_, userService := createGlobalAdmin(t, db)
	passwordHash, err := userService.HashPassword(t.Context(), "old-password")
	require.NoError(t, err)
	nonAdmin, err := userService.CreateUser(ctx, &user.User{
		ID:                     "user-1",
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
	require.NoError(t, userService.ValidatePassword(t.Context(), unchangedUser.PasswordHash, "old-password"))
	require.True(t, unchangedUser.RequiresPasswordChange)
}

func TestEnsurePasswordResetEnabled(t *testing.T) {
	require.ErrorContains(t, ensurePasswordResetEnabledInternal(&config.Config{}), "ALLOW_CLI_PASSWORD_RESET=true")
	require.NoError(t, ensurePasswordResetEnabledInternal(&config.Config{AllowCLIPasswordReset: true}))
}

func TestValidatePasswordPair(t *testing.T) {
	require.ErrorContains(t, validatePasswordPairInternal("short", "short", validation.PasswordPolicyBasic), "at least 8")
	require.ErrorContains(t, validatePasswordPairInternal("new-password", "different", validation.PasswordPolicyBasic), "do not match")
	require.NoError(t, validatePasswordPairInternal("new-password", "new-password", validation.PasswordPolicyBasic))
}

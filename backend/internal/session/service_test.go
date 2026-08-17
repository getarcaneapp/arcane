package session

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSessionService_RotateRefreshTokenRequiresCurrentHash(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)
	require.NoError(t, db.Create(&common.User{
		BaseModel: database.BaseModel{ID: "u-session"},
		Username:  "session-user",
	}).Error)

	sessionSvc := NewSessionService(db)
	session, refreshJTI, err := sessionSvc.CreateSession(ctx, "u-session", time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	rotated, newJTI, err := sessionSvc.RotateRefreshToken(ctx, session.ID, refreshJTI, auth.SessionMeta{})
	require.NoError(t, err)
	require.NotEmpty(t, newJTI)
	require.NotEqual(t, refreshJTI, newJTI)
	require.Equal(t, hashRefreshJTIInternal(newJTI), rotated.RefreshTokenHash)

	_, _, err = sessionSvc.RotateRefreshToken(ctx, session.ID, refreshJTI, auth.SessionMeta{})
	require.ErrorIs(t, err, common.ErrInvalidToken)
}

func TestSessionService_DeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)
	require.NoError(t, db.Create(&common.User{
		BaseModel: database.BaseModel{ID: "u-cleanup"},
		Username:  "cleanup-user",
	}).Error)

	sessionSvc := NewSessionService(db)
	expired, _, err := sessionSvc.CreateSession(ctx, "u-cleanup", time.Now().Add(-time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	oldRevoked, _, err := sessionSvc.CreateSession(ctx, "u-cleanup", time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	active, _, err := sessionSvc.CreateSession(ctx, "u-cleanup", time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	oldRevokedAt := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, db.WithContext(ctx).Model(&UserSession{}).
		Where("id = ?", oldRevoked.ID).
		Update("revoked_at", oldRevokedAt).Error)

	deleted, err := sessionSvc.DeleteExpiredSessions(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	var remaining []UserSession
	require.NoError(t, db.WithContext(ctx).Order("id").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, active.ID, remaining[0].ID)

	var deletedCount int64
	require.NoError(t, db.WithContext(ctx).Model(&UserSession{}).
		Where("id IN ?", []string{expired.ID, oldRevoked.ID}).
		Count(&deletedCount).Error)
	require.Zero(t, deletedCount)
}

func setupAuthServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&settings.SettingVariable{},
		&common.User{},
		&UserSession{},
		&role.Role{},
		&role.UserRoleAssignment{},
		&role.ApiKeyPermission{},
		&role.OidcRoleMapping{},
	))
	// environments and api_keys live in packages that import this one; the
	// in-package tests only need the tables to exist.
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS environments (id text PRIMARY KEY, created_at datetime, updated_at datetime, name text, api_url text, status text, enabled numeric, is_edge numeric, hidden numeric, last_seen datetime, last_edge_transport text, access_token text, api_key_id text, parent_environment_id text, swarm_node_id text)").Error)
	require.NoError(t, db.Exec("CREATE TABLE IF NOT EXISTS api_keys (id text PRIMARY KEY, created_at datetime, updated_at datetime, name text, description text, key_hash text, key_prefix text, kind text, user_id text, environment_id text, managed_by text, expires_at datetime, last_used_at datetime)").Error)
	return &database.DB{DB: db}
}

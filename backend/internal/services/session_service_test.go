package services

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/stretchr/testify/require"
)

func TestSessionService_RotateRefreshTokenRequiresCurrentHash(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	require.NoError(t, userSvc.db.Create(&models.User{
		BaseModel: models.BaseModel{ID: "u-session"},
		Username:  "session-user",
		Roles:     models.StringSlice{"user"},
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
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestSessionService_DeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	db := setupAuthServiceTestDB(t)
	userSvc := NewUserService(db)
	require.NoError(t, userSvc.db.Create(&models.User{
		BaseModel: models.BaseModel{ID: "u-cleanup"},
		Username:  "cleanup-user",
		Roles:     models.StringSlice{"user"},
	}).Error)

	sessionSvc := NewSessionService(db)
	expired, _, err := sessionSvc.CreateSession(ctx, "u-cleanup", time.Now().Add(-time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	oldRevoked, _, err := sessionSvc.CreateSession(ctx, "u-cleanup", time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)
	active, _, err := sessionSvc.CreateSession(ctx, "u-cleanup", time.Now().Add(time.Hour), auth.SessionMeta{})
	require.NoError(t, err)

	oldRevokedAt := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, db.WithContext(ctx).Model(&models.UserSession{}).
		Where("id = ?", oldRevoked.ID).
		Update("revoked_at", oldRevokedAt).Error)

	deleted, err := sessionSvc.DeleteExpiredSessions(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	var remaining []models.UserSession
	require.NoError(t, db.WithContext(ctx).Order("id").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, active.ID, remaining[0].ID)

	var deletedCount int64
	require.NoError(t, db.WithContext(ctx).Model(&models.UserSession{}).
		Where("id IN ?", []string{expired.ID, oldRevoked.ID}).
		Count(&deletedCount).Error)
	require.Zero(t, deletedCount)
}

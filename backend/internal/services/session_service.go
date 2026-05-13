package services

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	pkgutils "github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/types/auth"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SessionService struct {
	db *database.DB
}

func NewSessionService(db *database.DB) *SessionService {
	return &SessionService{db: db}
}

func (s *SessionService) CreateSession(ctx context.Context, userID string, expiresAt time.Time, meta auth.SessionMeta) (*models.UserSession, string, error) {
	refreshJTI := uuid.NewString()
	refreshHash := hashRefreshJTIInternal(refreshJTI)

	now := time.Now()
	session := &models.UserSession{
		UserID:           userID,
		RefreshTokenHash: refreshHash,
		UserAgent:        pkgutils.StringPtrFromTrimmed(meta.UserAgent),
		IPAddress:        pkgutils.StringPtrFromTrimmed(meta.IPAddress),
		LastUsedAt:       now,
		ExpiresAt:        expiresAt,
	}

	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, "", fmt.Errorf("failed to create user session: %w", err)
	}

	return session, refreshJTI, nil
}

func (s *SessionService) GetSessionByID(ctx context.Context, sessionID string) (*models.UserSession, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrInvalidToken
	}

	var session models.UserSession
	if err := s.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, fmt.Errorf("failed to get user session: %w", err)
	}
	return &session, nil
}

func (s *SessionService) RotateRefreshToken(ctx context.Context, sessionID string, refreshJTI string, meta auth.SessionMeta) (*models.UserSession, string, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(refreshJTI) == "" {
		return nil, "", ErrInvalidToken
	}

	newRefreshJTI := uuid.NewString()
	newHash := hashRefreshJTIInternal(newRefreshJTI)

	now := time.Now()
	var rotated models.UserSession

	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var session models.UserSession
		if err := tx.Where("id = ?", sessionID).First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidToken
			}
			return fmt.Errorf("failed to get user session for rotation: %w", err)
		}
		if err := validateSessionActiveInternal(&session); err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(session.RefreshTokenHash), []byte(hashRefreshJTIInternal(refreshJTI))) != 1 {
			return ErrInvalidToken
		}

		updates := map[string]any{
			"refresh_token_hash": newHash,
			"last_used_at":       now,
			"updated_at":         now,
			"user_agent":         pkgutils.StringPtrFromTrimmed(meta.UserAgent),
			"ip_address":         pkgutils.StringPtrFromTrimmed(meta.IPAddress),
		}
		result := tx.Model(&models.UserSession{}).
			Where("id = ? AND refresh_token_hash = ? AND revoked_at IS NULL", session.ID, session.RefreshTokenHash).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to rotate refresh token: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrInvalidToken
		}
		rotated = session
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	rotated.RefreshTokenHash = newHash
	rotated.LastUsedAt = now
	rotated.UpdatedAt = &now
	rotated.UserAgent = pkgutils.StringPtrFromTrimmed(meta.UserAgent)
	rotated.IPAddress = pkgutils.StringPtrFromTrimmed(meta.IPAddress)

	return &rotated, newRefreshJTI, nil
}

func (s *SessionService) RevokeSession(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.UserSession{}).
		Where("id = ? AND revoked_at IS NULL", sessionID).
		Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("failed to revoke user session: %w", err)
	}
	return nil
}

func (s *SessionService) DeleteExpiredSessions(ctx context.Context, revokedRetention time.Duration) (int64, error) {
	now := time.Now()
	revokedCutoff := now.Add(-revokedRetention)
	result := s.db.WithContext(ctx).
		Where("expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)", now, revokedCutoff).
		Delete(&models.UserSession{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete expired user sessions: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func hashRefreshJTIInternal(jti string) string {
	sum := sha256.Sum256([]byte(jti))
	return hex.EncodeToString(sum[:])
}

// RevokeAllUserSessionsExcept revokes every active session for userID, leaving
// exceptSessionID active. Pass "" to revoke all sessions.
func (s *SessionService) RevokeAllUserSessionsExcept(ctx context.Context, userID, exceptSessionID string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrInvalidToken
	}

	now := time.Now()
	query := s.db.WithContext(ctx).Model(&models.UserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID)
	if strings.TrimSpace(exceptSessionID) != "" {
		query = query.Where("id <> ?", exceptSessionID)
	}
	if err := query.Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("failed to revoke user sessions: %w", err)
	}
	return nil
}

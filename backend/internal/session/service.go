package session

import (
	"cmp"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/dbutil"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"
)

type SessionService struct {
	db *database.DB
}

func NewSessionService(db *database.DB) *SessionService {
	return &SessionService{db: db}
}

func (s *SessionService) CreateSession(ctx context.Context, userID string, expiresAt time.Time, meta auth.SessionMeta) (*UserSession, string, error) {
	refreshJTI := uuid.New().String()
	refreshHash := kit.SHA256Hex(refreshJTI)

	now := time.Now()
	source := cmp.Or(strings.TrimSpace(meta.Source), UserSessionSourceLocal)
	session := &UserSession{
		UserID:           userID,
		RefreshTokenHash: refreshHash,
		UserAgent:        mo.EmptyableToOption(strings.TrimSpace(meta.UserAgent)).ToPointer(),
		IPAddress:        mo.EmptyableToOption(strings.TrimSpace(meta.IPAddress)).ToPointer(),
		Source:           source,
		MFAMethod:        mo.EmptyableToOption(strings.TrimSpace(meta.MFAMethod)).ToPointer(),
		MFAVerifiedAt:    meta.MFAVerifiedAt,
		LastUsedAt:       now,
		ExpiresAt:        expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, "", fmt.Errorf("failed to create user session: %w", err)
	}
	return session, refreshJTI, nil
}

func (s *SessionService) CreateFederatedSession(ctx context.Context, userID string, expiresAt time.Time, credentialID string) (*UserSession, error) {
	refreshHash := kit.SHA256Hex(uuid.New().String())
	now := time.Now()

	session := &UserSession{
		UserID:                userID,
		RefreshTokenHash:      refreshHash,
		Source:                UserSessionSourceFederated,
		FederatedCredentialID: mo.EmptyableToOption(strings.TrimSpace(credentialID)).ToPointer(),
		LastUsedAt:            now,
		ExpiresAt:             expiresAt,
	}
	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, fmt.Errorf("failed to create federated user session: %w", err)
	}
	return session, nil
}

func (s *SessionService) GetSessionByID(ctx context.Context, sessionID string) (*UserSession, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, common.ErrInvalidToken
	}

	var session UserSession
	if err := s.db.WithContext(ctx).Where("id = ?", sessionID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.ErrInvalidToken
		}
		return nil, fmt.Errorf("failed to get user session: %w", err)
	}
	return &session, nil
}

func (s *SessionService) RotateRefreshToken(ctx context.Context, sessionID, refreshJTI string, meta auth.SessionMeta) (*UserSession, string, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(refreshJTI) == "" {
		return nil, "", common.ErrInvalidToken
	}

	newRefreshJTI := uuid.New().String()
	newHash := kit.SHA256Hex(newRefreshJTI)

	now := time.Now()
	var rotated UserSession

	err := dbutil.WithTx(ctx, s.db.DB, func(tx *gorm.DB) error {
		var session UserSession
		if err := tx.Where("id = ?", sessionID).First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.ErrInvalidToken
			}
			return fmt.Errorf("failed to get user session for rotation: %w", err)
		}
		if err := ValidateActive(&session); err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(session.RefreshTokenHash), []byte(kit.SHA256Hex(refreshJTI))) != 1 {
			return common.ErrInvalidToken
		}

		updates := map[string]any{
			"refresh_token_hash": newHash,
			"last_used_at":       now,
			"updated_at":         now,
			"user_agent":         mo.EmptyableToOption(strings.TrimSpace(meta.UserAgent)).ToPointer(),
			"ip_address":         mo.EmptyableToOption(strings.TrimSpace(meta.IPAddress)).ToPointer(),
		}
		result := tx.Model(&UserSession{}).
			Where("id = ? AND refresh_token_hash = ? AND revoked_at IS NULL", session.ID, session.RefreshTokenHash).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to rotate refresh token: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return common.ErrInvalidToken
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
	rotated.UserAgent = mo.EmptyableToOption(strings.TrimSpace(meta.UserAgent)).ToPointer()
	rotated.IPAddress = mo.EmptyableToOption(strings.TrimSpace(meta.IPAddress)).ToPointer()

	return &rotated, newRefreshJTI, nil
}

func (s *SessionService) RevokeSession(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&UserSession{}).
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
		Delete(&UserSession{})
	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete expired user sessions: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// RevokeAllUserSessionsExcept revokes every active session for userID, leaving
// exceptSessionID active. Pass "" to revoke all sessions.
func (s *SessionService) RevokeAllUserSessionsExcept(ctx context.Context, userID, exceptSessionID string) error {
	return RevokeAllUserSessionsExceptInDB(ctx, s.db.DB, userID, exceptSessionID)
}

// RevokeAllUserSessionsExceptInDB revokes sessions using the supplied transaction or database handle.
func RevokeAllUserSessionsExceptInDB(ctx context.Context, db *gorm.DB, userID, exceptSessionID string) error {
	if strings.TrimSpace(userID) == "" {
		return common.ErrInvalidToken
	}

	now := time.Now()
	query := db.WithContext(ctx).Model(&UserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID)
	if strings.TrimSpace(exceptSessionID) != "" {
		query = query.Where("id <> ?", exceptSessionID)
	}
	if err := query.Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error; err != nil {
		return fmt.Errorf("failed to revoke user sessions: %w", err)
	}
	return nil
}

// ValidateActive verifies that a persisted user session can still authorize requests.
func ValidateActive(userSession *UserSession) error {
	if userSession == nil {
		return common.ErrInvalidToken
	}
	if userSession.RevokedAt != nil {
		return common.Classify(common.ErrSessionRevoked, errors.New("Session has been revoked")) //nolint:staticcheck // Preserve the existing error message.
	}
	return kit.Ternary[error](time.Now().After(userSession.ExpiresAt), common.ErrExpiredToken, nil)
}

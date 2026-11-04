package enrollment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"gorm.io/gorm"

	"kit.getarcane.app"
	"kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/types/v3"
)

type (
	Service struct {
		db *database.DB
	}

	CreateParams struct {
		EnvironmentID *string
		MaxUses       *int
		Name          string
	}
)

const (
	secretLength = 32
	usableClause = "(max_uses IS NULL OR use_count < max_uses)"
)

var (
	ErrTokenNotFound = common.Classify(common.ErrNotFound, errors.New("enrollment token not found"))
	ErrInvalidToken  = common.Classify(common.ErrUnauthorized, errors.New("invalid enrollment token"))
)

func New(db *database.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(ctx context.Context, p CreateParams) (types.EnrollmentTokenCreated, error) {
	random := make([]byte, secretLength)
	if _, err := rand.Read(random); err != nil {
		return types.EnrollmentTokenCreated{}, fmt.Errorf("failed to generate enrollment token: %w", err)
	}
	secret := types.EnrollmentTokenPrefix + hex.EncodeToString(random)
	encrypted, err := crypto.Encrypt(secret)
	if err != nil {
		return types.EnrollmentTokenCreated{}, fmt.Errorf("failed to encrypt enrollment token: %w", err)
	}
	token := Token{
		BaseModel: database.BaseModel{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		},
		EnvironmentID:  p.EnvironmentID,
		MaxUses:        p.MaxUses,
		Name:           strings.TrimSpace(p.Name),
		TokenHash:      kit.SHA256Hex(secret),
		TokenEncrypted: encrypted,
	}

	replace := func(tx *gorm.DB) error {
		if p.EnvironmentID != nil {
			if deleteErr := tx.Where("environment_id = ?", *p.EnvironmentID).Delete(&Token{}).Error; deleteErr != nil {
				return deleteErr
			}
		}
		return tx.Create(&token).Error
	}
	err = s.db.WithContext(ctx).Transaction(replace)
	if database.IsDuplicateKeyError(s.db.DB, err) {
		err = s.db.WithContext(ctx).Transaction(replace)
	}
	if err != nil {
		return types.EnrollmentTokenCreated{}, fmt.Errorf("failed to create enrollment token: %w", err)
	}
	return types.EnrollmentTokenCreated{
		CreatedAt: token.CreatedAt,
		MaxUses:   token.MaxUses,
		ID:        token.ID,
		Name:      token.Name,
		Token:     secret,
	}, nil
}

func (s *Service) List(ctx context.Context) ([]types.EnrollmentToken, error) {
	var tokens []types.EnrollmentToken
	if err := s.db.WithContext(ctx).
		Model(&Token{}).
		Select("enrollment_tokens.id, enrollment_tokens.name, enrollment_tokens.environment_id, environments.name AS environment_name, " +
			"enrollment_tokens.max_uses, enrollment_tokens.use_count, enrollment_tokens.created_at, enrollment_tokens.last_used_at").
		Joins("LEFT JOIN environments ON environments.id = enrollment_tokens.environment_id").
		Order("enrollment_tokens.created_at ASC, enrollment_tokens.id ASC").
		Scan(&tokens).Error; err != nil {
		return nil, fmt.Errorf("failed to list enrollment tokens: %w", err)
	}
	return tokens, nil
}

func (s *Service) Delete(ctx context.Context, id string) (types.EnrollmentToken, error) {
	var token Token
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if takeErr := tx.Where("id = ?", id).Take(&token).Error; takeErr != nil {
			return takeErr
		}
		return tx.Delete(&token).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.EnrollmentToken{}, ErrTokenNotFound
	}
	if err != nil {
		return types.EnrollmentToken{}, fmt.Errorf("failed to delete enrollment token: %w", err)
	}
	return types.EnrollmentToken{
		CreatedAt:     token.CreatedAt,
		LastUsedAt:    token.LastUsedAt,
		EnvironmentID: token.EnvironmentID,
		MaxUses:       token.MaxUses,
		ID:            token.ID,
		Name:          token.Name,
		UseCount:      token.UseCount,
	}, nil
}

func (s *Service) Reveal(ctx context.Context, id string) (types.EnrollmentToken, string, error) {
	var token Token
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.EnrollmentToken{}, "", ErrTokenNotFound
	}
	if err != nil {
		return types.EnrollmentToken{}, "", fmt.Errorf("failed to load enrollment token: %w", err)
	}
	secret, err := crypto.Decrypt(token.TokenEncrypted)
	if err != nil {
		return types.EnrollmentToken{}, "", fmt.Errorf("failed to decrypt enrollment token: %w", err)
	}
	return types.EnrollmentToken{
		CreatedAt:     token.CreatedAt,
		LastUsedAt:    token.LastUsedAt,
		EnvironmentID: token.EnvironmentID,
		MaxUses:       token.MaxUses,
		ID:            token.ID,
		Name:          token.Name,
		UseCount:      token.UseCount,
	}, secret, nil
}

func (s *Service) Resolve(ctx context.Context, secret string) (types.EnrollmentToken, error) {
	var token Token
	err := s.db.WithContext(ctx).
		Where("token_hash = ?", kit.SHA256Hex(strings.TrimSpace(secret))).
		Take(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.EnrollmentToken{}, ErrInvalidToken
	}
	if err != nil {
		return types.EnrollmentToken{}, fmt.Errorf("failed to resolve enrollment token: %w", err)
	}
	return types.EnrollmentToken{
		CreatedAt:     token.CreatedAt,
		LastUsedAt:    token.LastUsedAt,
		EnvironmentID: token.EnvironmentID,
		MaxUses:       token.MaxUses,
		ID:            token.ID,
		Name:          token.Name,
		UseCount:      token.UseCount,
	}, nil
}

func (s *Service) Consume(ctx context.Context, id string) error {
	now := time.Now()
	result := s.db.WithContext(ctx).
		Model(&Token{}).
		Where("id = ? AND "+usableClause, id).
		Updates(map[string]any{
			"use_count":    gorm.Expr("use_count + 1"),
			"last_used_at": now,
			"updated_at":   now,
		})
	if result.Error != nil {
		return fmt.Errorf("failed to consume enrollment token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrInvalidToken
	}
	return nil
}

func (s *Service) Refund(ctx context.Context, id string) error {
	if err := s.db.WithContext(ctx).
		Model(&Token{}).
		Where("id = ? AND use_count > 0", id).
		Update("use_count", gorm.Expr("use_count - 1")).Error; err != nil {
		return fmt.Errorf("failed to refund enrollment token: %w", err)
	}
	return nil
}

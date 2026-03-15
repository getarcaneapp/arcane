package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
)

const ecrTokenTTL = 12 * time.Hour

// GetOrRefreshECRToken returns a valid ECR auth token (username + password) for the given
// registry. If the cached token (stored encrypted in the DB) is still within its 12-hour
// validity window it is returned directly; otherwise a new token is obtained from the AWS
// ECR API, persisted back to the DB, and returned.
func (s *ContainerRegistryService) GetOrRefreshECRToken(ctx context.Context, reg *models.ContainerRegistry) (username, password string, err error) {
	// 1. Return cached token if still valid.
	if reg.ECRTokenGeneratedAt != nil && time.Since(reg.ECRTokenGeneratedAt.UTC()) < ecrTokenTTL {
		if reg.ECRToken != "" {
			decrypted, decErr := crypto.Decrypt(reg.ECRToken)
			if decErr == nil && strings.TrimSpace(decrypted) != "" {
				return "AWS", decrypted, nil
			}
		}
	}

	// 2. Decrypt the stored AWS secret access key.
	secretKey, decErr := crypto.Decrypt(reg.AWSSecretAccessKey)
	if decErr != nil {
		return "", "", fmt.Errorf("failed to decrypt AWS secret key for registry %s: %w", reg.URL, decErr)
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return "", "", fmt.Errorf("AWS secret access key is empty for registry %s", reg.URL)
	}

	// 3. Call AWS ECR GetAuthorizationToken.
	cfg, cfgErr := config.LoadDefaultConfig(ctx,
		config.WithRegion(reg.AWSRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			reg.AWSAccessKeyID,
			secretKey,
			"",
		)),
	)
	if cfgErr != nil {
		return "", "", fmt.Errorf("failed to load AWS config for registry %s: %w", reg.URL, cfgErr)
	}

	ecrClient := ecr.NewFromConfig(cfg)
	result, ecrErr := ecrClient.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if ecrErr != nil {
		return "", "", fmt.Errorf("failed to get ECR authorization token for registry %s: %w", reg.URL, ecrErr)
	}
	if len(result.AuthorizationData) == 0 || result.AuthorizationData[0].AuthorizationToken == nil {
		return "", "", fmt.Errorf("ECR returned empty authorization data for registry %s", reg.URL)
	}

	// 4. Decode base64 token → "AWS:<password>".
	decoded, decodeErr := base64.StdEncoding.DecodeString(*result.AuthorizationData[0].AuthorizationToken)
	if decodeErr != nil {
		return "", "", fmt.Errorf("failed to decode ECR token for registry %s: %w", reg.URL, decodeErr)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", fmt.Errorf("unexpected ECR token format for registry %s", reg.URL)
	}
	ecrPassword := parts[1]

	// 5. Persist the new token (encrypted) and generation timestamp.
	encryptedToken, encErr := crypto.Encrypt(ecrPassword)
	if encErr != nil {
		return "", "", fmt.Errorf("failed to encrypt ECR token for registry %s: %w", reg.URL, encErr)
	}
	now := time.Now().UTC()
	reg.ECRToken = encryptedToken
	reg.ECRTokenGeneratedAt = &now
	if saveErr := s.db.WithContext(ctx).Save(reg).Error; saveErr != nil {
		// Non-fatal: log but continue — the token is still usable for this call.
		slog.WarnContext(ctx, "failed to persist ECR token to database", "registry", reg.URL, "error", saveErr)
	}

	return "AWS", ecrPassword, nil
}

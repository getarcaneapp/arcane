//go:build playwright

package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/apikey"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PlaywrightService struct {
	apiKeyService              *ApiKeyService
	userService                *UserService
	federatedCredentialService *FederatedCredentialService
}

func NewPlaywrightService(apiKeyService *ApiKeyService, userService *UserService, federatedCredentialService *FederatedCredentialService) *PlaywrightService {
	return &PlaywrightService{
		apiKeyService:              apiKeyService,
		userService:                userService,
		federatedCredentialService: federatedCredentialService,
	}
}

func (ps *PlaywrightService) CreateTestApiKeys(ctx context.Context, count int) ([]*apikey.ApiKeyCreatedDto, error) {
	slog.Info("Playwright: Creating test API keys", "count", count)

	// Get the arcane user to associate the API keys with
	user, err := ps.userService.GetUserByUsername(ctx, "arcane")
	if err != nil {
		return nil, fmt.Errorf("failed to get arcane user: %w", err)
	}

	// Grant every recognized permission globally so the test key behaves like
	// the legacy "admin-everywhere" credential the e2e suite expects. The
	// owner is the `arcane` bootstrap user, who holds global Admin and
	// therefore satisfies validateGrantsAgainstUserInternal.
	allPerms := authz.AllPermissions()
	grants := make([]apikey.PermissionGrant, len(allPerms))
	for i, p := range allPerms {
		grants[i] = apikey.PermissionGrant{Permission: p}
	}

	var createdKeys []*apikey.ApiKeyCreatedDto
	for i := 0; i < count; i++ {
		description := fmt.Sprintf("Test API key %d for Playwright tests", i+1)
		req := apikey.CreateApiKey{
			Name:        fmt.Sprintf("test-api-key-%d", i+1),
			Description: &description,
			Permissions: grants,
		}

		apiKey, err := ps.apiKeyService.CreateApiKey(ctx, user.ID, req)
		if err != nil {
			return nil, fmt.Errorf("failed to create test API key %d: %w", i+1, err)
		}

		createdKeys = append(createdKeys, apiKey)
	}

	slog.Info("Playwright: Test API keys created successfully", "count", len(createdKeys))
	return createdKeys, nil
}

func (ps *PlaywrightService) DeleteAllTestApiKeys(ctx context.Context) error {
	slog.Info("Playwright: Deleting all test API keys")

	// Get all API keys with test prefix
	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: "test-api-key",
		},
		Params: pagination.Params{
			Start: 0,
			Limit: 1000,
		},
	}

	apiKeys, _, err := ps.apiKeyService.ListApiKeys(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

	for _, apiKey := range apiKeys {
		if err := ps.apiKeyService.DeleteApiKey(ctx, apiKey.ID); err != nil {
			slog.Warn("Failed to delete test API key", "id", apiKey.ID, "error", err)
		}
	}

	slog.Info("Playwright: Test API keys deleted", "count", len(apiKeys))
	return nil
}

func (ps *PlaywrightService) CreateTestFederatedCredential(ctx context.Context, issuerURL string, audiences []string, subject string, roleID string, tokenTTLSeconds int) (string, error) {
	if ps.federatedCredentialService == nil || ps.federatedCredentialService.db == nil {
		return "", fmt.Errorf("federated credential service is not available")
	}
	if strings.TrimSpace(issuerURL) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(roleID) == "" || len(audiences) == 0 {
		return "", fmt.Errorf("issuerUrl, subject, roleId, and audiences are required")
	}
	if tokenTTLSeconds <= 0 {
		tokenTTLSeconds = 600
	}

	var credentialID string
	err := ps.federatedCredentialService.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		serviceUser := models.User{
			Username:         "svc_federated_e2e_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			IsServiceAccount: true,
		}
		if err := tx.Create(&serviceUser).Error; err != nil {
			return fmt.Errorf("failed to create federated e2e user: %w", err)
		}

		credential := models.FederatedCredential{
			Name:            "Playwright Federated Credential",
			Enabled:         true,
			IssuerURL:       strings.TrimRight(strings.TrimSpace(issuerURL), "/"),
			Audiences:       models.StringSlice(audiences),
			SubjectClaim:    "sub",
			SubjectMatch:    strings.TrimSpace(subject),
			MatchType:       models.FederatedCredentialMatchExact,
			RoleID:          strings.TrimSpace(roleID),
			IdentityUserID:  serviceUser.ID,
			TokenTTLSeconds: tokenTTLSeconds,
		}
		if err := tx.Create(&credential).Error; err != nil {
			return fmt.Errorf("failed to create federated e2e credential: %w", err)
		}

		assignment := models.UserRoleAssignment{
			UserID: serviceUser.ID,
			RoleID: credential.RoleID,
			Source: models.RoleAssignmentSourceManual,
		}
		if err := tx.Create(&assignment).Error; err != nil {
			return fmt.Errorf("failed to create federated e2e role assignment: %w", err)
		}

		credentialID = credential.ID
		return nil
	})
	if err != nil {
		return "", err
	}
	return credentialID, nil
}

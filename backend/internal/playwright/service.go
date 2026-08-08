//go:build playwright

package playwright

import (
	"context"
	"fmt"
	"log/slog"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	apikeytypes "github.com/getarcaneapp/arcane/types/v2/apikey"
)

type PlaywrightService struct {
	apiKeyService *apikey.ApiKeyService
	userService   *user.UserService
}

func NewPlaywrightService(apiKeyService *apikey.ApiKeyService, userService *user.UserService) *PlaywrightService {
	return &PlaywrightService{
		apiKeyService: apiKeyService,
		userService:   userService,
	}
}

func (ps *PlaywrightService) CreateTestApiKeys(ctx context.Context, count int) ([]*apikeytypes.ApiKeyCreatedDto, error) {
	slog.Info("Playwright: Creating test API keys", "count", count)

	// Get the arcane user to associate the API keys with
	user, err := ps.userService.GetUserByUsername(ctx, "arcane")
	if err != nil {
		return nil, errors.WrapIf(err, "failed to get arcane user")
	}

	// Grant every recognized permission globally so the test key behaves like
	// the legacy "admin-everywhere" credential the e2e suite expects. There is
	// no request context here, so grant validation runs against a sudo set.
	allPerms := authz.AllPermissions()
	grants := make([]apikeytypes.PermissionGrant, len(allPerms))
	for i, p := range allPerms {
		grants[i] = apikeytypes.PermissionGrant{Permission: p}
	}

	var createdKeys []*apikeytypes.ApiKeyCreatedDto
	for i := range count {
		description := fmt.Sprintf("Test API key %d for Playwright tests", i+1)
		req := apikeytypes.CreateApiKey{
			Name:        fmt.Sprintf("test-api-key-%d", i+1),
			Description: &description,
			Permissions: grants,
		}

		apiKey, err := ps.apiKeyService.CreateApiKey(ctx, user.ID, authz.SudoPermissionSet(), req)
		if err != nil {
			return nil, errors.WrapIff(err, "failed to create test API key %d", i+1)
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
		return errors.WrapIf(err, "failed to list API keys")
	}

	for _, apiKey := range apiKeys {
		if err := ps.apiKeyService.DeleteApiKey(ctx, apiKey.ID); err != nil {
			slog.Warn("Failed to delete test API key", "id", apiKey.ID, "error", err)
		}
	}

	slog.Info("Playwright: Test API keys deleted", "count", len(apiKeys))
	return nil
}

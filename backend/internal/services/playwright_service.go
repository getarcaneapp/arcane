//go:build playwright

package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	"github.com/getarcaneapp/arcane/types/apikey"
)

type PlaywrightService struct {
	apiKeyService *ApiKeyService
	userService   *UserService
}

func NewPlaywrightService(apiKeyService *ApiKeyService, userService *UserService) *PlaywrightService {
	return &PlaywrightService{
		apiKeyService: apiKeyService,
		userService:   userService,
	}
}

func (ps *PlaywrightService) CreateTestApiKeys(ctx context.Context, count int) ([]*apikey.ApiKeyCreatedDto, error) {
	slog.Info("Playwright: Creating test API keys", "count", count)

	// Get the arcane user to associate the API keys with
	user, err := ps.userService.GetUserByUsername(ctx, "arcane")
	if err != nil {
		return nil, fmt.Errorf("failed to get arcane user: %w", err)
	}

	var createdKeys []*apikey.ApiKeyCreatedDto
	for i := 0; i < count; i++ {
		req := apikey.CreateApiKey{
			Name:        fmt.Sprintf("test-api-key-%d", i+1),
			Description: stringPtr(fmt.Sprintf("Test API key %d for Playwright tests", i+1)),
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
		PaginationParams: pagination.PaginationParams{
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

func stringPtr(s string) *string {
	return &s
}

//go:build playwright

package playwright

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
)

type PlaywrightService struct {
	apiKeyService     *apikey.ApiKeyService
	userService       *user.UserService
	repositoryService *gitrepo.GitRepositoryService
	syncService       *gitops.GitOpsSyncService
	projectService    *project.ProjectService
}

func NewPlaywrightService(
	apiKeyService *apikey.ApiKeyService,
	userService *user.UserService,
	repositoryService *gitrepo.GitRepositoryService,
	syncService *gitops.GitOpsSyncService,
	projectService *project.ProjectService,
) *PlaywrightService {
	return &PlaywrightService{
		apiKeyService:     apiKeyService,
		userService:       userService,
		repositoryService: repositoryService,
		syncService:       syncService,
		projectService:    projectService,
	}
}

func (ps *PlaywrightService) CreateTestApiKeys(ctx context.Context, count int) ([]*types.ApiKeyCreatedDto, error) {
	slog.InfoContext(ctx, "Playwright: Creating test API keys", "count", count)

	// Get the arcane user to associate the API keys with
	localUser, err := ps.userService.GetUserByUsername(ctx, "arcane")
	if err != nil {
		return nil, fmt.Errorf("failed to get arcane user: %w", err)
	}

	// Grant every recognized permission globally so the test key behaves like
	// the legacy "admin-everywhere" credential the e2e suite expects. There is
	// no request context here, so grant validation runs against a sudo set.
	allPerms := authz.AllPermissions()
	grants := make([]types.ApiKeyPermissionGrant, len(allPerms))
	for i, p := range allPerms {
		grants[i] = types.ApiKeyPermissionGrant{Permission: p}
	}

	var createdKeys []*types.ApiKeyCreatedDto
	for i := range count {
		description := fmt.Sprintf("Test API key %d for Playwright tests", i+1)
		req := types.ApiKey{
			Name:        fmt.Sprintf("test-api-key-%d", i+1),
			Description: &description,
			Permissions: grants,
		}

		apiKey, createApiKeyErr := ps.apiKeyService.CreateApiKey(ctx, localUser.ID, authz.SudoPermissionSet(), req)
		if createApiKeyErr != nil {
			return nil, fmt.Errorf("failed to create test API key %d: %w", i+1, createApiKeyErr)
		}

		createdKeys = append(createdKeys, apiKey)
	}

	slog.InfoContext(ctx, "Playwright: Test API keys created successfully", "count", len(createdKeys))
	return createdKeys, nil
}

func (ps *PlaywrightService) DeleteAllTestApiKeys(ctx context.Context) error {
	slog.InfoContext(ctx, "Playwright: Deleting all test API keys")

	// Get all API keys with test prefix
	params := pagination.QueryParams{
		Search: "test-api-key",
		Start:  0,
		Limit:  1000,
	}

	apiKeys, _, err := ps.apiKeyService.ListApiKeys(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to list API keys: %w", err)
	}

	for _, apiKey := range apiKeys {
		if deleteApiKeyErr := ps.apiKeyService.DeleteApiKey(ctx, apiKey.ID); deleteApiKeyErr != nil {
			slog.WarnContext(ctx, "Failed to delete test API key", "id", apiKey.ID, "error", deleteApiKeyErr)
		}
	}

	slog.InfoContext(ctx, "Playwright: Test API keys deleted", "count", len(apiKeys))
	return nil
}

const (
	testRepositoryNameInternal = "gitsyncs-test-repo"
	testRepositoryURLInternal  = "https://github.com/getarcaneapp/gitsyncs.git"
	testSyncNameInternal       = "gitops-test-sync"
	testProjectNameInternal    = "gitops-test-project"
	testBranchInternal         = "main"
	testComposePathInternal    = "compose-test-repo/compose.yaml"
)

// CreateTestGitOpsProject seeds the suite prerequisite through the real GitOps services.
func (ps *PlaywrightService) CreateTestGitOpsProject(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	localUser, err := ps.userService.GetUserByUsername(ctx, "arcane")
	if err != nil {
		return fmt.Errorf("failed to get arcane user: %w", err)
	}
	actor := *localUser.Actor()
	repositoryID, err := ps.ensureTestRepository(ctx, actor)
	if err != nil {
		return err
	}
	sync, created, err := ps.ensureTestSync(ctx, repositoryID, actor)
	if err != nil {
		return err
	}
	// CreateSync already ran the initial sync; retry only if it failed.
	if !created || sync.LastSyncStatus == nil || *sync.LastSyncStatus != "success" || sync.LastSyncError != nil {
		result, performSyncErr := ps.syncService.PerformSync(ctx, "0", sync.ID, actor)
		if performSyncErr != nil {
			return fmt.Errorf("failed to sync GitOps test project: %w", performSyncErr)
		}
		if !result.Success {
			return fmt.Errorf("GitOps test sync failed: %s", result.Message)
		}
		sync, performSyncErr = ps.syncService.GetSyncByID(ctx, "0", sync.ID)
		if performSyncErr != nil {
			return performSyncErr
		}
	}
	if sync.ProjectID == nil || sync.LastSyncCommit == nil || *sync.LastSyncCommit == "" || sync.LastSyncStatus == nil || *sync.LastSyncStatus != "success" || sync.LastSyncError != nil {
		return errors.New("GitOps test sync did not bind a managed project and commit")
	}
	details, err := ps.projectService.GetProjectDetails(ctx, *sync.ProjectID, types.ProjectDetailsOptions{})
	if err != nil {
		return fmt.Errorf("failed to verify GitOps test project: %w", err)
	}
	if details.Name != testProjectNameInternal ||
		details.GitOpsManagedBy == nil ||
		*details.GitOpsManagedBy != sync.ID ||
		details.LastSyncCommit == nil ||
		*details.LastSyncCommit != *sync.LastSyncCommit {
		return errors.New("GitOps test project does not match its managed sync")
	}
	return nil
}

func (ps *PlaywrightService) ensureTestRepository(ctx context.Context, actor types.Actor) (string, error) {
	params := pagination.QueryParams{Search: testRepositoryNameInternal, Limit: 100}
	repositories, _, err := ps.repositoryService.GetRepositoriesPaginated(ctx, params)
	if err != nil {
		return "", err
	}
	repositoryID := ""
	for _, repository := range repositories {
		if repository.Name != testRepositoryNameInternal {
			continue
		}
		updated, updateRepositoryErr := ps.repositoryService.UpdateRepository(ctx, repository.ID, types.GitRepository{
			Name:     testRepositoryNameInternal,
			URL:      testRepositoryURLInternal,
			AuthType: "none",
			Enabled:  true,
		}, map[string]bool{"name": true, "url": true, "authType": true, "enabled": true}, actor)
		if updateRepositoryErr != nil {
			return "", updateRepositoryErr
		}
		repositoryID = updated.ID
		break
	}
	if repositoryID == "" {
		created, createRepositoryErr := ps.repositoryService.CreateRepository(ctx, types.GitRepository{
			Name:     testRepositoryNameInternal,
			URL:      testRepositoryURLInternal,
			AuthType: "none",
		}, nil, actor)
		if createRepositoryErr != nil {
			return "", createRepositoryErr
		}
		repositoryID = created.ID
	}
	if testConnectionErr := ps.repositoryService.TestConnection(ctx, repositoryID, testBranchInternal, actor); testConnectionErr != nil {
		return "", fmt.Errorf("failed to connect to GitOps test repository: %w", testConnectionErr)
	}
	return repositoryID, nil
}

func (ps *PlaywrightService) ensureTestSync(ctx context.Context, repositoryID string, actor types.Actor) (*project.GitOpsSync, bool, error) {
	params := pagination.QueryParams{Search: testSyncNameInternal, Limit: 100}
	syncs, _, _, err := ps.syncService.GetSyncsPaginated(ctx, "0", params)
	if err != nil {
		return nil, false, err
	}
	for _, sync := range syncs {
		if sync.Name != testSyncNameInternal {
			continue
		}
		updated, updateSyncErr := ps.syncService.UpdateSync(ctx, "0", sync.ID, types.GitOpsSync{
			Name:         testSyncNameInternal,
			RepositoryID: repositoryID,
			Branch:       testBranchInternal,
			ComposePath:  testComposePathInternal,
			TargetType:   "project",
			ProjectName:  testProjectNameInternal,
		}, map[string]bool{
			"name":               true,
			"repositoryId":       true,
			"branch":             true,
			"composePath":        true,
			"targetType":         true,
			"projectName":        true,
			"autoSync":           true,
			"syncDirectory":      true,
			"pullImageAfterSync": true,
			"redeployAfterSync":  true,
		}, actor)
		if updateSyncErr != nil {
			return nil, false, updateSyncErr
		}
		return updated, false, nil
	}
	created, err := ps.syncService.CreateSync(ctx, "0", types.GitOpsSync{
		Name:         testSyncNameInternal,
		RepositoryID: repositoryID,
		Branch:       testBranchInternal,
		ComposePath:  testComposePathInternal,
		TargetType:   "project",
		ProjectName:  testProjectNameInternal,
	}, map[string]bool{"autoSync": true, "syncDirectory": true, "pullImageAfterSync": true, "redeployAfterSync": true}, actor)
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

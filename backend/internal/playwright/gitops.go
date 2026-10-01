//go:build playwright

package playwright

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	gitopstypes "github.com/getarcaneapp/arcane/types/v2/gitops"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
)

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
	actor, err := ps.userService.GetUserByUsername(ctx, "arcane")
	if err != nil {
		return fmt.Errorf("failed to get arcane user: %w", err)
	}
	repositoryID, err := ps.ensureTestRepositoryInternal(ctx, *actor)
	if err != nil {
		return err
	}
	sync, created, err := ps.ensureTestSyncInternal(ctx, repositoryID, *actor)
	if err != nil {
		return err
	}
	// CreateSync already ran the initial sync; retry only if it failed.
	if !created || sync.LastSyncStatus == nil || *sync.LastSyncStatus != "success" || sync.LastSyncError != nil {
		result, err := ps.syncService.PerformSync(ctx, "0", sync.ID, *actor)
		if err != nil {
			return fmt.Errorf("failed to sync GitOps test project: %w", err)
		}
		if !result.Success {
			return fmt.Errorf("GitOps test sync failed: %s", result.Message)
		}
		sync, err = ps.syncService.GetSyncByID(ctx, "0", sync.ID)
		if err != nil {
			return err
		}
	}
	if sync.ProjectID == nil || sync.LastSyncCommit == nil || *sync.LastSyncCommit == "" || sync.LastSyncStatus == nil || *sync.LastSyncStatus != "success" || sync.LastSyncError != nil {
		return errors.New("GitOps test sync did not bind a managed project and commit")
	}
	details, err := ps.projectService.GetProjectDetails(ctx, *sync.ProjectID, projecttypes.DetailsOptions{})
	if err != nil {
		return fmt.Errorf("failed to verify GitOps test project: %w", err)
	}
	if details.Name != testProjectNameInternal || details.GitOpsManagedBy == nil || *details.GitOpsManagedBy != sync.ID || details.LastSyncCommit == nil || *details.LastSyncCommit != *sync.LastSyncCommit {
		return errors.New("GitOps test project does not match its managed sync")
	}
	return nil
}

func (ps *PlaywrightService) ensureTestRepositoryInternal(ctx context.Context, actor common.User) (string, error) {
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
		updated, err := ps.repositoryService.UpdateRepository(ctx, repository.ID, gitopstypes.UpdateRepositoryRequest{
			Name: new(testRepositoryNameInternal), URL: new(testRepositoryURLInternal), AuthType: new("none"), Enabled: new(true),
		}, actor)
		if err != nil {
			return "", err
		}
		repositoryID = updated.ID
		break
	}
	if repositoryID == "" {
		created, err := ps.repositoryService.CreateRepository(ctx, gitopstypes.CreateRepositoryRequest{
			Name: testRepositoryNameInternal, URL: testRepositoryURLInternal, AuthType: "none", Enabled: new(true),
		}, actor)
		if err != nil {
			return "", err
		}
		repositoryID = created.ID
	}
	if err := ps.repositoryService.TestConnection(ctx, repositoryID, testBranchInternal, actor); err != nil {
		return "", fmt.Errorf("failed to connect to GitOps test repository: %w", err)
	}
	return repositoryID, nil
}

func (ps *PlaywrightService) ensureTestSyncInternal(ctx context.Context, repositoryID string, actor common.User) (*project.GitOpsSync, bool, error) {
	params := pagination.QueryParams{Search: testSyncNameInternal, Limit: 100}
	syncs, _, _, err := ps.syncService.GetSyncsPaginated(ctx, "0", params)
	if err != nil {
		return nil, false, err
	}
	for _, sync := range syncs {
		if sync.Name != testSyncNameInternal {
			continue
		}
		updated, err := ps.syncService.UpdateSync(ctx, "0", sync.ID, gitopstypes.UpdateSyncRequest{
			Name: new(testSyncNameInternal), RepositoryID: new(repositoryID), Branch: new(testBranchInternal), ComposePath: new(testComposePathInternal),
			TargetType: new("project"), ProjectName: new(testProjectNameInternal), AutoSync: new(false), SyncDirectory: new(false), PullImageAfterSync: new(false), RedeployAfterSync: new(false),
		}, actor)
		if err != nil {
			return nil, false, err
		}
		return updated, false, nil
	}
	created, err := ps.syncService.CreateSync(ctx, "0", gitopstypes.CreateSyncRequest{
		Name: testSyncNameInternal, RepositoryID: repositoryID, Branch: testBranchInternal, ComposePath: testComposePathInternal,
		TargetType: "project", ProjectName: testProjectNameInternal, AutoSync: new(false), SyncDirectory: new(false), PullImageAfterSync: new(false), RedeployAfterSync: new(false),
	}, actor)
	if err != nil {
		return nil, false, err
	}
	return created, true, nil
}

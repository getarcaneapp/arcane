package stacks

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"go.getarcane.app/acfs"
	acfstypes "go.getarcane.app/acfs/types"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	libswarm "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
)

const (
	defaultSwarmStackSourceRootDir = "/app/data/swarm/sources"
	swarmStackComposeFilename      = "compose.yaml"
	swarmStackOverrideFilename     = "compose.override.yaml"
	swarmStackEnvFilename          = ".env"
)

// Service deploys and manages swarm stacks and their persisted sources.
type Service struct {
	docker           *dockerInternal.DockerClientService
	ensureManager    func(ctx context.Context) error
	listTasks        func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error)
	paginateServices func(ctx context.Context, services []swarm.Service, params pagination.QueryParams) ([]types.SwarmServiceSummary, pagination.Response, error)
	registryAuth     func(ctx context.Context, imageRef string) (string, error)
	settingsService  *settings.SettingsService
}

func NewService(
	dockerService *dockerInternal.DockerClientService,
	ensureManager func(ctx context.Context) error,
	listTasks func(ctx context.Context, filters client.Filters, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error),
	paginateServices func(ctx context.Context, services []swarm.Service, params pagination.QueryParams) ([]types.SwarmServiceSummary, pagination.Response, error),
	registryAuth func(ctx context.Context, imageRef string) (string, error),
	settingsService *settings.SettingsService,
) *Service {
	return &Service{
		docker:           dockerService,
		ensureManager:    ensureManager,
		listTasks:        listTasks,
		paginateServices: paginateServices,
		registryAuth:     registryAuth,
		settingsService:  settingsService,
	}
}

func (s *Service) ListStacksPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]types.SwarmStackSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	servicesResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceListResult, error) {
		return dockerClient.ServiceList(ctx, client.ServiceListOptions{})
	})
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to list swarm services: %w", err)
	}
	services := servicesResult.Items

	stacks := make(map[string]*types.SwarmStackSummary)
	for _, service := range services {
		stackName := service.Spec.Labels[types.SwarmStackNamespaceLabel]
		if stackName == "" {
			continue
		}

		entry, exists := stacks[stackName]
		if !exists {
			stacks[stackName] = &types.SwarmStackSummary{
				ID:        stackName,
				Name:      stackName,
				Namespace: stackName,
				Services:  1,
				CreatedAt: service.CreatedAt,
				UpdatedAt: service.UpdatedAt,
			}
			continue
		}

		entry.Services++
		if service.CreatedAt.Before(entry.CreatedAt) {
			entry.CreatedAt = service.CreatedAt
		}
		if service.UpdatedAt.After(entry.UpdatedAt) {
			entry.UpdatedAt = service.UpdatedAt
		}
	}

	persistedStacks, err := s.listPersistedStackSources(ctx, environmentID)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	for stackName, persisted := range persistedStacks {
		if _, exists := stacks[stackName]; exists {
			continue
		}
		stacks[stackName] = new(persisted)
	}

	items := make([]types.SwarmStackSummary, 0, len(stacks))
	for _, stack := range stacks {
		items = append(items, *stack)
	}

	config := pagination.Config[types.SwarmStackSummary]{
		SearchAccessors: []pagination.SearchAccessor[types.SwarmStackSummary]{
			func(stack types.SwarmStackSummary) (string, error) { return stack.Name, nil },
			func(stack types.SwarmStackSummary) (string, error) { return stack.Namespace, nil },
		},
		SortBindings: []pagination.SortBinding[types.SwarmStackSummary]{
			{Key: "name", Fn: func(a, b types.SwarmStackSummary) int { return strings.Compare(a.Name, b.Name) }},
			{Key: "services", Fn: func(a, b types.SwarmStackSummary) int { return cmp.Compare(a.Services, b.Services) }},
			{Key: "created", Fn: func(a, b types.SwarmStackSummary) int { return a.CreatedAt.Compare(b.CreatedAt) }},
			{Key: "updated", Fn: func(a, b types.SwarmStackSummary) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
		},
	}
	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func (s *Service) DeployStack(ctx context.Context, environmentID string, req types.SwarmStackDeployRequest) (*types.SwarmStackDeployResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	stackName := strings.TrimSpace(req.Name)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	dockerClient, err := s.docker.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	_, stackSourceDir, err := s.resolveSwarmStackSourceDir(ctx, environmentID, stackName)
	if err != nil {
		return nil, err
	}

	if upsertStackSourceErr := s.upsertStackSource(ctx, environmentID, stackName, req.ComposeContent, req.OverrideContent, req.EnvContent, req.Files); upsertStackSourceErr != nil {
		slog.WarnContext(ctx, "failed to persist swarm stack source", "environmentId", normalizeSwarmEnvironmentID(environmentID), "stackName", stackName, "error", upsertStackSourceErr)
	}

	workingDir := req.WorkingDir
	if workingDir == "" || len(req.Files) > 0 {
		workingDir = stackSourceDir
	}

	pm := projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "swarmStackSourcesDirectory"),
		defaultSwarmStackSourceRootDir,
		dockerClient,
	)

	if deployStackErr := libswarm.DeployStack(ctx, dockerClient, libswarm.StackDeployOptions{
		Name:             stackName,
		ComposeContent:   req.ComposeContent,
		OverrideContent:  req.OverrideContent,
		EnvContent:       req.EnvContent,
		WithRegistryAuth: req.WithRegistryAuth,
		RegistryAuthForImage: func(ctx context.Context, imageRef string) (string, error) {
			if s.registryAuth == nil {
				return "", nil
			}
			return s.registryAuth(ctx, imageRef)
		},
		Prune:        req.Prune,
		ResolveImage: req.ResolveImage,
		WorkingDir:   workingDir,
		PathMapper:   pm,
	}); deployStackErr != nil {
		return nil, classifySwarmStackError(deployStackErr)
	}

	return &types.SwarmStackDeployResponse{Name: stackName}, nil
}

func (s *Service) GetStack(ctx context.Context, environmentID, stackName string) (*types.SwarmStackInspect, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	services, err := s.listStackServicesRaw(ctx, stackName)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		persisted, persistedErr := s.getPersistedStackSourceSummary(ctx, environmentID, stackName)
		if persistedErr != nil {
			return nil, persistedErr
		}

		return &types.SwarmStackInspect{
			Name:      persisted.Name,
			Namespace: persisted.Namespace,
			Services:  persisted.Services,
			CreatedAt: persisted.CreatedAt,
			UpdatedAt: persisted.UpdatedAt,
		}, nil
	}

	createdAt := services[0].CreatedAt
	updatedAt := services[0].UpdatedAt
	for _, service := range services[1:] {
		if service.CreatedAt.Before(createdAt) {
			createdAt = service.CreatedAt
		}
		if service.UpdatedAt.After(updatedAt) {
			updatedAt = service.UpdatedAt
		}
	}

	return &types.SwarmStackInspect{
		Name:      stackName,
		Namespace: stackName,
		Services:  len(services),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (s *Service) GetStackSource(ctx context.Context, environmentID, stackName string) (*types.SwarmStackSource, error) {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	_, stackSourceDir, err := s.resolveSwarmStackSourceDir(ctx, environmentID, stackName)
	if err != nil {
		return nil, err
	}

	composeContent, err := acfs.ReadFile(ctx, stackSourceDir, "/"+swarmStackComposeFilename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errdefs.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read swarm stack compose source: %w", err)
	}

	overrideContent := ""
	overrideBytes, err := acfs.ReadFile(ctx, stackSourceDir, "/"+swarmStackOverrideFilename)
	if err == nil {
		overrideContent = string(overrideBytes)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read swarm stack override source: %w", err)
	}

	envContent := ""
	envBytes, err := acfs.ReadFile(ctx, stackSourceDir, "/"+swarmStackEnvFilename)
	if err == nil {
		envContent = string(envBytes)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read swarm stack env source: %w", err)
	}

	var files []types.SwarmSyncFile
	err = acfs.Walk(ctx, stackSourceDir, "/", func(entry acfstypes.Entry) error {
		if entry.IsDirectory || entry.IsSymlink {
			return nil
		}
		relativePath := strings.TrimPrefix(entry.Path, "/")
		if relativePath == swarmStackComposeFilename || relativePath == swarmStackOverrideFilename || relativePath == swarmStackEnvFilename {
			return nil
		}
		content, readFileErr := acfs.ReadFile(ctx, stackSourceDir, entry.Path)
		if readFileErr != nil {
			return readFileErr
		}
		files = append(files, types.SwarmSyncFile{
			RelativePath: relativePath,
			Content:      content,
		})
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read additional swarm stack source files: %w", err)
	}

	return &types.SwarmStackSource{
		Name:            stackName,
		ComposeContent:  string(composeContent),
		OverrideContent: overrideContent,
		EnvContent:      envContent,
		Files:           files,
	}, nil
}

var deployStackAfterSourceUpdate = (*Service).DeployStack

func (s *Service) UpdateStackSource(ctx context.Context, environmentID, stackName string, update types.SwarmStackSource, submitted map[string]bool) (*types.SwarmStackSource, error) {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	previous, err := s.GetStackSource(ctx, environmentID, stackName)
	if err != nil && !errors.Is(err, errdefs.ErrNotFound) {
		return nil, err
	}

	current := kit.FromPtr(previous)
	source := types.SwarmStackSource{
		Name:            stackName,
		ComposeContent:  kit.Ternary(submitted["composeContent"], update.ComposeContent, current.ComposeContent),
		OverrideContent: kit.Ternary(submitted["overrideContent"], update.OverrideContent, current.OverrideContent),
		EnvContent:      kit.Ternary(submitted["envContent"], update.EnvContent, current.EnvContent),
		Files:           kit.Ternary(submitted["files"], update.Files, current.Files),
	}
	if strings.TrimSpace(source.ComposeContent) == "" {
		return nil, errors.New("stack compose source is required")
	}

	if upsertStackSourceErr := s.upsertStackSource(ctx, environmentID, stackName, source.ComposeContent, source.OverrideContent, source.EnvContent, source.Files); upsertStackSourceErr != nil {
		return nil, upsertStackSourceErr
	}

	// Saving an edited stack must behave like `docker stack deploy` (#3463):
	// push the updated spec to the running services so the edit takes effect.
	// The saved source is the full stack spec, so services removed from it
	// must also be removed from the swarm.
	// WithRegistryAuth is always set, the same as the Git Sync deploy path:
	// resolution is per image and yields nothing unless a configured container
	// registry matches the image host. Without it, a service added in the edit
	// has no previous spec to fall back on and private images fail to pull.
	if _, deployStackAfterSourceUpdateErr := deployStackAfterSourceUpdate(s, ctx, environmentID, types.SwarmStackDeployRequest{
		Name:             stackName,
		ComposeContent:   source.ComposeContent,
		OverrideContent:  source.OverrideContent,
		EnvContent:       source.EnvContent,
		Files:            source.Files,
		Prune:            true,
		WithRegistryAuth: true,
	}); deployStackAfterSourceUpdateErr != nil {
		// Roll back the persisted source so reads never return an edit that
		// was never successfully deployed.
		var restoreErr error
		if previous != nil {
			restoreErr = s.upsertStackSource(ctx, environmentID, stackName, previous.ComposeContent, previous.OverrideContent, previous.EnvContent, previous.Files)
		} else {
			restoreErr = s.deleteStackSource(ctx, environmentID, stackName)
		}
		if restoreErr != nil {
			slog.WarnContext(ctx, "failed to restore swarm stack source after deploy failure", "environmentId", normalizeSwarmEnvironmentID(environmentID), "stackName", stackName, "error", restoreErr)
		}
		return nil, fmt.Errorf("failed to redeploy swarm stack from updated source: %w", deployStackAfterSourceUpdateErr)
	}

	return &source, nil
}

func (s *Service) listPersistedStackSources(ctx context.Context, environmentID string) (map[string]types.SwarmStackSummary, error) {
	_, environmentDir, err := s.resolveSwarmStackSourceEnvironmentDir(ctx, environmentID)
	if err != nil {
		return nil, err
	}

	entries, err := acfs.List(ctx, environmentDir, "/")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]types.SwarmStackSummary{}, nil
		}
		return nil, fmt.Errorf("failed to list swarm stack source directories: %w", err)
	}

	stacks := make(map[string]types.SwarmStackSummary, len(entries))
	for _, entry := range entries {
		if !entry.IsDirectory {
			continue
		}

		summary, buildPersistedStackSourceSummaryErr := s.buildPersistedStackSourceSummary(ctx, filepath.Join(environmentDir, entry.Name), entry.Name)
		if buildPersistedStackSourceSummaryErr != nil {
			if errdefs.IsNotFound(buildPersistedStackSourceSummaryErr) {
				continue
			}
			return nil, buildPersistedStackSourceSummaryErr
		}

		stacks[summary.Name] = *summary
	}

	return stacks, nil
}

// getPersistedStackSourceSummary maps any not-found failure to errdefs.ErrNotFound.
func (s *Service) getPersistedStackSourceSummary(ctx context.Context, environmentID, stackName string) (*types.SwarmStackSummary, error) {
	_, stackSourceDir, err := s.resolveSwarmStackSourceDir(ctx, environmentID, stackName)
	if err != nil {
		return nil, err
	}

	summary, err := s.buildPersistedStackSourceSummary(ctx, stackSourceDir, stackName)
	if errdefs.IsNotFound(err) {
		return nil, errdefs.ErrNotFound
	}
	return summary, err
}

func (s *Service) buildPersistedStackSourceSummary(ctx context.Context, stackSourceDir, stackName string) (*types.SwarmStackSummary, error) {
	composeEntry, err := acfs.Stat(ctx, stackSourceDir, "/"+swarmStackComposeFilename, true)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errdefs.ErrNotFound
		}
		return nil, fmt.Errorf("failed to stat swarm stack compose source: %w", err)
	}

	createdAt := composeEntry.ModTime
	updatedAt := composeEntry.ModTime

	envEntry, err := acfs.Stat(ctx, stackSourceDir, "/"+swarmStackEnvFilename, true)
	if err == nil {
		if envEntry.ModTime.Before(createdAt) {
			createdAt = envEntry.ModTime
		}
		if envEntry.ModTime.After(updatedAt) {
			updatedAt = envEntry.ModTime
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to stat swarm stack env source: %w", err)
	}

	return &types.SwarmStackSummary{
		ID:        stackName,
		Name:      stackName,
		Namespace: stackName,
		Services:  0,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func (s *Service) RemoveStack(ctx context.Context, environmentID, stackName string) error {
	if err := s.ensureManager(ctx); err != nil {
		return err
	}

	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return errors.New("stack name is required")
	}

	services, err := s.listStackServicesRaw(ctx, stackName)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		if _, persistedErr := s.getPersistedStackSourceSummary(ctx, environmentID, stackName); persistedErr != nil {
			return persistedErr
		}
	} else {
		if removeStackServicesErr := s.removeStackServices(ctx, stackName, services); removeStackServicesErr != nil {
			return removeStackServicesErr
		}
	}

	dockerClient, err := s.docker.GetClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if removeStackResourcesErr := libswarm.RemoveStackResources(ctx, dockerClient, stackName); removeStackResourcesErr != nil {
		return classifySwarmStackError(removeStackResourcesErr)
	}

	if deleteStackSourceErr := s.deleteStackSource(ctx, environmentID, stackName); deleteStackSourceErr != nil {
		slog.WarnContext(ctx, "failed to remove persisted swarm stack source", "environmentId", normalizeSwarmEnvironmentID(environmentID), "stackName", stackName, "error", deleteStackSourceErr)
	}

	return nil
}

func (s *Service) removeStackServices(ctx context.Context, stackName string, services []swarm.Service) error {
	serviceIDs := make(map[string]struct{}, len(services))
	for _, service := range services {
		serviceIDs[service.ID] = struct{}{}
		if _, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceRemoveResult, error) {
			return dockerClient.ServiceRemove(ctx, service.ID, client.ServiceRemoveOptions{})
		}); err != nil && !errdefs.IsNotFound(err) {
			return fmt.Errorf("failed to remove swarm service %s: %w", service.Spec.Name, err)
		}
	}

	if err := s.waitForRemovedServiceTasks(ctx, stackName, serviceIDs, 30*time.Second); err != nil {
		return err
	}

	return nil
}

func (s *Service) ListStackServicesPaginated(ctx context.Context, stackName string, params pagination.QueryParams) ([]types.SwarmServiceSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	services, err := s.listStackServicesRaw(ctx, stackName)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if len(services) == 0 {
		return nil, pagination.Response{}, errdefs.ErrNotFound
	}

	return s.paginateServices(ctx, services, params)
}

func (s *Service) ListStackTasksPaginated(ctx context.Context, stackName string, params pagination.QueryParams) ([]types.SwarmTaskSummary, pagination.Response, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, pagination.Response{}, err
	}

	services, err := s.listStackServicesRaw(ctx, stackName)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	if len(services) == 0 {
		return nil, pagination.Response{}, errdefs.ErrNotFound
	}

	filters := make(client.Filters)
	for _, service := range services {
		filters.Add("service", service.ID)
	}

	return s.listTasks(ctx, filters, params)
}

func (s *Service) RenderStackConfig(ctx context.Context, environmentID string, req types.SwarmStackRenderConfigRequest) (*types.SwarmStackRenderConfigResponse, error) {
	if err := s.ensureManager(ctx); err != nil {
		return nil, err
	}

	_, stackSourceDir, err := s.resolveSwarmStackSourceDir(ctx, environmentID, req.Name)
	if err != nil {
		return nil, err
	}
	dockerClient, err := s.docker.GetClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}
	pm := projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "swarmStackSourcesDirectory"),
		defaultSwarmStackSourceRootDir,
		dockerClient,
	)

	result, err := libswarm.RenderStackConfig(ctx, libswarm.StackRenderOptions{
		Name:            req.Name,
		ComposeContent:  req.ComposeContent,
		OverrideContent: req.OverrideContent,
		EnvContent:      req.EnvContent,
		WorkingDir:      stackSourceDir,
		PathMapper:      pm,
	})
	if err != nil {
		return nil, classifySwarmStackError(err)
	}

	return &types.SwarmStackRenderConfigResponse{
		Name:            result.Name,
		RenderedCompose: result.RenderedCompose,
		Services:        result.Services,
		Networks:        result.Networks,
		Volumes:         result.Volumes,
		Configs:         result.Configs,
		Secrets:         result.Secrets,
	}, nil
}

func (s *Service) listStackServicesRaw(ctx context.Context, stackName string) ([]swarm.Service, error) {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return nil, errors.New("stack name is required")
	}

	stackFilter := make(client.Filters).Add("label", fmt.Sprintf("%s=%s", types.SwarmStackNamespaceLabel, stackName))
	servicesResult, err := s.docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (client.ServiceListResult, error) {
		return dockerClient.ServiceList(ctx, client.ServiceListOptions{
			Filters: stackFilter,
			Status:  true,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list stack services: %w", err)
	}

	return servicesResult.Items, nil
}

func (s *Service) waitForRemovedServiceTasks(ctx context.Context, stackName string, serviceIDs map[string]struct{}, timeout time.Duration) error {
	if len(serviceIDs) == 0 {
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// Docker rejects a service filter naming a removed service, so filter by stack label.
	taskFilters := make(client.Filters).Add("label", fmt.Sprintf("%s=%s", types.SwarmStackNamespaceLabel, stackName))

	for {
		tasksResult, err := s.docker.Call(waitCtx, func(ctx context.Context, dockerClient *client.Client) (client.TaskListResult, error) {
			return dockerClient.TaskList(ctx, client.TaskListOptions{Filters: taskFilters})
		})
		if err != nil {
			return fmt.Errorf("failed to list tasks while waiting for stack removal: %w", err)
		}

		hasActiveTasks := slices.ContainsFunc(tasksResult.Items, func(task swarm.Task) bool {
			if _, removed := serviceIDs[task.ServiceID]; !removed {
				return false
			}
			return !slices.Contains([]swarm.TaskState{
				swarm.TaskStateComplete,
				swarm.TaskStateShutdown,
				swarm.TaskStateFailed,
				swarm.TaskStateRejected,
				swarm.TaskStateRemove,
				swarm.TaskStateOrphaned,
			}, task.Status.State)
		})
		if !hasActiveTasks {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for stack task convergence: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (s *Service) upsertStackSource(ctx context.Context, environmentID, stackName, composeContent, overrideContent, envContent string, files []types.SwarmSyncFile) error {
	stackName = strings.TrimSpace(stackName)
	if stackName == "" {
		return errors.New("stack name is required")
	}

	rootDir, stackSourceDir, err := s.resolveSwarmStackSourceDir(ctx, environmentID, stackName)
	if err != nil {
		return err
	}

	stackLogical, err := acfs.LogicalPath(rootDir, stackSourceDir)
	if err != nil {
		return fmt.Errorf("swarm stack source directory is outside its root: %w", err)
	}
	// os rather than acfs: acfs can only open a root that already exists.
	if mkdirAllErr := os.MkdirAll(rootDir, utils.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("failed to create swarm stack source root: %w", mkdirAllErr)
	}
	if mkdirAllErr := acfs.MkdirAll(ctx, rootDir, stackLogical, utils.DirPerm); mkdirAllErr != nil {
		return fmt.Errorf("failed to create swarm stack source directory: %w", mkdirAllErr)
	}

	syncFiles := make([]projects.SyncFile, 0, len(files)+3)
	syncFiles = append(syncFiles, projects.SyncFile{
		RelativePath: swarmStackComposeFilename,
		Content:      []byte(composeContent),
	})
	if overrideContent != "" {
		syncFiles = append(syncFiles, projects.SyncFile{
			RelativePath: swarmStackOverrideFilename,
			Content:      []byte(overrideContent),
		})
	}
	if envContent != "" {
		syncFiles = append(syncFiles, projects.SyncFile{
			RelativePath: swarmStackEnvFilename,
			Content:      []byte(envContent),
		})
	}

	for _, file := range files {
		relativePath := filepath.ToSlash(filepath.Clean(file.RelativePath))
		if relativePath == "." {
			return errors.New("swarm stack file path is required")
		}
		if relativePath == swarmStackComposeFilename || relativePath == swarmStackOverrideFilename || relativePath == swarmStackEnvFilename {
			continue
		}
		syncFiles = append(syncFiles, projects.SyncFile{RelativePath: relativePath, Content: file.Content})
	}

	existingFiles := make([]string, 0)
	if walkErr := acfs.Walk(ctx, stackSourceDir, "/", func(entry acfstypes.Entry) error {
		if entry.IsDirectory || entry.IsSymlink {
			return nil
		}
		existingFiles = append(existingFiles, strings.TrimPrefix(entry.Path, "/"))
		return nil
	}); walkErr != nil {
		return fmt.Errorf("failed to inspect existing swarm stack files: %w", walkErr)
	}

	writtenFiles, err := projects.WriteSyncedDirectory(ctx, rootDir, stackSourceDir, syncFiles)
	if err != nil {
		return fmt.Errorf("failed to write swarm stack files: %w", err)
	}
	if cleanupRemovedFilesErr := projects.CleanupRemovedFiles(ctx, rootDir, stackSourceDir, existingFiles, writtenFiles); cleanupRemovedFilesErr != nil {
		return fmt.Errorf("failed to remove stale swarm stack files: %w", cleanupRemovedFilesErr)
	}

	return nil
}

func (s *Service) deleteStackSource(ctx context.Context, environmentID, stackName string) error {
	if strings.TrimSpace(stackName) == "" {
		return errors.New("stack name is required")
	}

	rootDir, stackSourceDir, err := s.resolveSwarmStackSourceDir(ctx, environmentID, stackName)
	if err != nil {
		return err
	}

	stackLogical, err := acfs.LogicalPath(rootDir, stackSourceDir)
	if err != nil {
		return fmt.Errorf("swarm stack source directory is outside its root: %w", err)
	}
	if removeAllErr := acfs.RemoveAll(ctx, rootDir, stackLogical); removeAllErr != nil {
		return fmt.Errorf("failed to remove swarm stack source directory: %w", removeAllErr)
	}

	// Best-effort cleanup of now-empty environment directory.
	environmentDir := filepath.Dir(stackSourceDir)
	if environmentDir != rootDir {
		if removeErr := acfs.Remove(ctx, rootDir, path.Dir(stackLogical)); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			slog.DebugContext(ctx, "swarm stack source environment directory cleanup skipped", "dir", environmentDir, "error", removeErr)
		}
	}

	return nil
}

func normalizeSwarmEnvironmentID(environmentID string) string {
	return cmp.Or(strings.TrimSpace(environmentID), "0")
}

func (s *Service) resolveSwarmStackSourceDir(ctx context.Context, environmentID, stackName string) (string, string, error) {
	normalizedStackName := projects.SanitizeProjectName(strings.TrimSpace(stackName))
	if normalizedStackName == "" || strings.Trim(normalizedStackName, "_") == "" {
		return "", "", errors.New("invalid stack name")
	}

	rootDir, environmentDir, err := s.resolveSwarmStackSourceEnvironmentDir(ctx, environmentID)
	if err != nil {
		return "", "", err
	}

	stackSourceDir := filepath.Clean(filepath.Join(environmentDir, normalizedStackName))
	if !projects.IsSafeSubdirectory(rootDir, stackSourceDir) {
		return "", "", errors.New("swarm stack source path escapes storage root")
	}

	return rootDir, stackSourceDir, nil
}

func (s *Service) resolveSwarmStackSourceEnvironmentDir(ctx context.Context, environmentID string) (string, string, error) {
	normalizedEnvironmentID := projects.SanitizeProjectName(normalizeSwarmEnvironmentID(environmentID))
	if normalizedEnvironmentID == "" || strings.Trim(normalizedEnvironmentID, "_") == "" {
		normalizedEnvironmentID = "0"
	}

	configuredRootDir := defaultSwarmStackSourceRootDir
	if s.settingsService != nil {
		configuredRootDir = s.settingsService.GetStringSetting(ctx, "swarmStackSourcesDirectory")
	}
	rootDir := projects.ResolveConfiguredContainerDirectory(configuredRootDir, defaultSwarmStackSourceRootDir)

	environmentDir := filepath.Clean(filepath.Join(rootDir, normalizedEnvironmentID))
	if !projects.IsSafeSubdirectory(rootDir, environmentDir) {
		return "", "", errors.New("swarm stack source environment path escapes storage root")
	}

	return rootDir, environmentDir, nil
}

func classifySwarmStackError(err error) error {
	if errors.Is(err, libswarm.ErrInvalidStack) {
		return common.Classify(common.ErrBadRequest, err)
	}
	return err
}

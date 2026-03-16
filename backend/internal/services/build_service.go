package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	buildgit "github.com/getarcaneapp/arcane/backend/pkg/gitutil"
	libbuild "github.com/getarcaneapp/arcane/backend/pkg/libarcane/libbuild"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	buildtypes "github.com/getarcaneapp/arcane/types/builds"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	dockerregistry "github.com/moby/moby/api/types/registry"
	"gorm.io/gorm"
)

type BuildService struct {
	db              *database.DB
	settings        *SettingsService
	dockerService   *DockerClientService
	registryService *ContainerRegistryService
	gitRepository   *GitRepositoryService
	builder         buildtypes.Builder
	gitCloneFn      func(context.Context, string, string, buildgit.AuthConfig) (string, error)
	gitCleanupFn    func(string) error
}

const buildHistoryOutputLimitBytes = 2 * 1024 * 1024

func NewBuildService(
	db *database.DB,
	settings *SettingsService,
	dockerService *DockerClientService,
	registryService *ContainerRegistryService,
	gitRepository *GitRepositoryService,
) *BuildService {
	svc := &BuildService{
		db:              db,
		settings:        settings,
		dockerService:   dockerService,
		registryService: registryService,
		gitRepository:   gitRepository,
	}
	svc.builder = libbuild.NewBuilder(svc, dockerService, svc)

	return svc
}

func (s *BuildService) GetRegistryAuthForImage(ctx context.Context, imageRef string) (string, error) {
	if s.registryService == nil {
		return "", nil
	}
	return s.registryService.GetRegistryAuthForImage(ctx, imageRef)
}

func (s *BuildService) GetRegistryAuthForHost(ctx context.Context, registryHost string) (string, error) {
	if s.registryService == nil {
		return "", nil
	}
	return s.registryService.GetRegistryAuthForHost(ctx, registryHost)
}

func (s *BuildService) GetAllRegistryAuthConfigs(ctx context.Context) (map[string]dockerregistry.AuthConfig, error) {
	if s.registryService == nil {
		return nil, nil
	}
	return s.registryService.GetAllRegistryAuthConfigs(ctx)
}

func (s *BuildService) BuildSettings() buildtypes.BuildSettings {
	if s.settings == nil {
		return buildtypes.BuildSettings{}
	}
	settings := s.settings.GetSettingsConfig()
	return buildtypes.BuildSettings{
		DepotProjectId:   settings.DepotProjectId.Value,
		DepotToken:       settings.DepotToken.Value,
		BuildProvider:    settings.BuildProvider.Value,
		BuildTimeoutSecs: settings.BuildTimeout.AsInt(),
	}
}

func (s *BuildService) BuildImage(ctx context.Context, environmentID string, req imagetypes.BuildRequest, progressWriter io.Writer, serviceName string, user *models.User) (*imagetypes.BuildResult, error) {
	if s.builder == nil {
		return nil, errors.New("build service not available")
	}

	logCapture := libbuild.NewLogCapture(buildHistoryOutputLimitBytes)
	writer := io.Writer(logCapture)
	if progressWriter != nil {
		writer = io.MultiWriter(progressWriter, logCapture)
	}

	buildRecordID := ""
	if s.db != nil && strings.TrimSpace(environmentID) != "" {
		if record, err := s.createBuildRecord(ctx, environmentID, req, user); err != nil {
			slog.WarnContext(ctx, "failed to create build history record", "error", err)
		} else {
			buildRecordID = record.ID
		}
	}

	startedAt := time.Now()
	cleanupResolvedContext := func() error { return nil }
	var (
		result *imagetypes.BuildResult
		err    error
	)

	if resolvedReq, cleanupFn, resolveErr := s.resolveBuildRequestInternal(ctx, req, writer, serviceName); resolveErr != nil {
		err = resolveErr
	} else {
		cleanupResolvedContext = cleanupFn
		result, err = s.builder.BuildImage(ctx, resolvedReq, writer, serviceName)
	}

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()

	if cleanupErr := cleanupResolvedContext(); cleanupErr != nil {
		slog.WarnContext(ctx, "failed to cleanup temporary git build context", "error", cleanupErr)
	}

	if s.db != nil && buildRecordID != "" {
		output := logCapture.String()
		var outputPtr *string
		if output != "" {
			outputPtr = &output
		}

		provider := req.Provider
		var digest *string
		if result != nil {
			if result.Provider != "" {
				provider = result.Provider
			}
			if result.Digest != "" {
				digest = &result.Digest
			}
		}

		status := models.ImageBuildStatusSuccess
		var errMsg *string
		if err != nil {
			status = models.ImageBuildStatusFailed
			msg := err.Error()
			errMsg = &msg
		}

		if updateErr := s.completeBuildRecord(ctx, buildRecordID, status, outputPtr, logCapture.Truncated(), errMsg, digest, provider, completedAt, &durationMs); updateErr != nil {
			slog.WarnContext(ctx, "failed to update build history record", "error", updateErr)
		}
	}

	return result, err
}

func (s *BuildService) resolveBuildRequestInternal(
	ctx context.Context,
	req imagetypes.BuildRequest,
	progressWriter io.Writer,
	serviceName string,
) (imagetypes.BuildRequest, func() error, error) {
	source, ok, err := libbuild.ParseGitBuildContextSource(req.ContextDir)
	if err != nil {
		return imagetypes.BuildRequest{}, func() error { return nil }, err
	}
	if !ok || source == nil {
		if libbuild.IsPotentialRemoteBuildContextSource(req.ContextDir) {
			return imagetypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("unsupported remote build context source %q: only git repository URLs are supported", req.ContextDir)
		}
		return req, func() error { return nil }, nil
	}

	writeBuildProgressStatusInternal(progressWriter, serviceName, fmt.Sprintf("resolving remote git context %s", source.RepositoryURL))

	authConfig, matchedRepository, err := s.resolveGitBuildAuthInternal(ctx, source.RepositoryURL)
	if err != nil {
		return imagetypes.BuildRequest{}, func() error { return nil }, err
	}
	if matchedRepository {
		writeBuildProgressStatusInternal(progressWriter, serviceName, fmt.Sprintf("using saved git credentials for %s", source.RepositoryURL))
	}

	repoPath, err := s.cloneGitContextInternal(ctx, source.RepositoryURL, source.Ref, authConfig)
	if err != nil {
		return imagetypes.BuildRequest{}, func() error { return nil }, err
	}

	contextDir := repoPath
	if source.Subdir != "" {
		if err := buildgit.ValidatePath(repoPath, filepath.FromSlash(source.Subdir)); err != nil {
			_ = s.cleanupGitContextInternal(repoPath)
			return imagetypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("invalid git build context subdir: %w", err)
		}
		contextDir = filepath.Join(repoPath, filepath.FromSlash(source.Subdir))
	}

	info, err := os.Stat(contextDir)
	if err != nil {
		_ = s.cleanupGitContextInternal(repoPath)
		return imagetypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("failed to stat resolved git build context: %w", err)
	}
	if !info.IsDir() {
		_ = s.cleanupGitContextInternal(repoPath)
		return imagetypes.BuildRequest{}, func() error { return nil }, fmt.Errorf("resolved git build context is not a directory")
	}

	writeBuildProgressStatusInternal(progressWriter, serviceName, fmt.Sprintf("using remote build context %s", source.Raw))

	resolvedReq := req
	resolvedReq.ContextDir = contextDir

	return resolvedReq, func() error { return s.cleanupGitContextInternal(repoPath) }, nil
}

func (s *BuildService) resolveGitBuildAuthInternal(ctx context.Context, rawURL string) (buildgit.AuthConfig, bool, error) {
	if s.gitRepository == nil {
		return buildgit.AuthConfig{}, false, nil
	}

	repository, err := s.gitRepository.FindEnabledRepositoryByURL(ctx, rawURL)
	if err != nil {
		return buildgit.AuthConfig{}, false, fmt.Errorf("failed to resolve git repository credentials: %w", err)
	}
	if repository == nil {
		return buildgit.AuthConfig{}, false, nil
	}

	authConfig, err := s.gitRepository.GetAuthConfig(ctx, repository)
	if err != nil {
		return buildgit.AuthConfig{}, true, fmt.Errorf("failed to load git repository credentials: %w", err)
	}

	return authConfig, true, nil
}

func (s *BuildService) cloneGitContextInternal(ctx context.Context, repositoryURL, ref string, authConfig buildgit.AuthConfig) (string, error) {
	if s.gitCloneFn != nil {
		return s.gitCloneFn(ctx, repositoryURL, ref, authConfig)
	}

	if s.gitRepository != nil && s.gitRepository.gitClient != nil {
		return s.gitRepository.gitClient.Clone(ctx, repositoryURL, ref, authConfig)
	}

	return "", errors.New("git repository service not available")
}

func (s *BuildService) cleanupGitContextInternal(repoPath string) error {
	if repoPath == "" {
		return nil
	}
	if s.gitCleanupFn != nil {
		return s.gitCleanupFn(repoPath)
	}
	if s.gitRepository != nil && s.gitRepository.gitClient != nil {
		return s.gitRepository.gitClient.Cleanup(repoPath)
	}
	return errors.New("git repository service not available")
}

func writeBuildProgressStatusInternal(progressWriter io.Writer, serviceName, status string) {
	if progressWriter == nil || strings.TrimSpace(status) == "" {
		return
	}

	if err := json.NewEncoder(progressWriter).Encode(imagetypes.ProgressEvent{
		Type:    "build",
		Service: serviceName,
		Status:  status,
	}); err != nil {
		slog.Debug("failed to write build progress status", "error", err)
	}
}

func (s *BuildService) ListImageBuildsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]imagetypes.BuildRecord, pagination.Response, error) {
	if s.db == nil {
		return nil, pagination.Response{}, fmt.Errorf("build history not available")
	}

	var builds []models.ImageBuild
	q := s.db.WithContext(ctx).Model(&models.ImageBuild{}).Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"context_dir LIKE ? OR COALESCE(dockerfile, '') LIKE ? OR COALESCE(username, '') LIKE ? OR COALESCE(provider, '') LIKE ? OR COALESCE(error_message, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "status", params.Filters["status"])
	q = pagination.ApplyFilter(q, "provider", params.Filters["provider"])

	if params.Sort == "" {
		params.Sort = "createdAt"
	}

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &builds)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate builds: %w", err)
	}

	records := make([]imagetypes.BuildRecord, 0, len(builds))
	for _, build := range builds {
		records = append(records, buildToRecord(build, false))
	}

	return records, paginationResp, nil
}

func (s *BuildService) GetImageBuildByID(ctx context.Context, environmentID, buildID string) (*imagetypes.BuildRecord, error) {
	if s.db == nil {
		return nil, fmt.Errorf("build history not available")
	}

	var build models.ImageBuild
	if err := s.db.WithContext(ctx).First(&build, "id = ? AND environment_id = ?", buildID, environmentID).Error; err != nil {
		return nil, err
	}

	record := buildToRecord(build, true)
	return &record, nil
}

func (s *BuildService) createBuildRecord(ctx context.Context, environmentID string, req imagetypes.BuildRequest, user *models.User) (*models.ImageBuild, error) {
	buildArgs := mapToJSON(req.BuildArgs)
	labels := mapToJSON(req.Labels)
	ulimits := mapToJSON(req.Ulimits)

	var userID *string
	var username *string
	if user != nil {
		userID = &user.ID
		username = &user.Username
	}

	record := &models.ImageBuild{
		EnvironmentID: environmentID,
		UserID:        userID,
		Username:      username,
		Status:        models.ImageBuildStatusRunning,
		Provider:      req.Provider,
		ContextDir:    req.ContextDir,
		Dockerfile:    req.Dockerfile,
		Target:        req.Target,
		Tags:          models.StringSlice(req.Tags),
		Platforms:     models.StringSlice(req.Platforms),
		BuildArgs:     buildArgs,
		Labels:        labels,
		CacheFrom:     models.StringSlice(req.CacheFrom),
		CacheTo:       models.StringSlice(req.CacheTo),
		NoCache:       req.NoCache,
		Pull:          req.Pull,
		BuildNetwork:  req.Network,
		Isolation:     req.Isolation,
		ShmSize:       req.ShmSize,
		Ulimits:       ulimits,
		Entitlements:  models.StringSlice(req.Entitlements),
		Privileged:    req.Privileged,
		ExtraHosts:    models.StringSlice(req.ExtraHosts),
		Push:          req.Push,
		Load:          req.Load,
		BaseModel: models.BaseModel{
			CreatedAt: time.Now(),
		},
	}

	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return nil, fmt.Errorf("failed to create build record: %w", err)
	}

	return record, nil
}

func (s *BuildService) completeBuildRecord(
	ctx context.Context,
	buildID string,
	status models.ImageBuildStatus,
	output *string,
	outputTruncated bool,
	errMsg *string,
	digest *string,
	provider string,
	completedAt time.Time,
	durationMs *int64,
) error {
	if s.db == nil {
		return nil
	}

	updates := map[string]any{
		"status":           status,
		"completed_at":     completedAt,
		"duration_ms":      durationMs,
		"output":           output,
		"output_truncated": outputTruncated,
		"error_message":    errMsg,
		"digest":           digest,
		"provider":         provider,
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.ImageBuild{}).Where("id = ?", buildID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update build record: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("build record not found")
		}
		return nil
	})
}

func buildToRecord(build models.ImageBuild, includeOutput bool) imagetypes.BuildRecord {
	buildArgs := jsonToStringMap(build.BuildArgs)
	labels := jsonToStringMap(build.Labels)
	ulimits := jsonToStringMap(build.Ulimits)

	var output *string
	if includeOutput {
		output = build.Output
	}

	return imagetypes.BuildRecord{
		ID:              build.ID,
		EnvironmentID:   build.EnvironmentID,
		UserID:          build.UserID,
		Username:        build.Username,
		Status:          string(build.Status),
		Provider:        build.Provider,
		ContextDir:      build.ContextDir,
		Dockerfile:      build.Dockerfile,
		Target:          build.Target,
		Tags:            []string(build.Tags),
		Platforms:       []string(build.Platforms),
		BuildArgs:       buildArgs,
		Labels:          labels,
		CacheFrom:       []string(build.CacheFrom),
		CacheTo:         []string(build.CacheTo),
		NoCache:         build.NoCache,
		Pull:            build.Pull,
		Network:         build.BuildNetwork,
		Isolation:       build.Isolation,
		ShmSize:         build.ShmSize,
		Ulimits:         ulimits,
		Entitlements:    []string(build.Entitlements),
		Privileged:      build.Privileged,
		ExtraHosts:      []string(build.ExtraHosts),
		Push:            build.Push,
		Load:            build.Load,
		Digest:          build.Digest,
		ErrorMessage:    build.ErrorMessage,
		Output:          output,
		OutputTruncated: build.OutputTruncated,
		CompletedAt:     build.CompletedAt,
		DurationMs:      build.DurationMs,
		CreatedAt:       build.CreatedAt,
	}
}

func mapToJSON(input map[string]string) models.JSON {
	if len(input) == 0 {
		return nil
	}

	out := models.JSON{}
	for key, value := range input {
		out[key] = value
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func jsonToStringMap(input models.JSON) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = fmt.Sprint(value)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

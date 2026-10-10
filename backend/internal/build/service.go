package build

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/image"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"go.getarcane.app/builds/api"
	"go.getarcane.app/builds/pkg/contextsource"
	"go.getarcane.app/builds/types"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/capture"
	gitkit "go.getarcane.app/kit/pkg/git"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/build/children/workspace"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/gitutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
)

type BuildService struct {
	db              *database.DB
	settings        *settings.SettingsService
	dockerService   *dockerInternal.DockerClientService
	registryService *registry.ContainerRegistryService
	gitRepository   *gitrepo.GitRepositoryService
	eventService    *event.EventService
	workspace       *workspace.Service
	builder         types.Builder
	gitProbeFn      func(context.Context, string, git.AuthConfig) error
	gitCloneFn      func(context.Context, string, string, git.AuthConfig) (string, error)
	gitCleanupFn    func(string) error
}

const buildHistoryOutputLimitBytes = 2 * 1024 * 1024

func NewBuildService(
	db *database.DB,
	localSettings *settings.SettingsService,
	dockerService *dockerInternal.DockerClientService,
	registryService *registry.ContainerRegistryService,
	gitRepository *gitrepo.GitRepositoryService,
	eventService *event.EventService,
) *BuildService {
	svc := &BuildService{
		db:              db,
		settings:        localSettings,
		dockerService:   dockerService,
		registryService: registryService,
		gitRepository:   gitRepository,
		eventService:    eventService,
	}
	var buildsDirectory func() string
	if localSettings != nil {
		buildsDirectory = func() string { return localSettings.GetSettingsConfig().BuildsDirectory.Value }
	}
	svc.workspace = workspace.NewService(buildsDirectory)
	var registryAuthProvider types.RegistryAuthProvider
	if registryService != nil {
		registryAuthProvider = registryService
	}

	// registry.ContainerRegistryService already implements buildtypes.RegistryAuthProvider,
	// so the builder consumes it directly instead of through forwarding methods on BuildService.
	svc.builder = api.NewService(api.Config{
		SettingsProvider:     svc,
		DockerClientProvider: dockerService,
		RegistryAuthProvider: registryAuthProvider,
	})

	return svc
}

func (s *BuildService) BuildSettings() types.BuildSettings {
	if s.settings == nil {
		return types.BuildSettings{}
	}
	localSettings := s.settings.GetSettingsConfig()
	return types.BuildSettings{
		DepotProjectID:   localSettings.DepotProjectId.Value,
		DepotToken:       localSettings.DepotToken.Value,
		BuildProvider:    localSettings.BuildProvider.Value,
		BuildTimeoutSecs: localSettings.BuildTimeout.AsInt(),
	}
}

func (
	s *BuildService,
) BuildImage(
	ctx context.Context,
	environmentID string,
	req types.BuildRequest,
	progressWriter io.Writer,
	serviceName string,
	user *usertypes.Actor,
) (
	*types.BuildResult,
	error,
) {
	if s.builder == nil {
		return nil, errors.New("build service not available")
	}

	// The builder emits raw docker-CLI text. The log capture stores it verbatim
	// for build history; the progress writer gets it framed as {"log":...} lines.
	logCapture := capture.New(buildHistoryOutputLimitBytes)
	writer := io.Writer(logCapture)
	var logWriter io.WriteCloser
	if progressWriter != nil {
		logWriter = docker.NewLogLineWriter(progressWriter)
		writer = io.MultiWriter(logWriter, logCapture)
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
	defer func() {
		if cleanupErr := cleanupResolvedContext(); cleanupErr != nil {
			slog.WarnContext(ctx, "failed to cleanup temporary git build context", "error", cleanupErr)
		}
	}()
	var (
		result *types.BuildResult
		err    error
	)

	if resolvedReq, cleanupFn, resolveErr := s.resolveBuildRequestInternal(ctx, req, writer, serviceName); resolveErr != nil {
		err = resolveErr
	} else {
		cleanupResolvedContext = cleanupFn
		result, err = s.builder.BuildImage(ctx, resolvedReq, writer, serviceName)
	}

	completedAt := time.Now()
	if logWriter != nil {
		_ = logWriter.Close()
	}

	if s.db != nil && buildRecordID != "" {
		output := logCapture.String()
		var outputPtr *string
		if output != "" {
			outputPtr = &output
		}

		provider := s.effectiveBuildProviderInternal(req.Provider)
		var digest *string
		if result != nil {
			if result.Provider != "" {
				provider = result.Provider
			}
			if result.Digest != "" {
				digest = &result.Digest
			}
		}

		status := ImageBuildStatusSuccess
		var errMsg *string
		if err != nil {
			status = ImageBuildStatusFailed
			errMsg = new(err.Error())
		}

		if updateErr := s.completeBuildRecord(
			ctx,
			buildRecordID,
			status,
			outputPtr,
			logCapture.Truncated(),
			errMsg,
			digest,
			provider,
			completedAt,
			new(
				completedAt.Sub(
					startedAt,
				).Milliseconds(),
			),
		); updateErr != nil {
			slog.WarnContext(ctx, "failed to update build history record", "error", updateErr)
		}
	}

	if err != nil {
		s.logBuildFailureEventInternal(ctx, environmentID, req, serviceName, buildRecordID, err, user)
	}

	return result, err
}

func (s *BuildService) logBuildFailureEventInternal(ctx context.Context, environmentID string, req types.BuildRequest, serviceName, buildRecordID string, err error, user *usertypes.Actor) {
	if s.eventService == nil || err == nil {
		return
	}

	resourceName := cmp.Or(kit.TrimNonEmpty(req.Tags)...)
	if resourceName == "" {
		resourceName = strings.TrimSpace(serviceName)
	}
	if resourceName == "" {
		resourceName = sanitizeBuildContextForEventInternal(req.ContextDir)
	}

	userID := ""
	username := ""
	if user != nil {
		userID = user.ID
		username = user.Username
	}

	metadata := database.JSON{
		"action":     "build",
		"provider":   s.effectiveBuildProviderInternal(req.Provider),
		"contextDir": sanitizeBuildContextForEventInternal(req.ContextDir),
		"dockerfile": req.Dockerfile,
		"tags":       append([]string(nil), req.Tags...),
	}
	if service := strings.TrimSpace(serviceName); service != "" {
		metadata["serviceName"] = service
	}
	if buildRecordID != "" {
		metadata["buildRecordId"] = buildRecordID
	}

	s.eventService.LogErrorEvent(ctx, event.EventTypeImageError, "image", "", resourceName, userID, username, environmentID, err, metadata)
}

func sanitizeBuildContextForEventInternal(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	base, fragment, hasFragment := strings.Cut(trimmed, "#")
	parsed, err := url.Parse(base)
	if err != nil {
		return kit.Ternary(strings.Contains(base, "@"), "[unparseable URL]", trimmed)
	}
	if parsed.User == nil {
		return trimmed
	}

	parsed.User = url.User("redacted")
	sanitized := parsed.String()
	if hasFragment {
		sanitized += "#" + fragment
	}
	return sanitized
}

func (s *BuildService) effectiveBuildProviderInternal(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "" {
		return provider
	}
	if s.settings != nil {
		provider = strings.ToLower(strings.TrimSpace(s.settings.GetSettingsConfig().BuildProvider.Value))
	}
	return kit.Ternary(provider == "", "local", provider)
}

func (s *BuildService) resolveBuildRequestInternal(
	ctx context.Context,
	req types.BuildRequest,
	progressWriter io.Writer,
	serviceName string,
) (types.BuildRequest, func() error, error) {
	source, ok, err := contextsource.ParseGitBuildContextSource(req.ContextDir)
	if err != nil {
		return types.BuildRequest{}, func() error { return nil }, err
	}
	if !ok || source == nil {
		if contextsource.IsPotentialRemoteBuildContextSource(req.ContextDir) {
			return types.BuildRequest{}, func() error { return nil }, fmt.Errorf("unsupported remote build context source %q: only git repository URLs are supported", req.ContextDir)
		}
		return req, func() error { return nil }, nil
	}

	writeBuildProgressStatusInternal(ctx, progressWriter, serviceName, "resolving remote git context "+source.RepositoryURL)

	authConfig, matchedRepository, err := s.resolveGitBuildAuthInternal(ctx, source.RepositoryURL)
	if err != nil {
		return types.BuildRequest{}, func() error { return nil }, err
	}
	if matchedRepository {
		writeBuildProgressStatusInternal(ctx, progressWriter, serviceName, "using saved git credentials for "+source.RepositoryURL)
	}
	if gitkit.RequiresRemoteProbe(source.RepositoryURL) {
		writeBuildProgressStatusInternal(ctx, progressWriter, serviceName, "verifying remote git repository "+source.RepositoryURL)
		if probeGitContextErr := s.probeGitContextInternal(ctx, source.RepositoryURL, authConfig); probeGitContextErr != nil {
			return types.BuildRequest{}, func() error { return nil }, fmt.Errorf("failed to verify remote git repository %q: %w", source.RepositoryURL, probeGitContextErr)
		}
	}

	repoPath, err := s.cloneGitContextInternal(ctx, source.RepositoryURL, source.Ref, authConfig)
	if err != nil {
		return types.BuildRequest{}, func() error { return nil }, err
	}

	contextDir := repoPath
	if source.Subdir != "" {
		if validatePathErr := git.ValidatePath(repoPath, filepath.FromSlash(source.Subdir)); validatePathErr != nil {
			_ = s.cleanupGitContextInternal(repoPath)
			return types.BuildRequest{}, func() error { return nil }, fmt.Errorf("invalid git build context subdir: %w", validatePathErr)
		}
		contextDir = filepath.Join(repoPath, filepath.FromSlash(source.Subdir))
	}

	// os.* rather than acfs: the build context path comes from the user-provided
	// source and may point anywhere on the host, so no confinement root exists.
	info, err := os.Stat(contextDir)
	if err != nil {
		_ = s.cleanupGitContextInternal(repoPath)
		return types.BuildRequest{}, func() error { return nil }, fmt.Errorf("failed to stat resolved git build context: %w", err)
	}
	if !info.IsDir() {
		_ = s.cleanupGitContextInternal(repoPath)
		return types.BuildRequest{}, func() error { return nil }, errors.New("resolved git build context is not a directory")
	}

	writeBuildProgressStatusInternal(ctx, progressWriter, serviceName, "using remote build context "+source.Raw)

	resolvedReq := req
	resolvedReq.ContextDir = contextDir

	return resolvedReq, func() error { return s.cleanupGitContextInternal(repoPath) }, nil
}

func (s *BuildService) resolveGitBuildAuthInternal(ctx context.Context, rawURL string) (git.AuthConfig, bool, error) {
	if s.gitRepository == nil {
		return git.AuthConfig{}, false, nil
	}

	repository, err := s.gitRepository.FindEnabledRepositoryByURL(ctx, rawURL)
	if err != nil {
		return git.AuthConfig{}, false, fmt.Errorf("failed to resolve git repository credentials: %w", err)
	}
	if repository == nil {
		return git.AuthConfig{}, false, nil
	}

	authConfig, err := s.gitRepository.GetAuthConfig(ctx, repository)
	if err != nil {
		return git.AuthConfig{}, true, fmt.Errorf("failed to load git repository credentials: %w", err)
	}

	return authConfig, true, nil
}

func (s *BuildService) probeGitContextInternal(ctx context.Context, repositoryURL string, authConfig git.AuthConfig) error {
	if s.gitProbeFn != nil {
		return s.gitProbeFn(ctx, repositoryURL, authConfig)
	}

	if s.gitRepository != nil && s.gitRepository.Client != nil {
		return s.gitRepository.ProbeRemote(ctx, repositoryURL, authConfig)
	}

	return errors.New("git repository service not available")
}

func (s *BuildService) cloneGitContextInternal(ctx context.Context, repositoryURL, ref string, authConfig git.AuthConfig) (string, error) {
	if s.gitCloneFn != nil {
		return s.gitCloneFn(ctx, repositoryURL, ref, authConfig)
	}

	if s.gitRepository != nil && s.gitRepository.Client != nil {
		// Builds may run git describe or count commits, so they keep full history.
		return s.gitRepository.Clone(ctx, repositoryURL, ref, authConfig, 0)
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
	if s.gitRepository != nil && s.gitRepository.Client != nil {
		return s.gitRepository.Cleanup(repoPath)
	}
	return errors.New("git repository service not available")
}

func writeBuildProgressStatusInternal(ctx context.Context, progressWriter io.Writer, serviceName, status string) {
	if progressWriter == nil || strings.TrimSpace(status) == "" {
		return
	}

	line := status
	if service := strings.TrimSpace(serviceName); service != "" {
		line = service + ": " + status
	}
	if _, err := io.WriteString(progressWriter, line+"\n"); err != nil {
		slog.DebugContext(ctx, "failed to write build progress status", "error", err)
	}
}

func (s *BuildService) ListImageBuildsByEnvironmentPaginated(ctx context.Context, environmentID string, params pagination.QueryParams) ([]image.BuildRecord, pagination.Response, error) {
	if s.db == nil {
		return nil, pagination.Response{}, errors.New("build history not available")
	}

	var builds []ImageBuild
	// The list DTO never includes the build output (buildToRecord with
	// includeOutput=false), so skip reading the up-to-2-MiB output column.
	q := s.db.WithContext(ctx).Model(&ImageBuild{}).Omit("output").Where("environment_id = ?", environmentID)

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"context_dir LIKE ? OR COALESCE(dockerfile, '') LIKE ? OR COALESCE(username, '') LIKE ? OR COALESCE(provider, '') LIKE ? OR COALESCE(error_message, '') LIKE ?",
			searchPattern, searchPattern, searchPattern, searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "status", params.Filters["status"])
	q = pagination.ApplyFilter(q, "provider", params.Filters["provider"])

	params.Sort = cmp.Or(params.Sort, "createdAt")

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &builds)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate builds: %w", err)
	}

	records := make([]image.BuildRecord, 0, len(builds))
	for _, build := range builds {
		records = append(records, buildToRecord(build, false))
	}

	return records, paginationResp, nil
}

func (s *BuildService) GetImageBuildByID(ctx context.Context, environmentID, buildID string) (*image.BuildRecord, error) {
	if s.db == nil {
		return nil, errors.New("build history not available")
	}

	var build ImageBuild
	if err := s.db.WithContext(ctx).First(&build, "id = ? AND environment_id = ?", buildID, environmentID).Error; err != nil {
		return nil, err
	}

	return new(buildToRecord(build, true)), nil
}

func (s *BuildService) createBuildRecord(ctx context.Context, environmentID string, req types.BuildRequest, user *usertypes.Actor) (*ImageBuild, error) {
	buildArgs := mapToJSON(req.BuildArgs)
	labels := mapToJSON(req.Labels)
	ulimits := mapToJSON(req.Ulimits)

	var userID *string
	var username *string
	if user != nil {
		userID = &user.ID
		username = &user.Username
	}

	record := &ImageBuild{
		EnvironmentID: environmentID,
		UserID:        userID,
		Username:      username,
		Status:        ImageBuildStatusRunning,
		Provider:      req.Provider,
		ContextDir:    req.ContextDir,
		Dockerfile:    req.Dockerfile,
		Target:        req.Target,
		Tags:          database.StringSlice(req.Tags),
		Platforms:     database.StringSlice(req.Platforms),
		BuildArgs:     buildArgs,
		Labels:        labels,
		CacheFrom:     database.StringSlice(req.CacheFrom),
		CacheTo:       database.StringSlice(req.CacheTo),
		NoCache:       req.NoCache,
		Pull:          req.Pull,
		BuildNetwork:  req.Network,
		Isolation:     req.Isolation,
		ShmSize:       req.ShmSize,
		Ulimits:       ulimits,
		Entitlements:  database.StringSlice(req.Entitlements),
		Privileged:    req.Privileged,
		ExtraHosts:    database.StringSlice(req.ExtraHosts),
		Push:          req.Push,
		Load:          req.Load,
		CreatedAt:     time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return nil, fmt.Errorf("failed to create build record: %w", err)
	}
	return record, nil
}

func (s *BuildService) completeBuildRecord(
	ctx context.Context,
	buildID string,
	status ImageBuildStatus,
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
		result := tx.Model(&ImageBuild{}).Where("id = ?", buildID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("failed to update build record: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return errors.New("build record not found")
		}
		return nil
	})
}

func buildToRecord(build ImageBuild, includeOutput bool) image.BuildRecord {
	buildArgs := jsonToStringMap(build.BuildArgs)
	labels := jsonToStringMap(build.Labels)
	ulimits := jsonToStringMap(build.Ulimits)

	var output *string
	if includeOutput {
		output = build.Output
	}

	return image.BuildRecord{
		ID:              build.ID,
		EnvironmentID:   build.EnvironmentID,
		UserID:          build.UserID,
		Username:        build.Username,
		Status:          string(build.Status),
		Provider:        build.Provider,
		ContextDir:      build.ContextDir,
		Dockerfile:      build.Dockerfile,
		Target:          build.Target,
		Tags:            build.Tags,
		Platforms:       build.Platforms,
		BuildArgs:       buildArgs,
		Labels:          labels,
		CacheFrom:       build.CacheFrom,
		CacheTo:         build.CacheTo,
		NoCache:         build.NoCache,
		Pull:            build.Pull,
		Network:         build.BuildNetwork,
		Isolation:       build.Isolation,
		ShmSize:         build.ShmSize,
		Ulimits:         ulimits,
		Entitlements:    build.Entitlements,
		Privileged:      build.Privileged,
		ExtraHosts:      build.ExtraHosts,
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

func mapToJSON(input map[string]string) database.JSON {
	if len(input) == 0 {
		return nil
	}

	out := database.JSON{}
	for key, value := range input {
		out[key] = value
	}

	return kit.Ternary(len(out) == 0, nil, out)
}

func jsonToStringMap(input database.JSON) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		out[key] = fmt.Sprint(value)
	}

	return kit.Ternary(len(out) == 0, nil, out)
}

package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	libbuild "github.com/getarcaneapp/arcane/backend/pkg/libarcane/libbuild"
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
	builder         buildtypes.Builder
}

const buildHistoryOutputLimitBytes = 2 * 1024 * 1024

func NewBuildService(db *database.DB, settings *SettingsService, dockerService *DockerClientService, registryService *ContainerRegistryService) *BuildService {
	svc := &BuildService{
		db:              db,
		settings:        settings,
		dockerService:   dockerService,
		registryService: registryService,
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
	result, err := s.builder.BuildImage(ctx, req, writer, serviceName)
	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()

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

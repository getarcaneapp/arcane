// Package details derives project runtime state: service status from Compose
// and containers, include files, logs and image update checks.
package details

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.getarcane.app/docker"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/cgroup"
	"go.getarcane.app/updater/labels"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	dockerInternal "github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/iconcatalog"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/imageref"
)

// Service reads project runtime state, include files and update checks.
type Service struct {
	db              *database.DB
	dockerService   *dockerInternal.DockerClientService
	imageService    *image.ImageService
	settingsService *settings.SettingsService
}

func New(db *database.DB, dockerService *dockerInternal.DockerClientService, imageService *image.ImageService, settingsService *settings.SettingsService) *Service {
	return &Service{db: db, dockerService: dockerService, imageService: imageService, settingsService: settingsService}
}

// ComposeContainers routes the global compose-container
// list through the shared Docker client singleton when one is wired, avoiding
// a fresh docker CLI per call.
func (s *Service) ComposeContainers(ctx context.Context) ([]container.Summary, error) {
	var dockerClient client.APIClient
	if s.dockerService != nil {
		cli, err := s.dockerService.GetClient(ctx)
		if err != nil {
			return nil, err
		}
		dockerClient = cli
	}
	return projects.ListGlobalComposeContainers(ctx, dockerClient, s.dockerService.DockerHost())
}

// ComposeServices reports every Compose service with its container state,
// including services that have no container yet.
func (s *Service) ComposeServices(
	ctx context.Context,
	composeProject *types.Project,
	composeFileFullPath, projectsDirectory string,
	autoInjectEnv bool,
	catalog string,
) ([]project.RuntimeService, error) {
	meta, metaErr := projects.ParseArcaneComposeMetadata(ctx, composeFileFullPath, projectsDirectory, autoInjectEnv)
	if metaErr != nil {
		slog.WarnContext(ctx, "failed to parse Arcane compose metadata", "path", composeFileFullPath, "error", metaErr)
	}

	containers, err := projects.ComposePs(ctx, s.dockerService.DockerHost(), composeProject, nil, true)
	if err != nil {
		slog.ErrorContext(ctx, "compose ps error", "projectName", composeProject.Name, "error", err)
		return nil, fmt.Errorf("failed to get compose services status: %w", err)
	}
	// Compose ps omits image IDs, so one container list supplies them; a listing
	// failure only leaves image-level checks unmatched.
	imageIDs := make(map[string]string)
	if s.dockerService != nil {
		if summaries, listErr := s.ComposeContainers(ctx); listErr != nil {
			slog.WarnContext(ctx, "failed to list containers for project image identity", "projectName", composeProject.Name, "error", listErr)
		} else {
			for _, c := range summaries {
				if docker.ComposeProjectLabel(c.Labels) == composeProject.Name && c.ImageID != "" {
					imageIDs[c.ID] = c.ImageID
				}
			}
		}
	}
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()

	have := map[string]bool{}
	var services []project.RuntimeService

	// Create a map for quick lookup of service config
	serviceConfigs := make(map[string]types.ServiceConfig)
	for _, svc := range composeProject.Services {
		serviceConfigs[svc.Name] = svc
	}

	for _, c := range containers {
		var health *string
		if c.Health != "" {
			health = new(string(c.Health))
		}

		var svcConfig *types.ServiceConfig
		if cfg, ok := serviceConfigs[c.Service]; ok {
			svcConfig = &cfg
		}

		resolvedIcon := iconcatalog.Resolve(catalog, cmp.Or(
			projects.FindArcaneIconSet(c.Labels),
			meta.ServiceIconSets[c.Service],
			meta.ProjectIcon,
		))
		services = append(services, project.RuntimeService{
			Name:             c.Service,
			Image:            c.Image,
			Status:           string(c.State),
			ContainerID:      c.ID,
			ContainerName:    c.Name,
			Ports:            projects.FormatPorts(c.Publishers),
			Health:           health,
			IconLightURL:     resolvedIcon.IconLightURL,
			IconDarkURL:      resolvedIcon.IconDarkURL,
			ServiceConfig:    svcConfig,
			ContainerLabels:  c.Labels,
			RedeployDisabled: labels.ShouldDisableArcaneServerRedeploy(c.Labels, c.ID, currentContainerID, currentContainerErr),
			ImageID:          imageIDs[c.ID],
		})
		have[c.Service] = true
	}

	for _, svc := range composeProject.Services {
		if !have[svc.Name] {
			resolvedIcon := iconcatalog.Resolve(catalog, cmp.Or(
				meta.ServiceIconSets[svc.Name],
				meta.ProjectIcon,
			))
			services = append(services, project.RuntimeService{
				Name:          svc.Name,
				Image:         svc.Image,
				Status:        "stopped",
				Ports:         []string{},
				IconLightURL:  resolvedIcon.IconLightURL,
				IconDarkURL:   resolvedIcon.IconDarkURL,
				ServiceConfig: new(svc),
			})
		}
	}

	return services, nil
}

// IncludeFiles lists the files a compose file includes, expanding ${VAR}
// references from the project environment.
func (s *Service) IncludeFiles(ctx context.Context, composeFile string) []project.IncludeFile {
	if strings.TrimSpace(composeFile) == "" {
		return nil
	}

	// Load environment variables so that include paths with ${VAR} references are expanded
	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory, _ := projects.GetProjectsDirectory(ctx, strings.TrimSpace(cfg.ProjectsDirectory.Value))
	envLoader := projects.NewEnvLoader(projectsDirectory, filepath.Dir(composeFile), kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool))
	envMap, _, _ := envLoader.LoadEnvironment(ctx)

	includes, parseErr := projects.ParseIncludes(composeFile, envMap, false)
	if parseErr == nil {
		var includeFiles []project.IncludeFile
		for _, inc := range includes {
			includeFiles = append(includeFiles, project.IncludeFile{
				Path:         inc.Path,
				RelativePath: inc.RelativePath,
			})
		}
		return includeFiles
	}
	slog.WarnContext(ctx, "Failed to parse includes", "error", parseErr, "path", composeFile)
	return nil
}

// ContainerUpdateInfo returns update checks scoped to the projects' running containers.
func (s *Service) ContainerUpdateInfo(ctx context.Context, details []project.Details) map[string]*imagetypes.UpdateInfo {
	if s == nil || s.imageService == nil {
		return nil
	}
	var containers []container.Summary
	for _, detail := range details {
		for _, service := range detail.RuntimeServices {
			if service.ContainerID != "" {
				containers = append(containers, container.Summary{ID: service.ContainerID, Image: service.Image, ImageID: service.ImageID, Labels: service.ContainerLabels})
			}
		}
	}
	scoped, err := s.imageService.GetUpdateInfoByContainers(ctx, containers)
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch project container update info", "error", err)
		return nil
	}
	return scoped
}

// ServiceUpdateRecords loads per-service update checks for the projects.
func (s *Service) ServiceUpdateRecords(ctx context.Context, projectIDs []string) []imageupdate.ImageUpdateRecord {
	if s == nil || s.db == nil || len(projectIDs) == 0 {
		return nil
	}
	var records []imageupdate.ImageUpdateRecord
	if err := s.db.WithContext(ctx).Where("project_id <> ? AND project_id IN ?", "", projectIDs).Find(&records).Error; err != nil {
		slog.WarnContext(ctx, "failed to fetch project service update checks", "error", err)
		return nil
	}
	return records
}

// Logs streams Compose logs for the project line by line.
func Logs(ctx context.Context, projectName string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	done := make(chan error, 2)

	// Reader goroutine: forward lines to channel
	go func() {
		// Closing the read half unblocks any pending pw.Write in ComposeLogs.
		// Without it, an abandoned tail (ctx cancel, or a >1MiB line tripping
		// bufio.ErrTooLong) wedges the writer goroutine forever and this
		// function never collects its second done value.
		defer func() { _ = pr.CloseWithError(io.ErrClosedPipe) }()

		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case logsChan <- sc.Text():
			}
		}
		done <- sc.Err()
	}()

	// Writer goroutine: compose logs -> pipe
	go func() {
		composeLogsErr := projects.ComposeLogs(ctx, projects.NormalizeProjectName(projectName), pw, follow, tail, since, timestamps)
		_ = pw.Close()
		done <- composeLogsErr
	}()

	// Wait for both goroutines to finish to avoid sending on a closed channel
	err1 := <-done
	err2 := <-done

	for _, e := range []error{err1, err2} {
		if e != nil && !errors.Is(e, io.EOF) && !errors.Is(e, context.Canceled) {
			return e
		}
	}
	return nil
}

func MergeProjectContainerUpdateInfo(base map[string]*imagetypes.UpdateInfo, services []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) map[string]*imagetypes.UpdateInfo {
	result := make(map[string]*imagetypes.UpdateInfo, len(base))
	maps.Copy(result, base)
	// Shared image results are dropped for references used only by services
	// that opted out of update checks.
	monitored := make(map[string]bool)
	for _, service := range services {
		imageRef := strings.TrimSpace(service.Image)
		if imageRef == "" {
			continue
		}
		monitored[imageRef] = monitored[imageRef] || !imageref.IsUpdateCheckDisabled(service.ContainerLabels)
	}
	for imageRef, isMonitored := range monitored {
		if !isMonitored {
			delete(result, imageRef)
		}
	}
	seen := make(map[string]bool)
	for _, service := range services {
		info := scoped[service.ContainerID]
		if info == nil {
			continue
		}
		imageRef := strings.TrimSpace(service.Image)
		if imageRef == "" {
			continue
		}
		merged := *info
		if seen[imageRef] {
			previous := result[imageRef]
			merged.HasUpdate = merged.HasUpdate || previous.HasUpdate
			if previous.CheckTime.After(merged.CheckTime) {
				merged.CheckTime = previous.CheckTime
			}
			if previous.LatestVersion != merged.LatestVersion || previous.LatestDigest != merged.LatestDigest || previous.UpdateType != merged.UpdateType {
				merged.LatestVersion, merged.LatestDigest, merged.UpdateType = "", "", ""
			}
			if merged.Error == "" {
				merged.Error = previous.Error
			}
		}
		result[imageRef] = &merged
		seen[imageRef] = true
	}
	return result
}

// ConfiguredRuntimeServiceUpdateInfo binds runtime checks to the current Compose
// source, since container labels may predate an operator edit.
func ConfiguredRuntimeServiceUpdateInfo(services []types.ServiceConfig, runtime []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) map[string]*imagetypes.UpdateInfo {
	configs := make(map[string]types.ServiceConfig, len(services))
	for _, service := range services {
		configs[service.Name] = service
	}
	grouped := make(map[string][]project.RuntimeService)
	for _, service := range runtime {
		name := service.Name
		if name == "" {
			name = docker.ComposeServiceLabel(service.ContainerLabels)
		}
		config, ok := configs[name]
		if !ok || scoped[service.ContainerID] == nil {
			continue
		}
		if imageref.UpdatePolicyKey(config.Image, config.Labels) != imageref.UpdatePolicyKey(service.Image, service.ContainerLabels) {
			continue
		}
		service.Image = config.Image
		grouped[name] = append(grouped[name], service)
	}
	result := make(map[string]*imagetypes.UpdateInfo, len(grouped))
	for name, replicas := range grouped {
		result[name] = MergeProjectContainerUpdateInfo(nil, replicas, scoped)[strings.TrimSpace(configs[name].Image)]
	}
	return result
}

func ExcludeHiddenRuntimeServices(details []project.Details) (map[string]map[string]bool, map[string]map[string]bool) {
	hiddenServicesByProjectID := make(map[string]map[string]bool)
	hiddenRefsByProjectID := make(map[string]map[string]bool)
	for i := range details {
		hiddenServices := make(map[string]bool)
		hiddenRefs := make(map[string]bool)
		visibleServices := make([]project.RuntimeService, 0, len(details[i].RuntimeServices))
		for _, service := range details[i].RuntimeServices {
			hidden, _ := kit.ParseBool(service.ContainerLabels[libarcane.HiddenResourceLabel])
			if hidden {
				hiddenServices[service.Name] = true
				hiddenRefs[service.Image] = true
				continue
			}
			visibleServices = append(visibleServices, service)
		}
		for _, service := range visibleServices {
			delete(hiddenServices, service.Name)
			delete(hiddenRefs, service.Image)
		}
		hiddenServicesByProjectID[details[i].ID] = hiddenServices
		hiddenRefsByProjectID[details[i].ID] = hiddenRefs
		details[i].RuntimeServices = visibleServices
	}
	return hiddenServicesByProjectID, hiddenRefsByProjectID
}

// ResolveDetailsOverride returns the override file name and content, hidden when
// a COMPOSE_FILE selection omits it since deploy would ignore edits to it.
func ResolveDetailsOverride(projectPath, overrideContent string, composeSelection []string) (fileName, content string) {
	overridePath := projects.DetectComposeOverrideFile(projectPath)
	if len(composeSelection) > 0 {
		if overridePath == "" {
			return "", ""
		}
		// Selection entries are absolute and cleaned; compare full paths so a
		// same-named file in a subdirectory is not the project-root override.
		absOverride, err := filepath.Abs(filepath.Clean(overridePath))
		if err != nil || !slices.Contains(composeSelection, absOverride) {
			return "", ""
		}
	}
	if overridePath != "" {
		fileName = filepath.Base(overridePath)
	}
	return fileName, overrideContent
}

// ComposeSelectionRelativePaths returns a multi-file COMPOSE_FILE selection as
// ordered project-relative paths; nil for single or empty selections.
func ComposeSelectionRelativePaths(projectPath string, selection []string) []string {
	if len(selection) <= 1 {
		return nil
	}
	rels := make([]string, 0, len(selection))
	for _, f := range selection {
		if rel, err := filepath.Rel(projectPath, f); err == nil {
			rels = append(rels, rel)
		} else {
			rels = append(rels, filepath.Base(f))
		}
	}
	return rels
}

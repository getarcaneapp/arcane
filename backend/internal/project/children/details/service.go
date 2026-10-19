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
	return &Service{
		db:              db,
		dockerService:   dockerService,
		imageService:    imageService,
		settingsService: settingsService,
	}
}

// ComposeContainers lists global compose containers through the shared Docker client, avoiding a fresh docker CLI per call.
func (s *Service) ComposeContainers(ctx context.Context) ([]container.Summary, error) {
	cli, err := s.dockerService.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	return projects.ListGlobalComposeContainers(ctx, cli, s.dockerService.DockerHost())
}

func RuntimeProjectName(fallback string, runtimeContainers []container.Summary) string {
	if len(runtimeContainers) == 0 {
		return fallback
	}
	return cmp.Or(docker.ComposeProjectLabel(runtimeContainers[0].Labels), fallback)
}

// ComposeServices reports every Compose service with its container state,
// including services that have no container yet.
func (s *Service) ComposeServices(
	ctx context.Context,
	composeProject *types.Project,
	runtimeContainers []container.Summary,
	meta projects.ArcaneComposeMetadata,
	catalog string,
) ([]project.RuntimeService, error) {
	runtimeName := RuntimeProjectName(composeProject.Name, runtimeContainers)
	containers, err := projects.ComposePs(ctx, s.dockerService.DockerHost(), &types.Project{Name: runtimeName}, nil, true)
	if err != nil {
		slog.ErrorContext(ctx, "compose ps error", "projectName", runtimeName, "error", err)
		return nil, fmt.Errorf("failed to get compose services status: %w", err)
	}
	imageIDs := make(map[string]string, len(runtimeContainers))
	for _, c := range runtimeContainers {
		if c.ImageID != "" {
			imageIDs[c.ID] = c.ImageID
		}
	}
	currentContainerID, currentContainerErr := cgroup.CurrentContainerID()

	have := map[string]bool{}
	var services []project.RuntimeService
	for _, c := range containers {
		resolvedIcon := iconcatalog.Resolve(catalog, cmp.Or(
			projects.FindArcaneIconSet(c.Labels),
			meta.ServiceIconSets[c.Service],
			meta.ProjectIcon,
		))
		services = append(services, project.RuntimeService{
			Name:          c.Service,
			Image:         c.Image,
			Status:        string(c.State),
			ContainerID:   c.ID,
			ContainerName: c.Name,
			Ports:         projects.FormatPorts(c.Publishers),
			Health:        kit.Ternary(c.Health != "", new(string(c.Health)), nil),
			IconLightURL:  resolvedIcon.IconLightURL,
			IconDarkURL:   resolvedIcon.IconDarkURL,
			ServiceConfig: func() *types.ServiceConfig {
				if cfg, ok := composeProject.Services[c.Service]; ok {
					return &cfg
				}
				return nil
			}(),
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
	envMap, _, _ := projects.NewEnvLoader(projectsDirectory, filepath.Dir(composeFile), kit.ParseOrDefault(cfg.AutoInjectEnv.Value, false, strconv.ParseBool)).LoadEnvironment(ctx)

	includes, parseErr := projects.ParseIncludes(composeFile, envMap, false)
	if parseErr != nil {
		slog.WarnContext(ctx, "Failed to parse includes", "error", parseErr, "path", composeFile)
		return nil
	}
	var includeFiles []project.IncludeFile
	for _, inc := range includes {
		includeFiles = append(includeFiles, project.IncludeFile{
			Path:         inc.Path,
			RelativePath: inc.RelativePath,
		})
	}
	return includeFiles
}

// ContainerUpdateInfo returns update checks scoped to the projects' running containers.
func (s *Service) ContainerUpdateInfo(ctx context.Context, services []project.RuntimeService) map[string]*imagetypes.UpdateInfo {
	var containers []container.Summary
	for _, service := range services {
		if service.ContainerID != "" {
			containers = append(containers, container.Summary{
				ID:      service.ContainerID,
				Image:   service.Image,
				ImageID: service.ImageID,
				Labels:  service.ContainerLabels,
			})
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
	if len(projectIDs) == 0 {
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	done := make(chan error, 2)

	// Reader goroutine: forward lines to channel
	go func() {
		// Closing the read half unblocks a pending pw.Write in ComposeLogs after an abandoned tail
		// (ctx cancel or bufio.ErrTooLong); otherwise the writer goroutine wedges forever.
		defer cancel()
		defer func() { _ = pr.CloseWithError(io.ErrClosedPipe) }()

		reader := bufio.NewReaderSize(pr, 64*1024)
		var line []byte
		for {
			chunk, isPrefix, readErr := reader.ReadLine()
			if readErr != nil {
				done <- readErr
				return
			}
			line = append(line, chunk...)
			if isPrefix && len(line) < 1024*1024 {
				continue
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case logsChan <- string(line):
			}
			line = line[:0]
		}
	}()

	// Writer goroutine: compose logs -> pipe
	go func() {
		composeLogsErr := projects.ComposeLogs(ctx, projects.NormalizeProjectName(projectName), pw, follow, tail, since, timestamps)
		_ = pw.Close()
		done <- composeLogsErr
	}()

	// Wait for both goroutines to finish to avoid sending on a closed channel
	for _, e := range []error{<-done, <-done} {
		if e != nil && !errors.Is(e, io.EOF) && !errors.Is(e, context.Canceled) {
			return e
		}
	}
	return nil
}

func MergeProjectContainerUpdateInfo(base map[string]*imagetypes.UpdateInfo, services []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) map[string]*imagetypes.UpdateInfo {
	result := make(map[string]*imagetypes.UpdateInfo, len(base))
	maps.Copy(result, base)
	// Shared image results are dropped for references used only by services that opted out of update checks.
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
		previous := kit.Ternary(seen[imageRef], result[imageRef], info)
		result[imageRef] = &imagetypes.UpdateInfo{ //exhaustruct:enforce
			CheckTime: kit.Ternary(previous.CheckTime.After(info.CheckTime), previous.CheckTime, info.CheckTime),
			UpdateType: kit.Ternary(
				previous.LatestVersion != info.LatestVersion || previous.LatestDigest != info.LatestDigest || previous.UpdateType != info.UpdateType,
				"",
				info.UpdateType,
			),
			CurrentVersion: info.CurrentVersion,
			LatestVersion: kit.Ternary(
				previous.LatestVersion != info.LatestVersion || previous.LatestDigest != info.LatestDigest || previous.UpdateType != info.UpdateType,
				"",
				info.LatestVersion,
			),
			CurrentDigest: info.CurrentDigest,
			LatestDigest: kit.Ternary(
				previous.LatestVersion != info.LatestVersion || previous.LatestDigest != info.LatestDigest || previous.UpdateType != info.UpdateType,
				"",
				info.LatestDigest,
			),
			Error:          cmp.Or(info.Error, previous.Error),
			AuthMethod:     info.AuthMethod,
			AuthUsername:   info.AuthUsername,
			AuthRegistry:   info.AuthRegistry,
			ResponseTimeMs: info.ResponseTimeMs,
			HasUpdate:      info.HasUpdate || previous.HasUpdate,
			UsedCredential: info.UsedCredential,
		}
		seen[imageRef] = true
	}
	return result
}

// ConfiguredRuntimeServiceUpdateInfo binds runtime checks to the current Compose source, since labels may predate an edit.
func ConfiguredRuntimeServiceUpdateInfo(services []types.ServiceConfig, runtime []project.RuntimeService, scoped map[string]*imagetypes.UpdateInfo) map[string]*imagetypes.UpdateInfo {
	configs := make(map[string]types.ServiceConfig, len(services))
	for _, service := range services {
		configs[service.Name] = service
	}
	grouped := make(map[string][]project.RuntimeService)
	for _, service := range runtime {
		config, ok := configs[cmp.Or(service.Name, docker.ComposeServiceLabel(service.ContainerLabels))]
		if !ok || scoped[service.ContainerID] == nil {
			continue
		}
		if imageref.UpdatePolicyKey(config.Image, config.Labels) != imageref.UpdatePolicyKey(service.Image, service.ContainerLabels) {
			continue
		}
		grouped[config.Name] = append(grouped[config.Name], project.RuntimeService{
			ContainerLabels: service.ContainerLabels,
			Image:           config.Image,
			ContainerID:     service.ContainerID,
		})
	}
	result := make(map[string]*imagetypes.UpdateInfo, len(grouped))
	for name, replicas := range grouped {
		result[name] = MergeProjectContainerUpdateInfo(nil, replicas, scoped)[strings.TrimSpace(configs[name].Image)]
	}
	return result
}

func ExcludeHiddenRuntimeServices(details []project.Details) ([][]project.RuntimeService, map[string]map[string]bool, map[string]map[string]bool) {
	visibleByIndex := make([][]project.RuntimeService, len(details))
	hiddenServicesByProjectID := make(map[string]map[string]bool)
	hiddenRefsByProjectID := make(map[string]map[string]bool)
	for i := range details {
		hiddenServices := make(map[string]bool)
		hiddenRefs := make(map[string]bool)
		visibleServices := make([]project.RuntimeService, 0, len(details[i].RuntimeServices))
		for _, service := range details[i].RuntimeServices {
			if hidden, _ := kit.ParseBool(service.ContainerLabels[libarcane.HiddenResourceLabel]); hidden {
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
		visibleByIndex[i] = visibleServices
	}
	return visibleByIndex, hiddenServicesByProjectID, hiddenRefsByProjectID
}

// ResolveDetailsOverride returns the override file name and content, hidden when a COMPOSE_FILE selection omits it.
func ResolveDetailsOverride(projectPath, overrideContent string, composeSelection []string) (fileName, content string) {
	overridePath := projects.DetectComposeOverrideFile(projectPath)
	if len(composeSelection) > 0 {
		if overridePath == "" {
			return "", ""
		}
		// Selection entries are absolute; full paths keep a same-named subdirectory file from matching.
		absOverride, err := filepath.Abs(filepath.Clean(overridePath))
		if err != nil || !slices.Contains(composeSelection, absOverride) {
			return "", ""
		}
	}
	return kit.Ternary(overridePath != "", filepath.Base(overridePath), ""), overrideContent
}

// ComposeSelectionRelativePaths returns a multi-file COMPOSE_FILE selection as ordered project-relative paths.
func ComposeSelectionRelativePaths(projectPath string, selection []string) []string {
	if len(selection) <= 1 {
		return nil
	}
	rels := make([]string, 0, len(selection))
	for _, f := range selection {
		rel, err := filepath.Rel(projectPath, f)
		rels = append(rels, kit.Ternary(err == nil, rel, filepath.Base(f)))
	}
	return rels
}

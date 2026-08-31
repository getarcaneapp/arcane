package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"emperror.dev/errors"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/moby/moby/client"
	"github.com/samber/hot"
	"github.com/samber/mo"
	"gorm.io/gorm"
)

// composeNameCacheInternal maps normalized compose project names to project
// IDs so a name lookup skips the projects table scan.
type composeNameCacheInternal struct {
	mu     sync.RWMutex
	byName map[string]string
}

// parsedComposeCacheInternal caches parsed compose projects by project ID. An
// entry is dropped once its compose file or any of its includes changes on disk.
type parsedComposeCacheInternal struct {
	entries *hot.HotCache[string, composeCacheEntry]
}

type composeCacheEntry struct {
	composePath   string
	composeMtime  time.Time
	includeMtimes map[string]time.Time
	// envMtimes tracks the env files that influence file selection and
	// interpolation (.env, .env.global, COMPOSE_ENV_FILES entries). A zero time
	// records a file that did not exist when the entry was cached, so its later
	// appearance also invalidates. Needed because .env now selects the file set
	// via COMPOSE_FILE.
	envMtimes map[string]time.Time
	project   *composetypes.Project
}

func newParsedComposeCacheInternal() *parsedComposeCacheInternal {
	return &parsedComposeCacheInternal{entries: hot.NewHotCache[string, composeCacheEntry](hot.LRU, 2048).Build()}
}

func (c *composeNameCacheInternal) projectID(normalizedName string) mo.Option[string] {
	if normalizedName == "" {
		return mo.None[string]()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.byName == nil {
		return mo.None[string]()
	}

	projectID, ok := c.byName[normalizedName]
	return mo.TupleToOption(projectID, ok)
}

func (c *composeNameCacheInternal) put(normalizedName, projectID string) {
	if normalizedName == "" || projectID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.byName == nil {
		c.byName = make(map[string]string)
	}
	c.byName[normalizedName] = projectID
}

func (c *composeNameCacheInternal) invalidate(normalizedName string) {
	if normalizedName == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.byName, normalizedName)
}

func (c *composeNameCacheInternal) replace(byName map[string]string) {
	c.mu.Lock()
	c.byName = byName
	c.mu.Unlock()
}

func (c *parsedComposeCacheInternal) invalidate(projectID string) {
	if c == nil || c.entries == nil || strings.TrimSpace(projectID) == "" {
		return
	}
	c.entries.Delete(projectID)
}

func (s *ProjectService) lookupProjectByCachedComposeNameInternal(ctx context.Context, normalizedName string) (*Project, bool, error) {
	projectID, ok := s.composeNames.projectID(normalizedName).Get()
	if !ok {
		return nil, false, nil
	}

	var projectModel Project
	if err := s.db.WithContext(ctx).Where("id = ?", projectID).First(&projectModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.composeNames.invalidate(normalizedName)
			return nil, false, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, errors.WrapIf(err, "request canceled or timed out")
		}
		return nil, false, errors.WrapIf(err, "failed to get project by cached compose name")
	}
	if projects.NormalizeProjectName(projectModel.Name) != normalizedName {
		s.composeNames.invalidate(normalizedName)
		return nil, false, nil
	}

	return &projectModel, true, nil
}

func (s *ProjectService) rebuildComposeNameCacheInternal(ctx context.Context) error {
	var projectModels []Project
	if err := s.db.WithContext(ctx).Select("id", "name").Find(&projectModels).Error; err != nil {
		return err
	}

	byName := make(map[string]string, len(projectModels))
	for i := range projectModels {
		normalizedName := projects.NormalizeProjectName(projectModels[i].Name)
		if normalizedName == "" {
			continue
		}
		if _, exists := byName[normalizedName]; !exists {
			byName[normalizedName] = projectModels[i].ID
		}
	}

	s.composeNames.replace(byName)

	return nil
}

// ResolveProjectComposeFile returns the base compose file for a project. The
// precedence mirrors `docker compose`: COMPOSE_FILE in the merged environment
// (.env.global first, the project's .env on top) wins, then a GitOps sync's
// configured compose path, then standard detection.
func (s *ProjectService) ResolveProjectComposeFile(ctx context.Context, proj *Project) (string, error) {
	if proj == nil {
		return "", errors.New("project is nil")
	}

	projectsDirectory := ""
	if s.settingsService != nil {
		var dirErr error
		projectsDirectory, dirErr = s.GetProjectsDirectory(ctx)
		if dirErr != nil {
			// The .env.global layer is skipped for an empty projects directory;
			// keep resolution working but surface the misconfiguration.
			slog.WarnContext(ctx, "failed to resolve projects directory for compose selection", "projectID", proj.ID, "error", dirErr)
		}
	}
	if files, selErr := projects.ComposeFileEnvSelection(ctx, projectsDirectory, proj.Path); selErr != nil {
		return "", selErr
	} else if len(files) > 0 {
		return files[0], nil
	}

	if proj.GitOpsManagedBy != nil && strings.TrimSpace(*proj.GitOpsManagedBy) != "" {
		var syncRecord GitOpsSync
		if err := s.db.WithContext(ctx).
			Select("compose_path").
			Where("id = ?", *proj.GitOpsManagedBy).
			First(&syncRecord).Error; err == nil {
			composeFileName := strings.TrimSpace(filepath.Base(syncRecord.ComposePath))
			if composeFileName != "" && composeFileName != "." {
				candidate := filepath.Join(proj.Path, composeFileName)
				// os.Stat rather than acfs: proj.Path may be an imported project
				// outside the projects directory, and the compose file may be a
				// symlink resolving outside it.
				if info, statErr := os.Stat(candidate); statErr == nil {
					if !info.IsDir() {
						return candidate, nil
					}
				} else if !os.IsNotExist(statErr) {
					return "", errors.WrapIff(statErr, "failed to inspect GitOps compose file %s", candidate)
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.WrapIff(err, "failed to resolve GitOps compose path for project %s", proj.ID)
		}
	}

	composeFile, err := projects.DetectComposeFile(ctx, projectsDirectory, proj.Path)
	if err != nil {
		return "", common.Classify(common.ErrProjectComposeFileNotFound, errors.WrapIf(err, "Project compose file not found"))
	}

	return composeFile, nil
}

func (s *ProjectService) loadComposeProjectForProjectInternal(ctx context.Context, proj *Project, cfg *settings.Settings) (*composetypes.Project, string, error) {
	composeFileFullPath, err := s.ResolveProjectComposeFile(ctx, proj)
	if err != nil {
		return nil, "", err
	}

	if cfg == nil {
		cfg = s.settingsService.GetSettingsOrDefaults(ctx)
	}
	projectsDirectory := getProjectsDirectoryOrDefaultInternal(ctx, cfg)

	var dockerClient *client.Client
	if s.dockerService != nil {
		dockerClient, _ = s.dockerService.GetClient(ctx)
	}
	pathMapper := projects.NewPathMapperForConfiguredDirectory(
		ctx,
		s.settingsService.GetStringSetting(ctx, "projectsDirectory", "/app/data/projects"),
		"/app/data/projects",
		dockerClient,
	)

	composeProject, loadErr := projects.LoadComposeProject(ctx, composeFileFullPath, projects.NormalizeProjectName(proj.Name), projectsDirectory, utils.BoolOrDefault(cfg.AutoInjectEnv.Value, false), pathMapper, nil, nil, false)
	if loadErr != nil {
		return nil, "", loadErr
	}

	return composeProject, composeFileFullPath, nil
}

func (s *ProjectService) getCachedComposeProjectInternal(ctx context.Context, proj *Project, cfg *settings.Settings) (*composetypes.Project, error) {
	if proj == nil {
		return nil, errors.New("project is nil")
	}
	if s.parsedCompose == nil {
		s.parsedCompose = newParsedComposeCacheInternal()
	}

	if cached, ok := s.parsedCompose.entries.Peek(proj.ID); ok {
		if validComposeCacheEntryInternal(cached) {
			return cached.project, nil
		}
		s.parsedCompose.entries.Delete(proj.ID)
	}

	if cfg == nil {
		cfg = s.settingsService.GetSettingsOrDefaults(ctx)
	}

	entry, found, err := s.parsedCompose.entries.GetWithLoaders(proj.ID, func(_ []string) (map[string]composeCacheEntry, error) {
		composeProject, composePath, err := s.loadComposeProjectForProjectInternal(ctx, proj, cfg)
		if err != nil {
			return nil, err
		}

		entry := composeCacheEntry{
			composePath:   composePath,
			includeMtimes: make(map[string]time.Time),
			envMtimes:     collectEnvMtimesInternal(proj, getProjectsDirectoryOrDefaultInternal(ctx, cfg), composeProject),
			project:       composeProject,
		}
		// os.Stat rather than acfs here and below: see
		// validComposeCacheEntryInternal — these paths may resolve outside the
		// project directory (#3556).
		if info, statErr := os.Stat(composePath); statErr == nil {
			entry.composeMtime = info.ModTime()
		} else {
			return nil, errors.WrapIf(statErr, "stat compose file")
		}
		if composeProject != nil {
			for _, composeFile := range composeProject.ComposeFiles {
				if composeFile == "" || composeFile == composePath {
					continue
				}
				info, statErr := os.Stat(composeFile)
				if statErr != nil {
					return nil, errors.WrapIff(statErr, "stat compose include %s", composeFile)
				}
				entry.includeMtimes[composeFile] = info.ModTime()
			}
		}

		return map[string]composeCacheEntry{proj.ID: entry}, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("compose cache loader returned no project")
	}

	return entry.project, nil
}

// collectEnvMtimesInternal records the mtimes of every env file that influences
// compose-file selection or interpolation for a project: the project .env, the
// global .env.global, and any COMPOSE_ENV_FILES entries. A file that does not
// exist is recorded as a zero time so its later appearance invalidates too.
func collectEnvMtimesInternal(proj *Project, projectsDirectory string, composeProject *composetypes.Project) map[string]time.Time {
	paths := []string{
		filepath.Join(proj.Path, projects.EffectiveEnvFileName),
		filepath.Join(projectsDirectory, projects.GlobalEnvFileName),
	}
	if composeProject != nil {
		if envOpts, err := projects.ParseComposeEnvOptions(composeProject.WorkingDir, projects.EnvMap(composeProject.Environment)); err == nil {
			paths = append(paths, envOpts.EnvFiles...)
		}
	}

	mtimes := make(map[string]time.Time, len(paths))
	for _, p := range paths {
		// os.Stat rather than acfs: env files may be symlinks resolving outside
		// any confinement root (a supported setup).
		if info, err := os.Stat(p); err == nil {
			mtimes[p] = info.ModTime()
		} else {
			mtimes[p] = time.Time{}
		}
	}
	return mtimes
}

// The compose file and its includes are absolute paths compose-go resolved, and
// either may legitimately be a symlink to, or a file under, a directory outside
// the project (#3556). These invalidation probes therefore stay on os.Stat
// rather than a root-confined stat, which would reject those layouts.
func validComposeCacheEntryInternal(entry composeCacheEntry) bool {
	if entry.project == nil || entry.composePath == "" {
		return false
	}

	info, err := os.Stat(entry.composePath)
	if err != nil || !info.ModTime().Equal(entry.composeMtime) {
		return false
	}
	for includePath, cachedMtime := range entry.includeMtimes {
		info, err := os.Stat(includePath)
		if err != nil || !info.ModTime().Equal(cachedMtime) {
			return false
		}
	}
	for envPath, cachedMtime := range entry.envMtimes {
		info, err := os.Stat(envPath)
		switch {
		case err != nil:
			// Present when cached, now gone/unreadable: invalidate.
			if !cachedMtime.IsZero() {
				return false
			}
		case cachedMtime.IsZero():
			// Absent when cached, now present: invalidate.
			return false
		case !info.ModTime().Equal(cachedMtime):
			return false
		}
	}
	return true
}

func (s *ProjectService) refreshComposeProjectNameInternal(ctx context.Context, proj *Project) {
	if proj == nil {
		return
	}

	dirName := proj.Name
	if proj.DirName != nil && *proj.DirName != "" {
		dirName = *proj.DirName
	}

	meta, err := s.loadComposeMetadataForSyncInternal(ctx, proj.Path, dirName)
	if err != nil {
		slog.WarnContext(ctx, "failed to refresh compose project name", "projectID", proj.ID, "path", proj.Path, "error", err)
		return
	}

	updates := map[string]any{}
	shouldUpdateName := meta.explicitProjectName || projects.NormalizeProjectName(proj.Name) != proj.Name
	if shouldUpdateName && meta.resolvedProjectName != "" && proj.Name != meta.resolvedProjectName {
		updates["name"] = meta.resolvedProjectName
	}
	if mo.PointerToOption(proj.ComposeProjectName) != mo.PointerToOption(meta.composeProjectName) {
		updates["compose_project_name"] = meta.composeProjectName
	}
	if len(updates) == 0 {
		return
	}

	updates["updated_at"] = time.Now()
	if err := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", proj.ID).
		Updates(updates).Error; err != nil {
		slog.WarnContext(ctx, "failed to persist refreshed compose project name", "projectID", proj.ID, "error", err)
		return
	}

	if name, ok := updates["name"].(string); ok {
		proj.Name = name
	}
	if _, ok := updates["compose_project_name"]; ok {
		proj.ComposeProjectName = meta.composeProjectName
	}
}

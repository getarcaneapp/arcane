package project

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/moby/moby/client"
	"github.com/samber/mo"
	"gorm.io/gorm"
)

// projectCleanupDecision records a project the reconcile pass intends to delete,
// alongside the reason logged when the deletion is carried out.
type projectCleanupDecision struct {
	project Project
	reason  string
}

type composeSyncMetadataInternal struct {
	serviceCount        int
	resolvedProjectName string
	composeProjectName  *string
	explicitProjectName bool
}

func (s *ProjectService) refreshProjectImageRefsInternal(ctx context.Context, proj *Project) {
	if proj == nil || proj.ID == "" {
		return
	}

	s.parsedCompose.invalidate(proj.ID)
	refs, buildRefs, err := s.getProjectImageRefsFromComposeInternal(ctx, *proj, nil)
	if err != nil {
		if dbErr := s.db.WithContext(ctx).
			Model(&Project{}).
			Where("id = ?", proj.ID).
			Updates(map[string]any{
				"image_refs_json":       "",
				"build_image_refs_json": nil,
			}).Error; dbErr != nil {
			slog.WarnContext(ctx, "failed to clear stale project image refs", "projectID", proj.ID, "error", dbErr)
		}
		proj.ImageRefsJSON = ""
		proj.BuildImageRefsJSON = nil
		slog.WarnContext(ctx, "failed to refresh project image refs", "projectID", proj.ID, "projectName", proj.Name, "error", err)
		return
	}
	imageRefsJSON := projects.MarshalImageRefsJSON(refs)
	buildImageRefsJSON := projects.MarshalImageRefsJSON(buildRefs)
	if buildImageRefsJSON == "" {
		buildImageRefsJSON = "[]"
	}
	if err := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", proj.ID).
		Updates(map[string]any{
			"image_refs_json":       imageRefsJSON,
			"build_image_refs_json": buildImageRefsJSON,
		}).Error; err != nil {
		slog.WarnContext(ctx, "failed to persist project image refs", "projectID", proj.ID, "error", err)
		return
	}
	proj.ImageRefsJSON = imageRefsJSON
	proj.BuildImageRefsJSON = new(buildImageRefsJSON)
}

func (s *ProjectService) HandleProjectFilesChanged(ctx context.Context, paths []string) {
	if len(paths) == 0 || s.db == nil {
		return
	}

	affected, err := s.resolveProjectsByChangedPathsInternal(ctx, paths)
	if err != nil {
		slog.WarnContext(ctx, "failed to resolve changed project files", "error", err)
		return
	}
	for i := range affected {
		s.parsedCompose.invalidate(affected[i].ID)
		s.refreshProjectImageRefsInternal(ctx, &affected[i])
		if err := s.reconcileComposeTagsForProjectInternal(ctx, &affected[i]); err != nil {
			slog.WarnContext(ctx, "failed to reconcile Compose project tags after file change", "projectID", affected[i].ID, "error", err)
		}
	}
}

func (s *ProjectService) BackfillProjectImageRefs(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, nil
	}

	var projectsList []Project
	if err := s.db.WithContext(ctx).
		Where("build_image_refs_json IS NULL").
		Find(&projectsList).Error; err != nil {
		return 0, errors.WrapIf(err, "list projects for image ref backfill")
	}
	for i := range projectsList {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		s.refreshProjectImageRefsInternal(ctx, &projectsList[i])
	}
	return len(projectsList), nil
}

func (s *ProjectService) resolveProjectsByChangedPathsInternal(ctx context.Context, paths []string) ([]Project, error) {
	var projectsList []Project
	if err := s.db.WithContext(ctx).Find(&projectsList).Error; err != nil {
		return nil, errors.WrapIf(err, "list projects for changed paths")
	}

	seen := make(map[string]struct{})
	affected := make([]Project, 0)
	for _, changedPath := range paths {
		cleanChangedPath := filepath.Clean(changedPath)
		for _, proj := range projectsList {
			projectPath := filepath.Clean(proj.Path)
			if cleanChangedPath != projectPath && !strings.HasPrefix(cleanChangedPath, projectPath+string(os.PathSeparator)) {
				continue
			}
			if _, ok := seen[proj.ID]; ok {
				continue
			}
			seen[proj.ID] = struct{}{}
			affected = append(affected, proj)
		}
	}
	return affected, nil
}

func (s *ProjectService) SyncProjectsFromFileSystem(ctx context.Context) error {
	// Serialized because the walk and the cleanup are two halves of one
	// decision: overlapping syncs let an older walk's cleanup delete a project a
	// newer walk had just upserted, because the older walk's `seen` set predates
	// it. Filesystem-watcher debounces fire these back to back.
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	followProjectSymlinks := s.settingsService.GetBoolSetting(ctx, "followProjectSymlinks", false)
	projectsDir, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		slog.WarnContext(ctx, "unable to prepare projects directory", "error", err)
		return nil
	}

	discoveredProjects, discoveryErr := projects.DiscoverProjectDirectories(ctx, projectsDir, followProjectSymlinks, s.config.ProjectScanMaxDepth)
	if discoveryErr != nil {
		if os.IsNotExist(discoveryErr) {
			return nil
		}
		return errors.WrapIff(discoveryErr, "Failed to discover projects in %q", projectsDir)
	}

	renameSyncState := s.activeProjectRenameSyncStateInternal(ctx)
	seen := map[string]struct{}{}
	for _, discoveredProject := range discoveredProjects {
		if renameSyncState.skipDiscoveredPathInternal(discoveredProject.Path) {
			continue
		}
		if uerr := s.upsertProjectForDir(ctx, discoveredProject.DirName, discoveredProject.Path); uerr != nil {
			slog.WarnContext(ctx, "failed to sync project from folder", "dir", discoveredProject.Path, "error", uerr)
			continue
		}
		seen[discoveredProject.Path] = struct{}{}
	}
	renameSyncState.markProtectedPathsSeenInternal(seen)

	if cerr := s.cleanupDBProjectsInternal(ctx, seen, followProjectSymlinks, projectsDir, s.config.ProjectScanMaxDepth); cerr != nil {
		slog.WarnContext(ctx, "error during DB cleanup of projects", "error", cerr)
	}

	return nil
}

func (s *ProjectService) upsertProjectForDir(ctx context.Context, dirName, dirPath string) error {
	var existing Project
	err := s.db.WithContext(ctx).
		Where("path = ?", dirPath).
		First(&existing).Error

	composeMetadata, serviceCountErr := s.loadComposeMetadataForSyncInternal(ctx, dirPath, dirName)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create a minimal project entry
		reason := "Project discovered from filesystem, status pending Docker service query"
		proj := &Project{
			Name:               composeMetadata.resolvedProjectName,
			DirName:            new(dirName),
			Path:               dirPath,
			Status:             ProjectStatusUnknown,
			StatusReason:       new(reason),
			ServiceCount:       composeMetadata.serviceCount,
			RunningCount:       0,
			ComposeProjectName: composeMetadata.composeProjectName,
		}
		slog.InfoContext(ctx, "Discovered new project with unknown status",
			"project", dirName,
			"path", dirPath,
			"reason", reason)
		if serviceCountErr != nil {
			slog.WarnContext(ctx, "failed to read compose service count during project discovery", "project", dirName, "path", dirPath, "error", serviceCountErr)
		}
		if cerr := s.db.WithContext(ctx).Create(proj).Error; cerr != nil {
			return errors.WrapIff(cerr, "create project for %q failed", dirPath)
		}
		s.warnDuplicateComposeNameForPathInternal(ctx, composeMetadata.resolvedProjectName, dirPath, proj.ID)
		return s.reconcileComposeTagsForProjectInternal(ctx, proj)
	}
	if err != nil {
		return errors.WrapIff(err, "query existing project for %q failed", dirPath)
	}

	updates := map[string]any{}
	if existing.Path != dirPath {
		updates["path"] = dirPath
	}
	if existing.DirName == nil || *existing.DirName != dirName {
		updates["dir_name"] = dirName
	}
	if serviceCountErr == nil && existing.ServiceCount != composeMetadata.serviceCount {
		updates["service_count"] = composeMetadata.serviceCount
	} else if serviceCountErr != nil {
		slog.WarnContext(ctx, "failed to refresh compose service count during project sync", "projectID", existing.ID, "path", dirPath, "error", serviceCountErr)
	}
	if serviceCountErr == nil && mo.PointerToOption(existing.ComposeProjectName) != mo.PointerToOption(composeMetadata.composeProjectName) {
		updates["compose_project_name"] = composeMetadata.composeProjectName
	}
	if serviceCountErr == nil {
		if composeMetadata.explicitProjectName {
			if existing.Name != composeMetadata.resolvedProjectName {
				updates["name"] = composeMetadata.resolvedProjectName
			}
		} else if normalizedExistingName := projects.NormalizeProjectName(existing.Name); normalizedExistingName != existing.Name {
			updates["name"] = normalizedExistingName
		}
	}
	if len(updates) == 0 {
		return s.reconcileComposeTagsForProjectInternal(ctx, &existing)
	}

	updates["updated_at"] = time.Now()
	if uerr := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("id = ?", existing.ID).
		Updates(updates).Error; uerr != nil {
		return errors.WrapIff(uerr, "update project %s failed", existing.ID)
	}
	if serviceCountErr == nil {
		s.warnDuplicateComposeNameForPathInternal(ctx, composeMetadata.resolvedProjectName, dirPath, existing.ID)
	}
	return s.reconcileComposeTagsForProjectInternal(ctx, &existing)
}

func (s *ProjectService) warnDuplicateComposeNameForPathInternal(ctx context.Context, composeProjectName, dirPath, projectID string) {
	if strings.TrimSpace(composeProjectName) == "" {
		return
	}

	var count int64
	if err := s.db.WithContext(ctx).
		Model(&Project{}).
		Where("name = ? AND path <> ? AND id <> ?", composeProjectName, dirPath, projectID).
		Count(&count).Error; err != nil {
		slog.WarnContext(ctx, "failed to check duplicate compose project names during project sync", "composeProjectName", composeProjectName, "path", dirPath, "error", err)
		return
	}
	if count > 0 {
		slog.WarnContext(ctx, "multiple project directories resolve to the same compose project name", "composeProjectName", composeProjectName, "path", dirPath, "duplicates", count)
	}
}

func (s *ProjectService) cleanupDBProjectsInternal(ctx context.Context, seen map[string]struct{}, followProjectSymlinks bool, projectsDir string, maxDepth int) error {
	var all []Project
	if err := s.db.WithContext(ctx).Find(&all).Error; err != nil {
		return errors.WrapIf(err, "list projects for cleanup failed")
	}

	// Decide deletions without performing them. Collecting decisions up front lets
	// the mass-wipe guard veto an entire suspicious pass (e.g. the projects volume
	// is unmounted, so every path is missing at once) before any rows are removed.
	candidates := 0
	pendingDeletions := make([]projectCleanupDecision, 0)
	tempDeletions := make([]projectCleanupDecision, 0)
	for _, p := range all {
		if skipProjectCleanupInternal(p, seen) {
			continue
		}
		if isInternalScratchProjectInternal(p) {
			tempDeletions = append(tempDeletions, projectCleanupDecision{project: p, reason: "removed internal Arcane scratch record (project-update/gitops temp dir)"})
			continue
		}
		// Projects inside filesystem snapshot/trash directories (e.g. BTRFS
		// #snapshot) are point-in-time copies mistakenly registered by earlier
		// discovery passes. The decision is name-based, not missing-path-based,
		// so it bypasses the mass-wipe guard like the scratch records above.
		if rel := getProjectRelativePathInternal(projectsDir, p.Path); rel != "" && projects.PathContainsSnapshotDirectory(rel) {
			tempDeletions = append(tempDeletions, projectCleanupDecision{project: p, reason: "removed project inside a filesystem snapshot/trash directory"})
			continue
		}
		candidates++
		if decision, remove := s.evaluateProjectCleanupInternal(ctx, p, followProjectSymlinks, projectsDir, maxDepth).Get(); remove {
			pendingDeletions = append(pendingDeletions, decision)
		}
	}

	for _, decision := range tempDeletions {
		s.deleteProjectDuringCleanupInternal(ctx, decision.project, decision.reason)
	}

	if cleanupWouldMassWipeInternal(ctx, candidates, len(pendingDeletions), projectsDir) {
		return nil
	}

	for _, decision := range pendingDeletions {
		s.deleteProjectDuringCleanupInternal(ctx, decision.project, decision.reason)
	}
	return nil
}

// cleanupWouldMassWipeInternal reports whether the pending deletions look like an
// accidental mass wipe rather than legitimate removals. It engages when a single
// pass would prune more than one project AND more than half of the cleanup
// candidates — so the table cannot be near-emptied at once (e.g. when the projects
// directory is unmounted or mis-mapped and every path goes missing), no matter how
// few projects the deployment has. A single removal is always allowed: it is
// indistinguishable from a legitimate "deleted my only project" and is not a mass
// wipe. When the guard engages it logs a WARN pointing the operator at the likely
// volume/mount misconfiguration and the caller skips every deletion in the pass.
func cleanupWouldMassWipeInternal(ctx context.Context, candidates, deleteCount int, projectsDir string) bool {
	if deleteCount <= 1 || deleteCount*2 <= candidates {
		return false
	}

	slog.WarnContext(ctx,
		"skipping project cleanup: this reconcile would delete most projects in a single pass, which usually means the projects directory is empty, unmounted, or mis-mapped; preserving DB records — check the projects volume is mounted and mapped correctly",
		"wouldDelete", deleteCount,
		"cleanupCandidates", candidates,
		"projectsDir", projectsDir,
	)
	return true
}

func skipProjectCleanupInternal(p Project, seen map[string]struct{}) bool {
	// Skip paths seen in this pass.
	if _, ok := seen[p.Path]; ok {
		return true
	}

	// Skip projects whose lifecycle is owned by the gitops system. Their compose
	// files may not exist on disk yet (e.g. during a sync or after an SSH/clone
	// failure) and should never be deleted here.
	return p.GitOpsManagedBy != nil && strings.TrimSpace(*p.GitOpsManagedBy) != ""
}

// isInternalScratchProjectInternal reports whether a project row was imported from
// one of Arcane's own scratch directories (project-update preview/backup, or GitOps
// sync-stage/backup). Such rows are never real user projects — a crash or restart
// mid-operation can leak the scratch dir, which the filesystem discovery then imports.
// They are force-removed during cleanup regardless of whether the dir still exists.
func isInternalScratchProjectInternal(p Project) bool {
	if projects.IsInternalScratchDirName(p.Name) || projects.IsInternalScratchDirName(filepath.Base(p.Path)) {
		return true
	}
	return p.DirName != nil && projects.IsInternalScratchDirName(*p.DirName)
}

// evaluateProjectCleanupInternal decides whether a project that was not seen in
// the current filesystem pass should be pruned. It performs only read-only checks
// (warning in place for the "keep" cases); the actual deletion is deferred to the
// caller so the mass-wipe guard can veto an entire suspicious pass.
func (s *ProjectService) evaluateProjectCleanupInternal(ctx context.Context, p Project, followProjectSymlinks bool, projectsDir string, maxDepth int) mo.Option[projectCleanupDecision] {
	if s.projectExceedsScanDepthInternal(p, projectsDir, maxDepth) {
		return mo.Some(projectCleanupDecision{project: p, reason: "removed project: directory is beyond the configured scan depth"})
	}

	validDir, err := projects.IsProjectDirectoryPath(p.Path, followProjectSymlinks)
	if err != nil {
		return evaluateProjectPathErrorInternal(ctx, p, err)
	}
	if !validDir {
		return mo.Some(projectCleanupDecision{project: p, reason: "removed project: path is no longer a valid project directory"})
	}

	return s.evaluateProjectComposeFileInternal(ctx, p)
}

func (s *ProjectService) projectExceedsScanDepthInternal(p Project, projectsDir string, maxDepth int) bool {
	// Remove projects that still exist on disk but now fall outside the configured
	// scan depth (e.g. after PROJECT_SCAN_MAX_DEPTH was lowered). They are no
	// longer discovered, so they must not linger in the list. Projects at the
	// projects root or outside it (relativePath == "") are left to the on-disk
	// validation below.
	if maxDepth <= 0 {
		return false
	}

	rel := getProjectRelativePathInternal(projectsDir, p.Path)
	return rel != "" && strings.Count(rel, "/")+1 > maxDepth
}

func evaluateProjectPathErrorInternal(ctx context.Context, p Project, err error) mo.Option[projectCleanupDecision] {
	if os.IsNotExist(err) {
		return mo.Some(projectCleanupDecision{project: p, reason: "removed project: directory no longer exists"})
	}

	slog.WarnContext(ctx, "stat error during cleanup; keeping DB record", "path", p.Path, "error", err)
	return mo.None[projectCleanupDecision]()
}

func (s *ProjectService) evaluateProjectComposeFileInternal(ctx context.Context, p Project) mo.Option[projectCleanupDecision] {
	_, err := s.ResolveProjectComposeFile(ctx, &p)
	if err == nil {
		return mo.None[projectCleanupDecision]()
	}

	// The project directory still exists here (it passed the directory-validity
	// check above). Only prune the DB record when the directory genuinely has no
	// compose file. Any other resolution failure — an ambiguous match ("multiple
	// custom compose files"), an unreadable directory, a transient parse error —
	// means the project still has compose content on disk and may be deployable.
	// Deleting it would silently destroy a live project whose files are intact, so
	// keep the record and warn instead.
	if !errors.Is(err, common.ErrComposeFileNotFound) {
		slog.WarnContext(ctx, "project directory present but compose file unresolved during cleanup; keeping DB record",
			"projectID", p.ID, "path", p.Path, "error", err)
		return mo.None[projectCleanupDecision]()
	}

	return mo.Some(projectCleanupDecision{project: p, reason: "removed orphaned project: directory present but contains no compose file"})
}

// deleteProjectDuringCleanupInternal removes a project record discovered to be
// stale during the filesystem reconcile. Every removal is logged (WARN on
// success, ERROR on failure) so this destructive operation always leaves an
// audit trail — previously successful deletions were silent.
func (s *ProjectService) deleteProjectDuringCleanupInternal(ctx context.Context, p Project, reason string, attrs ...any) {
	logAttrs := make([]any, 0, 6+len(attrs))
	logAttrs = append(logAttrs, "projectID", p.ID, "name", p.Name, "path", p.Path)
	logAttrs = append(logAttrs, attrs...)

	if derr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteProjectWithTagsInternal(tx, p.ID)
	}); derr != nil {
		slog.ErrorContext(ctx, "failed to delete project during filesystem cleanup",
			append(logAttrs, "reason", reason, "error", derr)...)
		return
	}

	slog.WarnContext(ctx, reason, logAttrs...)
}

// loadComposeMetadataForSyncInternal loads the compose file once and returns
// the service count plus compose-go's effective project name.
func (s *ProjectService) loadComposeMetadataForSyncInternal(ctx context.Context, dirPath, dirName string) (composeSyncMetadataInternal, error) {
	normName := projects.NormalizeProjectName(dirName)
	meta := composeSyncMetadataInternal{
		resolvedProjectName: normName,
	}

	cfg := s.settingsService.GetSettingsOrDefaults(ctx)
	projectsDirectory, pErr := projects.GetProjectsDirectory(ctx, strings.TrimSpace(cfg.ProjectsDirectory.Value))
	if pErr != nil {
		return meta, pErr
	}

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

	autoInjectEnv := utils.BoolOrDefault(cfg.AutoInjectEnv.Value, false)

	// First, try loading without forcing a project name so compose-go can
	// resolve COMPOSE_PROJECT_NAME from the .env file. If this fails (e.g.
	// no .env and directory name is not a valid compose project name), fall
	// back to the normalized directory name.
	proj, _, err := projects.LoadComposeProjectFromDir(ctx, dirPath, "", projectsDirectory, autoInjectEnv, pathMapper)
	if err != nil {
		proj, _, err = projects.LoadComposeProjectFromDir(ctx, dirPath, normName, projectsDirectory, autoInjectEnv, pathMapper)
		if err != nil {
			return meta, err
		}
	} else if proj.Name != "" && proj.Name != normName {
		meta.explicitProjectName = true
	}

	meta.serviceCount = len(proj.Services)
	if proj.Name != "" {
		meta.resolvedProjectName = proj.Name
	}

	// If compose-go resolved a different name (from COMPOSE_PROJECT_NAME),
	// store it so we can match containers correctly.
	if proj.Name != "" && proj.Name != normName {
		meta.composeProjectName = new(proj.Name)
	}

	return meta, nil
}

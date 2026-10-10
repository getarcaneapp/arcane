package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/getarcaneapp/arcane/types/v2/gitops"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/samber/mo"
	"go.getarcane.app/acfs"
	"go.getarcane.app/builds/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
)

type builder interface {
	BuildImage(ctx context.Context, environmentID string, req types.BuildRequest, progressWriter io.Writer, serviceName string, user *usertypes.Actor) (*types.BuildResult, error)
	BuildSettings() types.BuildSettings
}

// projectMetadataEnv carries the request-scoped inputs compose
// resolution needs beyond the project itself, resolved once per list request.
type projectMetadataEnv struct {
	projectsDirectory string
	autoInjectEnv     bool
	// settings is the snapshot shared by compose loads; nil resolves lazily.
	settings *settings.Settings
	// gitOpsComposePaths maps preloaded GitOps sync IDs to their configured
	// compose paths. Sync IDs that were preloaded but have no row map to "",
	// which resolves the same way as a missing row; sync IDs absent from the
	// map are queried per project.
	gitOpsComposePaths map[string]string

	// composeFiles memoizes successfully resolved compose files by project ID.
	// Failures are not stored so a later phase in the same request retries
	// resolution instead of replaying an error that may have been transient.
	composeFilesMu sync.Mutex
	composeFiles   map[string]string
}

func gitOpsSyncID(proj *Project) string {
	if proj == nil || proj.GitOpsManagedBy == nil {
		return ""
	}
	return strings.TrimSpace(*proj.GitOpsManagedBy)
}

// composeFile memoizes successful compose file resolutions for the request.
// The lock is not held across resolve, which does I/O; workers resolve
// distinct projects, so a rare duplicate resolution is harmless.
func (env *projectMetadataEnv) composeFile(projectID string, resolve func() (string, error)) (string, error) {
	if env == nil || projectID == "" {
		return resolve()
	}
	env.composeFilesMu.Lock()
	cached, ok := env.composeFiles[projectID]
	env.composeFilesMu.Unlock()
	if ok {
		return cached, nil
	}
	path, err := resolve()
	if err != nil {
		return "", err
	}
	env.composeFilesMu.Lock()
	if env.composeFiles == nil {
		env.composeFiles = make(map[string]string)
	}
	env.composeFiles[projectID] = path
	env.composeFilesMu.Unlock()
	return path, nil
}

// composeNameCache maps normalized compose project names to project
// IDs so a name lookup skips the projects table scan.
type composeNameCache struct {
	mu     sync.RWMutex
	byName map[string]string
}

func (c *composeNameCache) projectID(normalizedName string) mo.Option[string] {
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

func (c *composeNameCache) put(normalizedName, projectID string) {
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

func (c *composeNameCache) invalidate(normalizedName string) {
	if normalizedName == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.byName, normalizedName)
}

func (c *composeNameCache) replace(byName map[string]string) {
	c.mu.Lock()
	c.byName = byName
	c.mu.Unlock()
}

// projectCleanupDecision records a project the reconcile pass intends to delete,
// alongside the reason logged when the deletion is carried out.
type projectCleanupDecision struct {
	project Project
	reason  string
}

func deleteProjectWithTags(tx *gorm.DB, projectID string) error {
	if err := tx.Where("project_id = ?", projectID).Delete(&ProjectTag{}).Error; err != nil {
		return fmt.Errorf("delete project tags: %w", err)
	}
	if err := tx.Where("mode = ? AND project_id = ?", gitops.SyncModeBackup, projectID).Delete(&GitOpsSync{}).Error; err != nil {
		return fmt.Errorf("delete project git backups: %w", err)
	}
	if err := tx.Delete(&Project{}, "id = ?", projectID).Error; err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// resolveAuthoritativeProjectName enforces that a top-level `name:` in
// the compose file is authoritative over the submitted project name. For
// name-only renames, it checks the compose file on disk so the lock can't be
// bypassed via the API.
func resolveAuthoritativeProjectName(ctx context.Context, proj *Project, name, composeContent *string) *string {
	if composeContent != nil {
		if yamlName := projects.ComposeContentProjectName(*composeContent); yamlName != "" {
			return &yamlName
		}
		return name
	}
	if name != nil {
		if onDiskCompose, _, readErr := projects.ReadProjectFiles(ctx, proj.Path, ""); readErr == nil {
			if yamlName := projects.ComposeContentProjectName(onDiskCompose); yamlName != "" {
				return &yamlName
			}
		}
	}
	return name
}

func withProjectRenameRollback(ctx context.Context, proj *Project, projectStateCommitted *bool, run func() error) error {
	originalPath := proj.Path
	originalDirName := proj.DirName

	if err := run(); err != nil {
		if projectStateCommitted != nil && *projectStateCommitted {
			return err
		}
		if proj.Path != originalPath {
			// The rollback has to run even when the caller's context is already
			// cancelled, or a cancelled update leaves the directory renamed
			// with the database still pointing at the original path.
			rollbackCtx := context.WithoutCancel(ctx)

			// Both paths share a parent whenever the rename stayed inside the
			// projects directory; an imported project can sit elsewhere, in
			// which case the move crosses roots and cannot be confined.
			var renameErr error
			if parent := filepath.Dir(originalPath); parent == filepath.Dir(proj.Path) {
				renameErr = acfs.Rename(rollbackCtx, parent, "/"+filepath.Base(proj.Path), "/"+filepath.Base(originalPath))
			} else {
				renameErr = os.Rename(proj.Path, originalPath)
			}
			if renameErr != nil {
				slog.WarnContext(ctx, "failed to rollback project directory rename", "from", proj.Path, "to", originalPath, "error", renameErr)
				return err
			}
			proj.Path = originalPath
			proj.DirName = originalDirName
		}
		return err
	}

	return nil
}

// projectRecord exposes a stored project's state to child features.
func projectRecord(p Project) projecttypes.Record {
	return projecttypes.Record{
		ID:                 p.ID,
		Name:               p.Name,
		DirName:            p.DirName,
		Path:               p.Path,
		Status:             string(p.Status),
		StatusReason:       p.StatusReason,
		ServiceCount:       p.ServiceCount,
		RunningCount:       p.RunningCount,
		GitOpsManagedBy:    p.GitOpsManagedBy,
		ComposeProjectName: p.ComposeProjectName,
		BuildImageRefsJSON: p.BuildImageRefsJSON,
		IsArchived:         p.IsArchived,
		ArchivedAt:         p.ArchivedAt,
		CreatedAt:          p.CreatedAt,
		UpdatedAt:          p.UpdatedAt,
	}
}

func projectRecords(projectsList []Project) []projecttypes.Record {
	records := make([]projecttypes.Record, len(projectsList))
	for i, p := range projectsList {
		records[i] = projectRecord(p)
	}
	return records
}

// tagStore persists tag assignments for the tags child inside one transaction.
type tagStore struct {
	tx *gorm.DB
}

func (s tagStore) CountTag(projectID, name string, source projecttypes.TagSource) (int64, error) {
	var count int64
	err := s.tx.Model(&ProjectTag{}).Where("project_id = ? AND name = ? AND source = ?", projectID, name, source).Count(&count).Error
	return count, err
}

func (s tagStore) CountSource(projectID string, source projecttypes.TagSource) (int64, error) {
	var count int64
	err := s.tx.Model(&ProjectTag{}).Where("project_id = ? AND source = ?", projectID, source).Count(&count).Error
	return count, err
}

func (s tagStore) StoredColor(name string) (projecttypes.TagColor, bool, error) {
	var row ProjectTag
	err := s.tx.Where("name = ?", name).Order("source DESC, color").First(&row).Error
	switch {
	case err == nil:
		return projecttypes.TagColor(row.Color), true, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", false, nil
	default:
		return "", false, err
	}
}

func (s tagStore) Insert(rows []projecttypes.TagAssignment) error {
	tagRows := make([]ProjectTag, 0, len(rows))
	for _, row := range rows {
		tagRows = append(tagRows, ProjectTag{ProjectID: row.ProjectID, Name: row.Name, Source: string(row.Source), Color: string(row.Color)})
	}
	return s.tx.Create(tagRows).Error
}

func (s tagStore) InsertIgnoringConflict(row projecttypes.TagAssignment) error {
	tagRow := ProjectTag{ProjectID: row.ProjectID, Name: row.Name, Source: string(row.Source), Color: string(row.Color)}
	return s.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&tagRow).Error
}

func (s tagStore) DeleteTag(projectID, name string, source projecttypes.TagSource) error {
	return s.tx.Where("project_id = ? AND name = ? AND source = ?", projectID, name, source).Delete(&ProjectTag{}).Error
}

func (s tagStore) DeleteSource(projectID string, source projecttypes.TagSource) error {
	return s.tx.Where("project_id = ? AND source = ?", projectID, source).Delete(&ProjectTag{}).Error
}

package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/samber/mo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	buildtypes "kit.getarcane.app/builds/types"

	projectsync "getarcane.dev/arcane/v3/internal/project/children/sync"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/projects"
	"getarcane.dev/types/v3"
)

type (
	builder interface {
		BuildImage(ctx context.Context, environmentID string, req buildtypes.BuildRequest, progressWriter io.Writer, serviceName string, user *types.Actor) (*buildtypes.BuildResult, error)
		BuildSettings() buildtypes.BuildSettings
	}

	projectMetadataEnv struct {
		settings           *settings.Settings
		gitOpsComposePaths map[string]string
		pathMapper         func() *projects.PathMapper

		composeFiles      map[string]string
		projectsDirectory string

		composeFilesMu sync.Mutex
		autoInjectEnv  bool
	}

	composeNameCache struct {
		byName map[string]string
		mu     sync.RWMutex
	}

	projectCleanupDecision struct {
		reason  string
		project Project
	}

	tagStore struct {
		tx *gorm.DB
	}
)

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
	env.composeFiles[projectID] = path
	env.composeFilesMu.Unlock()
	return path, nil
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

func deleteProjectWithTags(tx *gorm.DB, projectID string) error {
	if err := tx.Where("project_id = ?", projectID).Delete(&ProjectTag{}).Error; err != nil {
		return fmt.Errorf("delete project tags: %w", err)
	}
	if err := tx.Where("mode = ? AND project_id = ?", types.GitOpsSyncModeBackup, projectID).Delete(&GitOpsSync{}).Error; err != nil {
		return fmt.Errorf("delete project git backups: %w", err)
	}
	if err := tx.Delete(&Project{}, "id = ?", projectID).Error; err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func projectRecord(p Project) types.ProjectRecord {
	return types.ProjectRecord{
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

func projectRecords(projectsList []Project) []types.ProjectRecord {
	records := make([]types.ProjectRecord, len(projectsList))
	for i, p := range projectsList {
		records[i] = projectRecord(p)
	}
	return records
}

func (s tagStore) CountTag(projectID, name string, source types.ProjectTagSource) (int64, error) {
	var count int64
	err := s.tx.Model(&ProjectTag{}).Where("project_id = ? AND name = ? AND source = ?", projectID, name, source).Count(&count).Error
	return count, err
}

func (s tagStore) CountSource(projectID string, source types.ProjectTagSource) (int64, error) {
	var count int64
	err := s.tx.Model(&ProjectTag{}).Where("project_id = ? AND source = ?", projectID, source).Count(&count).Error
	return count, err
}

func (s tagStore) StoredColor(name string) (types.ProjectTagColor, bool, error) {
	var row ProjectTag
	err := s.tx.Where("name = ?", name).Order("source DESC, color").First(&row).Error
	switch {
	case err == nil:
		return types.ProjectTagColor(row.Color), true, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", false, nil
	default:
		return "", false, err
	}
}

func (s tagStore) Insert(rows []types.ProjectTagAssignment) error {
	tagRows := make([]ProjectTag, 0, len(rows))
	for _, row := range rows {
		tagRows = append(tagRows, ProjectTag{ProjectID: row.ProjectID, Name: row.Name, Source: string(row.Source), Color: string(row.Color)})
	}
	return s.tx.Create(tagRows).Error
}

func (s tagStore) InsertIgnoringConflict(row types.ProjectTagAssignment) error {
	tagRow := ProjectTag{ProjectID: row.ProjectID, Name: row.Name, Source: string(row.Source), Color: string(row.Color)}
	return s.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&tagRow).Error
}

func (s tagStore) DeleteTag(projectID, name string, source types.ProjectTagSource) error {
	return s.tx.Where("project_id = ? AND name = ? AND source = ?", projectID, name, source).Delete(&ProjectTag{}).Error
}

func (s tagStore) DeleteSource(projectID string, source types.ProjectTagSource) error {
	return s.tx.Where("project_id = ? AND source = ?", projectID, source).Delete(&ProjectTag{}).Error
}

func projectEnvDetails(ctx context.Context, projectPath string) (string, bool, error) {
	envState, err := projects.ReadProjectEnvState(ctx, projectPath)
	if err != nil {
		return "", false, fmt.Errorf("failed to read project env state: %w", err)
	}
	content, err := projectsync.ResolveStoredEffectiveEnvContent(envState)
	return content, envState.HasEffective || envState.HasGitSource || envState.HasOverride, err
}

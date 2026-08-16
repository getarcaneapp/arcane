package project

import (
	"context"
	"sort"
	"strings"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	projectpkg "github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errComposeTagReadOnly = errors.New("tag is defined in Compose and can only be changed through x-arcane metadata")

// GetProjectTags returns the effective UI and Compose tag associations for a project.
func (s *ProjectService) GetProjectTags(ctx context.Context, projectID string) ([]projecttypes.Tag, error) {
	tagsByProject, err := s.loadProjectTagsInternal(ctx, []string{projectID})
	if err != nil {
		return nil, err
	}
	return tagsByProject[projectID], nil
}

// ListProjectTagOptions returns the distinct tag names and colors available in the current environment.
func (s *ProjectService) ListProjectTagOptions(ctx context.Context) ([]projecttypes.TagOption, error) {
	var rows []models.ProjectTag
	if err := s.db.WithContext(ctx).Order("name, source DESC, color").Find(&rows).Error; err != nil {
		return nil, errors.WrapIf(err, "list project tag options")
	}
	options := make([]projecttypes.TagOption, 0)
	for _, row := range rows {
		if len(options) > 0 && options[len(options)-1].Name == row.Name {
			continue
		}
		options = append(options, projecttypes.TagOption{Name: row.Name, Color: projecttypes.TagColor(row.Color)})
	}
	return options, nil
}

// UpdateProjectTag attaches or detaches a UI-managed tag and rejects Compose-owned names.
func (s *ProjectService) UpdateProjectTag(ctx context.Context, projectID, name string, color projecttypes.TagColor, attached bool, user models.User) ([]projecttypes.Tag, error) {
	normalized, err := projectpkg.NormalizeProjectTag(name)
	if err != nil {
		return nil, err
	}
	normalizedColor := projecttypes.TagColorGray
	if attached {
		normalizedColor, err = projectpkg.NormalizeProjectTagColor(color)
		if err != nil {
			return nil, err
		}
	}

	var projectModel models.Project
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&projectModel, "id = ?", projectID).Error; err != nil {
			return errors.WrapIf(err, "find project for tag update")
		}

		var composeCount int64
		if err := tx.Model(&models.ProjectTag{}).
			Where("project_id = ? AND name = ? AND source = ?", projectID, normalized, projecttypes.TagSourceCompose).
			Count(&composeCount).Error; err != nil {
			return errors.WrapIf(err, "check Compose tag source")
		}
		if composeCount > 0 {
			return errComposeTagReadOnly
		}

		if attached {
			var existing int64
			if err := tx.Model(&models.ProjectTag{}).
				Where("project_id = ? AND name = ? AND source = ?", projectID, normalized, projecttypes.TagSourceUI).
				Count(&existing).Error; err != nil {
				return errors.WrapIf(err, "check UI tag source")
			}
			if existing > 0 {
				return nil
			}
			var count int64
			if err := tx.Model(&models.ProjectTag{}).
				Where("project_id = ? AND source = ?", projectID, projecttypes.TagSourceUI).
				Count(&count).Error; err != nil {
				return errors.WrapIf(err, "count UI project tags")
			}
			if count >= projectpkg.ProjectTagsPerSourceLimit {
				return errors.Errorf("a project cannot have more than %d UI tags", projectpkg.ProjectTagsPerSourceLimit)
			}
			resolvedColor, err := resolveProjectTagColorInternal(tx, normalized, normalizedColor)
			if err != nil {
				return err
			}
			row := models.ProjectTag{ProjectID: projectID, Name: normalized, Source: string(projecttypes.TagSourceUI), Color: string(resolvedColor)}
			return errors.WrapIf(tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error, "attach UI project tag")
		}

		return errors.WrapIf(tx.Where("project_id = ? AND name = ? AND source = ?", projectID, normalized, projecttypes.TagSourceUI).
			Delete(&models.ProjectTag{}).Error, "detach UI project tag")
	})
	if err != nil {
		return nil, err
	}

	metadata := models.JSON{"action": "update_tags", "projectID": projectID, "projectName": projectModel.Name, "tag": normalized, "attached": attached}
	s.logProjectEventInternal(ctx, models.EventTypeProjectUpdate, projectID, projectModel.Name, user, metadata, "could not log project tag update")
	return s.GetProjectTags(ctx, projectID)
}

func (s *ProjectService) reconcileComposeProjectTagsInternal(ctx context.Context, projectID string, tags []projecttypes.TagOption) error {
	normalized, err := normalizeComposeProjectTagsInternal(tags)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var projectModel models.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&projectModel, "id = ?", projectID).Error; err != nil {
			return errors.WrapIf(err, "find project for Compose tag reconciliation")
		}
		if err := tx.Where("project_id = ? AND source = ?", projectID, projecttypes.TagSourceCompose).Delete(&models.ProjectTag{}).Error; err != nil {
			return errors.WrapIf(err, "clear Compose project tags")
		}
		if len(normalized) == 0 {
			return nil
		}
		rows := make([]models.ProjectTag, 0, len(normalized))
		for _, tag := range normalized {
			rows = append(rows, models.ProjectTag{ProjectID: projectID, Name: tag.Name, Source: string(projecttypes.TagSourceCompose), Color: string(tag.Color)})
		}
		return errors.WrapIf(tx.Create(&rows).Error, "replace Compose project tags")
	})
}

func normalizeComposeProjectTagsInternal(tags []projecttypes.TagOption) ([]projecttypes.TagOption, error) {
	result := make([]projecttypes.TagOption, 0, min(len(tags), projectpkg.ProjectTagsPerSourceLimit))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		name, err := projectpkg.NormalizeProjectTag(tag.Name)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(tag.Color)) == "" {
			return nil, errors.New("compose tag color is required")
		}
		color, err := projectpkg.NormalizeProjectTagColor(tag.Color)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			continue
		}
		if len(result) >= projectpkg.ProjectTagsPerSourceLimit {
			return nil, errors.Errorf("a project cannot have more than %d compose tags", projectpkg.ProjectTagsPerSourceLimit)
		}
		seen[name] = struct{}{}
		result = append(result, projecttypes.TagOption{Name: name, Color: color})
	}
	return result, nil
}

func createUIProjectTagsInternal(tx *gorm.DB, projectID string, tags []string, colors map[string]projecttypes.TagColor) error {
	if len(tags) == 0 {
		return nil
	}
	rows := make([]models.ProjectTag, 0, len(tags))
	for _, tag := range tags {
		color, err := resolveProjectTagColorInternal(tx, tag, colors[tag])
		if err != nil {
			return err
		}
		rows = append(rows, models.ProjectTag{ProjectID: projectID, Name: tag, Source: string(projecttypes.TagSourceUI), Color: string(color)})
	}
	return errors.WrapIf(tx.Create(&rows).Error, "attach initial project tags")
}

func resolveProjectTagColorInternal(tx *gorm.DB, name string, fallback projecttypes.TagColor) (projecttypes.TagColor, error) {
	var row models.ProjectTag
	err := tx.Where("name = ?", name).Order("source DESC, color").First(&row).Error
	switch {
	case err == nil:
		return projecttypes.TagColor(row.Color), nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return projectpkg.NormalizeProjectTagColor(fallback)
	default:
		return "", errors.WrapIf(err, "resolve project tag color")
	}
}

func normalizeProjectTagColorsInternal(colors map[string]projecttypes.TagColor) (map[string]projecttypes.TagColor, error) {
	result := make(map[string]projecttypes.TagColor, len(colors))
	for name, color := range colors {
		normalizedName, err := projectpkg.NormalizeProjectTag(name)
		if err != nil {
			return nil, err
		}
		normalizedColor, err := projectpkg.NormalizeProjectTagColor(color)
		if err != nil {
			return nil, err
		}
		result[normalizedName] = normalizedColor
	}
	return result, nil
}

func excludeComposeOwnedUITagsInternal(uiTags []string, composeTags []projecttypes.TagOption) []string {
	composeOwned := make(map[string]struct{}, len(composeTags))
	for _, tag := range composeTags {
		composeOwned[tag.Name] = struct{}{}
	}
	result := make([]string, 0, len(uiTags))
	for _, tag := range uiTags {
		if _, exists := composeOwned[tag]; !exists {
			result = append(result, tag)
		}
	}
	return result
}

func deleteProjectWithTagsInternal(tx *gorm.DB, projectID string) error {
	if err := tx.Where("project_id = ?", projectID).Delete(&models.ProjectTag{}).Error; err != nil {
		return errors.WrapIf(err, "delete project tags")
	}
	return errors.WrapIf(tx.Delete(&models.Project{}, "id = ?", projectID).Error, "delete project")
}

func (s *ProjectService) reconcileComposeTagsForProjectInternal(ctx context.Context, projectModel *models.Project) error {
	if projectModel == nil {
		return nil
	}
	composeFile, err := s.ResolveProjectComposeFile(ctx, projectModel)
	if err != nil {
		return errors.WrapIf(err, "resolve Compose file for tag reconciliation")
	}
	projectsDirectory, err := s.GetProjectsDirectory(ctx)
	if err != nil {
		return err
	}
	meta, err := projectpkg.ParseArcaneComposeMetadata(ctx, composeFile, projectsDirectory, s.settingsService.GetBoolSetting(ctx, "autoInjectEnv", false))
	if err != nil {
		return err
	}
	if !meta.ProjectTagsAuthoritative {
		return nil
	}
	return s.reconcileComposeProjectTagsInternal(ctx, projectModel.ID, meta.ProjectTags)
}

func (s *ProjectService) loadProjectTagsInternal(ctx context.Context, projectIDs []string) (map[string][]projecttypes.Tag, error) {
	result := make(map[string][]projecttypes.Tag, len(projectIDs))
	if len(projectIDs) == 0 {
		return result, nil
	}
	var rows []models.ProjectTag
	if err := s.db.WithContext(ctx).Where("project_id IN ?", projectIDs).Order("project_id, name, source, color").Find(&rows).Error; err != nil {
		return nil, errors.WrapIf(err, "load project tags")
	}
	for _, row := range rows {
		tags := result[row.ProjectID]
		if len(tags) == 0 || tags[len(tags)-1].Name != row.Name {
			tags = append(tags, projecttypes.Tag{Name: row.Name, Color: projecttypes.TagColor(row.Color)})
		}
		tags[len(tags)-1].Sources = append(tags[len(tags)-1].Sources, projecttypes.TagSource(row.Source))
		result[row.ProjectID] = tags
	}
	for projectID := range result {
		sort.SliceStable(result[projectID], func(i, j int) bool { return result[projectID][i].Name < result[projectID][j].Name })
		for index := range result[projectID] {
			sort.SliceStable(result[projectID][index].Sources, func(i, j int) bool {
				return result[projectID][index].Sources[i] == projecttypes.TagSourceUI && result[projectID][index].Sources[j] != projecttypes.TagSourceUI
			})
		}
	}
	return result, nil
}

func (s *ProjectService) enrichProjectsWithTagsInternal(ctx context.Context, items []projecttypes.Details) error {
	projectIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" && !item.IsDiscovered && !strings.HasPrefix(item.ID, "compose:") {
			projectIDs = append(projectIDs, item.ID)
		}
	}
	tagsByProject, err := s.loadProjectTagsInternal(ctx, projectIDs)
	if err != nil {
		return err
	}
	for index := range items {
		items[index].Tags = tagsByProject[items[index].ID]
	}
	return nil
}

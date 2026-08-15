package project

import (
	"context"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/require"
)

func TestProjectTags_ReconcilePreservesUIAndComposeOwnershipIsReadOnly(t *testing.T) {
	db := setupProjectTestDB(t)
	service := &ProjectService{db: db}
	ctx := context.Background()
	projectModel := models.Project{
		BaseModel:       models.BaseModel{ID: "project-tags"},
		Name:            "tagged",
		Path:            "/tmp/tagged",
		Status:          models.ProjectStatusStopped,
		IsArchived:      true,
		GitOpsManagedBy: new("gitops-1"),
	}
	require.NoError(t, db.Create(&projectModel).Error)

	tags, err := service.UpdateProjectTag(ctx, projectModel.ID, " Database ", projecttypes.TagColorPurple, true, models.User{})
	require.NoError(t, err)
	require.Equal(t, []projecttypes.Tag{{Name: "database", Color: projecttypes.TagColorPurple, Sources: []projecttypes.TagSource{projecttypes.TagSourceUI}}}, tags)

	require.NoError(t, service.reconcileComposeProjectTagsInternal(ctx, projectModel.ID, []projecttypes.TagOption{
		{Name: "DATABASE", Color: projecttypes.TagColorBlue},
		{Name: "maintenance-window", Color: projecttypes.TagColorOrange},
	}))
	tags, err = service.GetProjectTags(ctx, projectModel.ID)
	require.NoError(t, err)
	require.Equal(t, []projecttypes.Tag{
		{Name: "database", Color: projecttypes.TagColorBlue, Sources: []projecttypes.TagSource{projecttypes.TagSourceUI, projecttypes.TagSourceCompose}},
		{Name: "maintenance-window", Color: projecttypes.TagColorOrange, Sources: []projecttypes.TagSource{projecttypes.TagSourceCompose}},
	}, tags)

	_, err = service.UpdateProjectTag(ctx, projectModel.ID, "database", "", false, models.User{})
	require.ErrorIs(t, err, errComposeTagReadOnly)
	_, err = service.UpdateProjectTag(ctx, projectModel.ID, "maintenance-window", projecttypes.TagColorBlue, true, models.User{})
	require.ErrorIs(t, err, errComposeTagReadOnly)

	require.NoError(t, service.reconcileComposeProjectTagsInternal(ctx, projectModel.ID, nil))
	tags, err = service.UpdateProjectTag(ctx, projectModel.ID, "database", "", false, models.User{})
	require.NoError(t, err)
	require.Empty(t, tags)
}

func TestProjectTags_FilterUsesExactORNames(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	projects := []models.Project{
		{BaseModel: models.BaseModel{ID: "one"}, Name: "one", Path: "/tmp/one", Status: models.ProjectStatusStopped},
		{BaseModel: models.BaseModel{ID: "two"}, Name: "two", Path: "/tmp/two", Status: models.ProjectStatusStopped},
		{BaseModel: models.BaseModel{ID: "three"}, Name: "three", Path: "/tmp/three", Status: models.ProjectStatusStopped},
	}
	require.NoError(t, db.Create(&projects).Error)
	require.NoError(t, db.Create(&[]models.ProjectTag{
		{ProjectID: "one", Name: "database", Source: string(projecttypes.TagSourceUI)},
		{ProjectID: "two", Name: "maintenance", Source: string(projecttypes.TagSourceCompose)},
		{ProjectID: "three", Name: "database-prod", Source: string(projecttypes.TagSourceUI)},
	}).Error)

	var result []models.Project
	err := applyProjectTagsDBFilterInternal(db.WithContext(ctx).Model(&models.Project{}), " DATABASE,maintenance ").Order("id").Find(&result).Error
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.ElementsMatch(t, []string{"one", "two"}, []string{result[0].ID, result[1].ID})
	require.NotContains(t, []string{result[0].ID, result[1].ID}, "three")

	result = nil
	err = applyProjectSearchDBFilterInternal(db.WithContext(ctx).Model(&models.Project{}), "MAINT").Find(&result).Error
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "two", result[0].ID)
}

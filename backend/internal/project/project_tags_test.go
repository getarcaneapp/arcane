package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"testing"

	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	"github.com/stretchr/testify/require"
)

func TestProjectTags_ReconcilePreservesUIAndComposeOwnershipIsReadOnly(t *testing.T) {
	db := setupProjectTestDB(t)
	service := &ProjectService{db: db}
	ctx := context.Background()
	projectModel := Project{
		BaseModel:       database.BaseModel{ID: "project-tags"},
		Name:            "tagged",
		Path:            "/tmp/tagged",
		Status:          ProjectStatusStopped,
		IsArchived:      true,
		GitOpsManagedBy: new("gitops-1"),
	}
	require.NoError(t, db.Create(&projectModel).Error)

	tags, err := service.UpdateProjectTag(ctx, projectModel.ID, " Database ", projecttypes.TagColorPurple, true, common.User{})
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

	_, err = service.UpdateProjectTag(ctx, projectModel.ID, "database", "", false, common.User{})
	require.ErrorIs(t, err, errComposeTagReadOnly)
	_, err = service.UpdateProjectTag(ctx, projectModel.ID, "maintenance-window", projecttypes.TagColorBlue, true, common.User{})
	require.ErrorIs(t, err, errComposeTagReadOnly)

	require.NoError(t, service.reconcileComposeProjectTagsInternal(ctx, projectModel.ID, nil))
	tags, err = service.UpdateProjectTag(ctx, projectModel.ID, "database", "", false, common.User{})
	require.NoError(t, err)
	require.Empty(t, tags)
}

func TestProjectTags_FilterUsesExactORNames(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()
	projects := []Project{
		{BaseModel: database.BaseModel{ID: "one"}, Name: "one", Path: "/tmp/one", Status: ProjectStatusStopped},
		{BaseModel: database.BaseModel{ID: "two"}, Name: "two", Path: "/tmp/two", Status: ProjectStatusStopped},
		{BaseModel: database.BaseModel{ID: "three"}, Name: "three", Path: "/tmp/three", Status: ProjectStatusStopped},
	}
	require.NoError(t, db.Create(&projects).Error)
	require.NoError(t, db.Create(&[]ProjectTag{
		{ProjectID: "one", Name: "database", Source: string(projecttypes.TagSourceUI)},
		{ProjectID: "two", Name: "maintenance", Source: string(projecttypes.TagSourceCompose)},
		{ProjectID: "three", Name: "database-prod", Source: string(projecttypes.TagSourceUI)},
	}).Error)

	var result []Project
	err := applyProjectTagsDBFilterInternal(db.WithContext(ctx).Model(&Project{}), " DATABASE,maintenance ").Order("id").Find(&result).Error
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.ElementsMatch(t, []string{"one", "two"}, []string{result[0].ID, result[1].ID})
	require.NotContains(t, []string{result[0].ID, result[1].ID}, "three")

	result = nil
	err = applyProjectSearchDBFilterInternal(db.WithContext(ctx).Model(&Project{}), "MAINT").Find(&result).Error
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "two", result[0].ID)
}

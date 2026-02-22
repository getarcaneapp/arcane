package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupEventServiceTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Event{}))
	return &database.DB{DB: db}
}

func TestCreateEventRequestJSONOmitempty(t *testing.T) {
	t.Run("includes optional fields when set", func(t *testing.T) {
		payload, err := json.Marshal(CreateEventRequest{
			Type:         models.EventTypeContainerStart,
			Title:        "Container started",
			Description:  "Container 'web' has been started",
			ResourceType: new("container"),
			ResourceID:   new("container-1"),
			ResourceName: new("web"),
			UserID:       new("user-1"),
			Username:     new("arcane"),
		})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(payload, &decoded))

		require.Equal(t, "container", decoded["resourceType"])
		require.Equal(t, "container-1", decoded["resourceId"])
		require.Equal(t, "web", decoded["resourceName"])
		require.Equal(t, "user-1", decoded["userId"])
		require.Equal(t, "arcane", decoded["username"])
	})

	t.Run("omits optional fields when nil", func(t *testing.T) {
		payload, err := json.Marshal(CreateEventRequest{
			Type:  models.EventTypeUserLogin,
			Title: "User logged in",
		})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(payload, &decoded))

		_, hasResourceType := decoded["resourceType"]
		_, hasResourceID := decoded["resourceId"]
		_, hasResourceName := decoded["resourceName"]
		_, hasUserID := decoded["userId"]
		_, hasUsername := decoded["username"]

		require.False(t, hasResourceType)
		require.False(t, hasResourceID)
		require.False(t, hasResourceName)
		require.False(t, hasUserID)
		require.False(t, hasUsername)
	})
}

func TestEventService_LogEventsPersistOptionalPointers(t *testing.T) {
	ctx := context.Background()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db)

	metadata := models.JSON{"source": "test"}
	err := svc.LogContainerEvent(ctx, models.EventTypeContainerStart, "container-1", "web", "user-1", "arcane", "0", metadata)
	require.NoError(t, err)

	var containerEvent models.Event
	err = db.WithContext(ctx).Where("type = ?", models.EventTypeContainerStart).First(&containerEvent).Error
	require.NoError(t, err)

	require.NotNil(t, containerEvent.ResourceType)
	require.Equal(t, "container", *containerEvent.ResourceType)
	require.NotNil(t, containerEvent.ResourceID)
	require.Equal(t, "container-1", *containerEvent.ResourceID)
	require.NotNil(t, containerEvent.ResourceName)
	require.Equal(t, "web", *containerEvent.ResourceName)
	require.NotNil(t, containerEvent.UserID)
	require.Equal(t, "user-1", *containerEvent.UserID)
	require.NotNil(t, containerEvent.Username)
	require.Equal(t, "arcane", *containerEvent.Username)
	require.NotNil(t, containerEvent.EnvironmentID)
	require.Equal(t, "0", *containerEvent.EnvironmentID)
	require.Equal(t, "test", containerEvent.Metadata["source"])

	err = svc.LogUserEvent(ctx, models.EventTypeUserLogin, "user-2", "arcane-user", nil)
	require.NoError(t, err)

	var userEvent models.Event
	err = db.WithContext(ctx).Where("type = ?", models.EventTypeUserLogin).First(&userEvent).Error
	require.NoError(t, err)

	require.Nil(t, userEvent.ResourceType)
	require.Nil(t, userEvent.ResourceID)
	require.Nil(t, userEvent.ResourceName)
	require.NotNil(t, userEvent.UserID)
	require.Equal(t, "user-2", *userEvent.UserID)
	require.NotNil(t, userEvent.Username)
	require.Equal(t, "arcane-user", *userEvent.Username)
}

func TestCloneEventMetadataInternal(t *testing.T) {
	src := models.JSON{"a": "b"}
	cloned := cloneEventMetadataInternal(src)

	require.Equal(t, "b", cloned["a"])
	cloned["a"] = "changed"
	require.Equal(t, "b", src["a"], "clone should not mutate source metadata")

	nilClone := cloneEventMetadataInternal(nil)
	require.NotNil(t, nilClone)
	require.Empty(t, nilClone)

	nested := models.JSON{
		"outer": map[string]any{
			"slice": []any{
				map[string]any{"k": "v"},
			},
		},
	}
	nestedClone := cloneEventMetadataInternal(nested)
	require.NotNil(t, nestedClone["outer"])

	outer := nested["outer"].(map[string]any)
	outerClone := nestedClone["outer"].(models.JSON)
	sliceOriginal := outer["slice"].([]any)
	sliceClone := outerClone["slice"].([]any)

	sliceClone[0].(models.JSON)["k"] = "changed"
	require.Equal(t, "v", sliceOriginal[0].(map[string]any)["k"], "nested map inside slice should be deep-cloned")
}

func TestEventService_LogErrorEvent_DoesNotMutateInputMetadata(t *testing.T) {
	ctx := context.Background()
	db := setupEventServiceTestDB(t)
	svc := NewEventService(db)

	metadata := models.JSON{"phase": "pull"}
	svc.LogErrorEvent(
		ctx,
		models.EventTypeImageScan,
		"image",
		"img-1",
		"nginx:latest",
		"user-1",
		"arcane",
		"0",
		errors.New("pull failed"),
		metadata,
	)

	_, mutated := metadata["error"]
	require.False(t, mutated, "input metadata should not be mutated by LogErrorEvent")

	var saved models.Event
	err := db.WithContext(ctx).Where("type = ?", models.EventTypeImageScan).First(&saved).Error
	require.NoError(t, err)
	require.Equal(t, models.EventSeverityError, saved.Severity)
	require.Equal(t, "pull", saved.Metadata["phase"])
	require.Equal(t, "pull failed", saved.Metadata["error"])
}

package activity

import (
	"context"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/stretchr/testify/require"
)

func TestJobActivityVisibilityFiltersBeforePagination(t *testing.T) {
	db := setupActivityServiceTestDBInternal(t)
	service := NewActivityService(db, nil)
	for _, target := range []string{"0", "allowed", "private"} {
		require.NoError(t, db.Create(&Activity{BaseModel: database.BaseModel{ID: target}, EnvironmentID: "0", Type: activitytypes.TypeJobRun, Status: activitytypes.StatusQueued, StartedAt: time.Now(), Metadata: database.JSON{"environmentId": target}}).Error)
	}
	for _, metadata := range []database.JSON{nil, {"environmentId": 0}} {
		require.NoError(t, db.Create(&Activity{EnvironmentID: "0", Type: activitytypes.TypeJobRun, Status: activitytypes.StatusQueued, StartedAt: time.Now(), Metadata: metadata}).Error)
	}
	permissions := authz.NewPermissionSet()
	permissions.PerEnv["0"] = map[string]struct{}{authz.PermActivitiesRead: {}}
	permissions.PerEnv["allowed"] = map[string]struct{}{authz.PermActivitiesRead: {}}
	ctx := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, permissions)
	seen := make(map[string]bool)
	for page := range 2 {
		activities, response, err := service.ListActivitiesPaginated(ctx, "0", pagination.QueryParams{Start: page, Limit: 1})
		require.NoError(t, err)
		require.EqualValues(t, 2, response.TotalItems)
		require.Len(t, activities, 1)
		require.NotEqual(t, "private", activities[0].ID)
		require.True(t, canReadJobActivityInternal(ctx, activities[0]))
		seen[activities[0].ID] = true
	}
	require.Len(t, seen, 2)
	require.False(t, canReadJobActivityInternal(ctx, activitytypes.Activity{Type: activitytypes.TypeJobRun, EnvironmentID: "0", Metadata: map[string]any{"environmentId": "private"}}))
	privileged := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.SudoPermissionSet())
	activities, response, err := service.ListActivitiesPaginated(privileged, "0", pagination.QueryParams{Limit: 10})
	require.NoError(t, err)
	require.Len(t, activities, 3)
	require.EqualValues(t, 3, response.TotalItems)
	require.False(t, canReadJobActivityInternal(privileged, activitytypes.Activity{Type: activitytypes.TypeJobRun, EnvironmentID: "0"}))
	require.NoError(t, db.Model(&Activity{}).Where("type = ?", activitytypes.TypeJobRun).Update("status", activitytypes.StatusSuccess).Error)
	permissions.PerEnv["0"][authz.PermActivitiesDelete] = struct{}{}
	deleted, err := service.DeleteHistory(ctx, "0")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	var retained Activity
	require.NoError(t, db.First(&retained, "id = ?", "private").Error)
	require.NoError(t, db.First(&Activity{}, "id = ?", "allowed").Error)

}

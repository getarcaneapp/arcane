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
	for _, environmentID := range []string{"0", "private"} {
		require.NoError(t, db.Create(&Activity{ID: environmentID + "-job", EnvironmentID: environmentID, Type: activitytypes.TypeJobRun, Status: activitytypes.StatusSuccess, StartedAt: time.Now(), Metadata: database.JSON{"environmentId": environmentID}}).Error)
		require.NoError(t, db.Create(&Activity{ID: environmentID + "-pull", EnvironmentID: environmentID, Type: activitytypes.TypeImagePull, Status: activitytypes.StatusSuccess, StartedAt: time.Now()}).Error)
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
		require.True(t, canReadJobActivityInternal(ctx, activities[0]))
		seen[activities[0].ID] = true
	}
	require.Len(t, seen, 2)
	activities, response, err := service.ListActivitiesPaginated(ctx, "private", pagination.QueryParams{Limit: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, response.TotalItems)
	require.Len(t, activities, 1)
	require.Equal(t, "private-pull", activities[0].ID)
	require.False(t, canReadJobActivityInternal(ctx, activitytypes.Activity{Type: activitytypes.TypeJobRun, EnvironmentID: "private", Metadata: map[string]any{"environmentId": "allowed"}}))
	require.True(t, canReadJobActivityInternal(ctx, activitytypes.Activity{Type: activitytypes.TypeJobRun, Metadata: map[string]any{"environmentId": "allowed"}}))
	privileged := context.WithValue(t.Context(), middleware.ContextKeyUserPermissions, authz.SudoPermissionSet())
	activities, response, err = service.ListActivitiesPaginated(privileged, "private", pagination.QueryParams{Limit: 10})
	require.NoError(t, err)
	require.Len(t, activities, 2)
	require.EqualValues(t, 2, response.TotalItems)
	require.False(t, canReadJobActivityInternal(privileged, activitytypes.Activity{Type: activitytypes.TypeJobRun}))
	permissions.PerEnv["0"][authz.PermActivitiesDelete] = struct{}{}
	deleted, err := service.DeleteHistory(ctx, "private")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.NoError(t, db.First(&Activity{}, "id = ?", "private-job").Error)
	deleted, err = service.DeleteHistory(ctx, "0")
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)
}

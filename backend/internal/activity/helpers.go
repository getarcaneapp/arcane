package activity

import (
	"cmp"
	"context"

	"github.com/getarcaneapp/arcane/types/v3"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
)

// Job summaries live in their target environment's environment_id column.
func scopeJobActivityVisibility(ctx context.Context, query *gorm.DB, environmentID, permission string) *gorm.DB {
	permissions, present := middleware.PermissionsFromContext(ctx)
	if !present || permissions.Allows(permission, environmentID) {
		return query
	}
	return query.Where("type <> ?", types.ActivityTypeJobRun)
}

func canReadJobActivity(ctx context.Context, item types.Activity) bool {
	if item.Type != types.ActivityTypeJobRun {
		return true
	}
	metadataEnvironmentID, _ := item.Metadata["environmentId"].(string)
	environmentID := cmp.Or(item.EnvironmentID, metadataEnvironmentID)
	if environmentID == "" {
		return false
	}
	permissions, _ := middleware.PermissionsFromContext(ctx)
	return permissions.Allows(authz.PermActivitiesRead, environmentID)
}

func (h *ActivityHandler) canReadActivityStreamEvent(ctx context.Context, event types.ActivityStreamEvent) bool {
	if event.Activity != nil {
		return canReadJobActivity(ctx, *event.Activity)
	}
	if event.ActivityID == "" {
		return true
	}
	// Only job activities are scoped by target environment. Events that carry a
	// type hint for any other activity type need no database reread; job
	// metadata is mutable, so job events are still checked against the row.
	if event.ActivityType != "" && event.ActivityType != types.ActivityTypeJobRun {
		return true
	}
	var model Activity
	// Message events carry no activity metadata. Check the owning row before output.
	if err := h.activityService.db.WithContext(ctx).Select("type", "environment_id", "metadata").Where("id = ? AND environment_id = ?", event.ActivityID, "0").First(&model).Error; err != nil {
		return false
	}
	return canReadJobActivity(ctx, types.Activity{Type: model.Type, EnvironmentID: model.EnvironmentID, Metadata: model.Metadata})
}

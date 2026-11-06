package activity

import (
	"cmp"
	"context"

	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

// Job summaries live in their target environment's environment_id column.
func scopeJobActivityVisibility(ctx context.Context, query *gorm.DB, environmentID, permission string) *gorm.DB {
	permissions, present := middleware.PermissionsFromContext(ctx)
	if !present || permissions.Allows(permission, environmentID) {
		return query
	}
	return query.Where("type <> ?", types.ActivityTypeJobRun)
}

func canReadJobActivity(ctx context.Context, item types.Activity, permission string) bool {
	if item.Type != types.ActivityTypeJobRun {
		return true
	}
	metadataEnvironmentID, _ := item.Metadata["environmentId"].(string)
	environmentID := cmp.Or(item.EnvironmentID, metadataEnvironmentID)
	if environmentID == "" {
		return false
	}
	permissions, _ := middleware.PermissionsFromContext(ctx)
	return permissions.Allows(permission, environmentID)
}

func (h *ActivityHandler) canReadActivityStreamEvent(ctx context.Context, event types.ActivityStreamEvent) bool {
	if event.Activity != nil {
		return canReadJobActivity(ctx, *event.Activity, authz.PermActivitiesList)
	}
	if event.ActivityID == "" {
		return true
	}
	if event.ActivityType != "" && event.ActivityType != types.ActivityTypeJobRun {
		return true
	}
	var model Activity
	// Message events carry no activity metadata. Check the owning row before output.
	if err := h.activityService.db.WithContext(ctx).Select("type", "environment_id", "metadata").Where("id = ? AND environment_id = ?", event.ActivityID, "0").First(&model).Error; err != nil {
		return false
	}
	return canReadJobActivity(ctx, types.Activity{Type: model.Type, EnvironmentID: model.EnvironmentID, Metadata: model.Metadata}, authz.PermActivitiesList)
}

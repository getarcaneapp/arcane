package activity

import (
	"context"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"gorm.io/gorm"
)

// Job summaries are stored on the manager but belong to their execution environment.
func scopeJobActivityVisibilityInternal(ctx context.Context, query *gorm.DB, permission string) *gorm.DB {
	permissions, present := middleware.PermissionsFromContext(ctx)
	if !present {
		return query
	}
	target := "json_extract(metadata, '$.environmentId')"
	valid := "json_type(metadata, '$.environmentId') = 'text' AND " + target + " <> ''"
	if query.Name() == "postgres" {
		target = "CAST(metadata AS jsonb)->>'environmentId'"
		valid = "jsonb_typeof(CAST(metadata AS jsonb)->'environmentId') = 'string' AND " + target + " <> ''"
	}
	if permissions.Allows(permission, "") {
		return query.Where("(type <> ? OR ("+valid+"))", activitytypes.TypeJobRun)
	}
	if permissions == nil {
		return query.Where("type <> ?", activitytypes.TypeJobRun)
	}
	allowed := make([]string, 0, len(permissions.PerEnv))
	for environmentID := range permissions.PerEnv {
		if permissions.Allows(permission, environmentID) {
			allowed = append(allowed, environmentID)
		}
	}
	if len(allowed) == 0 {
		return query.Where("type <> ?", activitytypes.TypeJobRun)
	}
	return query.Where("(type <> ? OR ("+valid+" AND "+target+" IN ?))", activitytypes.TypeJobRun, allowed)
}

func canReadJobActivityInternal(ctx context.Context, item activitytypes.Activity) bool {
	if item.Type != activitytypes.TypeJobRun {
		return true
	}
	environmentID, _ := item.Metadata["environmentId"].(string)
	if environmentID == "" {
		return false
	}
	permissions, _ := middleware.PermissionsFromContext(ctx)
	return permissions.Allows(authz.PermActivitiesRead, environmentID)
}

func (h *ActivityHandler) canReadActivityStreamEventInternal(ctx context.Context, event activitytypes.StreamEvent) bool {
	if event.Activity != nil {
		return canReadJobActivityInternal(ctx, *event.Activity)
	}
	if event.ActivityID == "" {
		return true
	}
	var model Activity
	// Message events carry no activity metadata. Check the owning row before output.
	if err := h.activityService.db.WithContext(ctx).Select("type", "environment_id", "metadata").Where("id = ? AND environment_id = ?", event.ActivityID, "0").First(&model).Error; err != nil {
		return false
	}
	return canReadJobActivityInternal(ctx, activitytypes.Activity{Type: model.Type, EnvironmentID: model.EnvironmentID, Metadata: model.Metadata})
}

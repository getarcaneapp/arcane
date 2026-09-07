package activity

import (
	"cmp"
	"context"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/samber/lo"
)

// ListRemoteActivities combines manager-owned summaries with agent-owned work
// before slicing the page. The remote records must already match the filters.
func (s *ActivityService) ListRemoteActivities(ctx context.Context, environmentID string, remote []activitytypes.Activity, params pagination.QueryParams) ([]activitytypes.Activity, pagination.Response, error) {
	all := params
	all.Start, all.Limit = 0, -1
	activities, _, err := s.ListActivitiesPaginated(ctx, environmentID, all)
	if err != nil {
		return nil, pagination.Response{}, err
	}
	for _, item := range remote {
		item.EnvironmentID = environmentID
		activities = append(activities, item)
	}
	params.Start = max(params.Start, 0)
	if params.Limit != -1 {
		if params.Limit <= 0 {
			params.Limit = 20
		}
		params.Limit = min(params.Limit, 100)
	}
	total := int64(len(activities))
	config := pagination.Config[activitytypes.Activity]{SortBindings: []pagination.SortBinding[activitytypes.Activity]{{
		Fn: func(a, b activitytypes.Activity) int { return compareActivitiesInternal(a, b, params) },
	}}}
	pageParams := params
	pageParams.Order = pagination.SortAsc
	activities = config.OrderAndPaginate(activities, pageParams)
	return activities, pagination.BuildResponse(total, 0, params), nil
}

func compareActivitiesInternal(a, b activitytypes.Activity, params pagination.QueryParams) int {
	if params.Sort == "" {
		aActive := a.Status == activitytypes.StatusQueued || a.Status == activitytypes.StatusRunning
		bActive := b.Status == activitytypes.StatusQueued || b.Status == activitytypes.StatusRunning
		if aActive != bActive {
			if aActive {
				return -1
			}
			return 1
		}
		aTime, bTime := a.CreatedAt, b.CreatedAt
		if a.EndedAt != nil {
			aTime = *a.EndedAt
		}
		if b.EndedAt != nil {
			bTime = *b.EndedAt
		}
		return cmp.Or(bTime.Compare(aTime), strings.Compare(b.ID, a.ID))
	}

	var result int
	switch params.Sort {
	case "environmentId":
		result = strings.Compare(a.EnvironmentID, b.EnvironmentID)
	case "type":
		result = cmp.Compare(a.Type, b.Type)
	case "status":
		result = cmp.Compare(a.Status, b.Status)
	case "resourceType":
		result = strings.Compare(lo.FromPtr(a.ResourceType), lo.FromPtr(b.ResourceType))
	case "resourceName":
		result = strings.Compare(lo.FromPtr(a.ResourceName), lo.FromPtr(b.ResourceName))
	case "startedAt":
		result = a.StartedAt.Compare(b.StartedAt)
	case "createdAt":
		result = a.CreatedAt.Compare(b.CreatedAt)
	case "updatedAt":
		result = lo.FromPtr(a.UpdatedAt).Compare(lo.FromPtr(b.UpdatedAt))
	case "endedAt":
		result = lo.FromPtr(a.EndedAt).Compare(lo.FromPtr(b.EndedAt))
	case "durationMs":
		result = cmp.Compare(lo.FromPtr(a.DurationMs), lo.FromPtr(b.DurationMs))
	}
	if params.Order == pagination.SortDesc {
		result = -result
	}
	return cmp.Or(result, strings.Compare(a.ID, b.ID))
}

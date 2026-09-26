package activity

import (
	"cmp"
	"context"
	"strings"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/samber/lo"
)

// remoteActivityPageCeiling mirrors the agent's page clamp in PaginateAndSortDB.
const remoteActivityPageCeiling = 100

// normalizeRemoteActivityParamsInternal applies the agent's paging bounds so both
// sources are cut the same way; -1 stays the explicit "show all" sentinel.
func normalizeRemoteActivityParamsInternal(params pagination.QueryParams) pagination.QueryParams {
	params.Start = max(params.Start, 0)
	switch {
	case params.Limit == -1:
	case params.Limit <= 0:
		params.Limit = 20
	case params.Limit > remoteActivityPageCeiling:
		params.Limit = remoteActivityPageCeiling
	}
	return params
}

// remoteActivityWindowInternal is how many newest rows each source must supply
// for the merged page to be exact: every row in the global top start+limit is
// in its own source's top start+limit under the shared sort order.
func remoteActivityWindowInternal(params pagination.QueryParams) int {
	if params.Limit == -1 {
		return -1
	}
	return params.Start + params.Limit
}

// collectActivityWindowInternal reads window rows from a source in pages the
// agent accepts, stopping early when the source runs out. -1 reads everything.
func collectActivityWindowInternal(window int, fetchPage func(start, limit int) ([]activitytypes.Activity, int64, error)) ([]activitytypes.Activity, int64, error) {
	if window == -1 {
		return fetchPage(0, -1)
	}
	var rows []activitytypes.Activity
	var total int64
	for len(rows) < window {
		limit := min(remoteActivityPageCeiling, window-len(rows))
		page, pageTotal, err := fetchPage(len(rows), limit)
		if err != nil {
			return nil, 0, err
		}
		rows = append(rows, page...)
		total = pageTotal
		if len(page) < limit {
			break
		}
	}
	return rows, total, nil
}

// ListRemoteActivities combines manager-owned summaries with agent-owned work
// before slicing the page. The remote records must already match the filters
// and be the agent's newest window; remoteTotal is the agent's filtered count.
func (s *ActivityService) ListRemoteActivities(ctx context.Context, environmentID string, remote []activitytypes.Activity, remoteTotal int64, params pagination.QueryParams) ([]activitytypes.Activity, pagination.Response, error) {
	params = normalizeRemoteActivityParamsInternal(params)
	activities, localTotal, err := collectActivityWindowInternal(remoteActivityWindowInternal(params), func(start, limit int) ([]activitytypes.Activity, int64, error) {
		local := params
		local.Start, local.Limit = start, limit
		rows, page, err := s.ListActivitiesPaginated(ctx, environmentID, local)
		if err != nil {
			return nil, 0, err
		}
		return rows, page.TotalItems, nil
	})
	if err != nil {
		return nil, pagination.Response{}, err
	}
	for _, item := range remote {
		item.EnvironmentID = environmentID
		activities = append(activities, item)
	}
	total := remoteTotal + localTotal
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

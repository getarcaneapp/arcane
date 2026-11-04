package environment

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"kit.getarcane.app"
	"kit.getarcane.app/mapping"

	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type Lister struct {
	db *database.DB
}

func NewLister(db *database.DB) *Lister {
	return &Lister{db: db}
}

func (l *Lister) ListAccessibleEnvironments(ctx context.Context, ps *authz.PermissionSet, params pagination.QueryParams) (types.Paginated[types.Environment], error) {
	envs, page, err := l.ListEnvironmentsPaginated(ctx, params, ps.AccessibleEnvironmentIDs())
	if err != nil {
		return types.Paginated[types.Environment]{}, err
	}
	rows := make([]types.Environment, 0, len(envs))
	for _, env := range envs {
		rows = append(rows, ApplyEnvironmentRuntimeState(env))
	}
	return types.Paginated[types.Environment]{
		Success:    true,
		Data:       rows,
		Pagination: handlerutil.PaginationResponse(page),
	}, nil
}

func (l *Lister) ListEnvironmentsPaginated(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]types.Environment, pagination.Response, error) {
	if strings.TrimSpace(params.Filters["type"]) != "" || strings.TrimSpace(params.Filters["status"]) != "" || params.Sort == "status" {
		var envs []Environment
		if err := l.db.WithContext(ctx).
			Model(&Environment{}).
			Where("hidden = ?", false).
			Find(&envs).Error; err != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to list environments: %w", err)
		}

		items, mapErr := mapping.MapSlice[Environment, types.Environment](envs)
		if mapErr != nil {
			return nil, pagination.Response{}, fmt.Errorf("failed to map environments: %w", mapErr)
		}

		// nil means no restriction; non-nil restricts to the caller's accessible environments.
		if accessibleEnvIDs != nil {
			items = slices.DeleteFunc(items, func(item types.Environment) bool { return !slices.Contains(accessibleEnvIDs, item.ID) })
		}

		rows := make([]types.Environment, 0, len(items))
		for _, item := range items {
			rows = append(rows, ApplyEnvironmentRuntimeState(item))
		}

		result := pagination.Config[types.Environment]{
			SearchAccessors: []pagination.SearchAccessor[types.Environment]{
				func(env types.Environment) (string, error) { return env.Name, nil },
				func(env types.Environment) (string, error) { return env.ApiUrl, nil },
			},
			SortBindings: []pagination.SortBinding[types.Environment]{
				{
					Key: "id",
					Fn: func(a, b types.Environment) int {
						return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
					},
				},
				{
					Key: "name",
					Fn: func(a, b types.Environment) int {
						return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
					},
				},
				{
					Key: "status",
					Fn: func(a, b types.Environment) int {
						return strings.Compare(strings.ToLower(a.Status), strings.ToLower(b.Status))
					},
				},
				{
					Key: "enabled",
					Fn: func(a, b types.Environment) int {
						if a.Enabled == b.Enabled {
							return 0
						}
						return kit.Ternary(a.Enabled, 1, -1)
					},
				},
				{
					Key: "apiUrl",
					Fn: func(a, b types.Environment) int {
						return strings.Compare(strings.ToLower(a.ApiUrl), strings.ToLower(b.ApiUrl))
					},
				},
			},
			FilterAccessors: []pagination.FilterAccessor[types.Environment]{
				{
					Key: "status",
					Fn: func(item types.Environment, filterValue string) bool {
						return strings.EqualFold(item.Status, strings.TrimSpace(filterValue))
					},
				},
				{
					Key: "enabled",
					Fn: func(item types.Environment, filterValue string) bool {
						value, valid := kit.ParseBool(filterValue)
						return !valid || item.Enabled == value
					},
				},
				{
					Key: "type",
					Fn: func(item types.Environment, filterValue string) bool {
						if item.ID == LocalEnvironmentID {
							return false
						}
						// Disconnected agents classify by the transport they last used.
						switch strings.ToLower(strings.TrimSpace(kit.Ternary(kit.FromPtr(item.Connected) && item.AgentTransport != nil,
							kit.FromPtr(item.AgentTransport), kit.FromPtr(item.LastAgentTransport)))) {
						case agent.TransportWebSocket:
							return strings.ToLower(strings.TrimSpace(filterValue)) == "websocket"
						case agent.TransportGRPC:
							return strings.ToLower(strings.TrimSpace(filterValue)) == "grpc"
						default:
							return strings.ToLower(strings.TrimSpace(filterValue)) == "agent"
						}
					},
				},
			},
		}.SearchOrderAndPaginate(rows, params)

		return result.Items, pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params), nil
	}

	var envs []Environment
	q := l.db.WithContext(ctx).Model(&Environment{}).Where("hidden = ?", false)
	// nil means no restriction; a non-nil slice limits results, so an empty one matches nothing.
	switch {
	case accessibleEnvIDs == nil:
		// no restriction
	case len(accessibleEnvIDs) == 0:
		q = q.Where("1 = 0")
	default:
		q = q.Where("id IN ?", accessibleEnvIDs)
	}

	if term := strings.TrimSpace(params.Search); term != "" {
		searchPattern := "%" + term + "%"
		q = q.Where(
			"name LIKE ? OR api_url LIKE ?",
			searchPattern, searchPattern,
		)
	}

	q = pagination.ApplyFilter(q, "status", params.Filters["status"])
	q = pagination.ApplyBooleanFilter(q, "enabled", params.Filters["enabled"])

	paginationResp, err := pagination.PaginateAndSortDB(params, q, &envs)
	if err != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to paginate environments: %w", err)
	}

	out, mapErr := mapping.MapSlice[Environment, types.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, fmt.Errorf("failed to map environments: %w", mapErr)
	}

	return out, paginationResp, nil
}

package environment

import (
	"context"
	"strings"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/environment"
)

func (s *EnvironmentService) ListEnvironmentsPaginated(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]environment.Environment, pagination.Response, error) {
	if strings.TrimSpace(params.Filters["type"]) != "" {
		return s.listEnvironmentsPaginatedWithRuntimeFiltersInternal(ctx, params, accessibleEnvIDs)
	}

	var envs []Environment
	q := s.db.WithContext(ctx).Model(&Environment{}).Where("hidden = ?", false)
	// accessibleEnvIDs == nil means "no restriction". A non-nil slice limits the
	// result to those environment IDs; an empty slice therefore matches nothing.
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
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to paginate environments")
	}

	out, mapErr := mapper.MapSlice[Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, errors.WrapIf(mapErr, "failed to map environments")
	}

	return out, paginationResp, nil
}

// filterEnvironmentsByIDInternal returns only the environments whose ID is in
// allowedIDs. A non-nil but empty allowedIDs yields an empty slice. Used to
// restrict the runtime-filtered list path to a caller's accessible environments.
func filterEnvironmentsByIDInternal(items []environment.Environment, allowedIDs []string) []environment.Environment {
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	out := make([]environment.Environment, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func (s *EnvironmentService) listEnvironmentsPaginatedWithRuntimeFiltersInternal(ctx context.Context, params pagination.QueryParams, accessibleEnvIDs []string) ([]environment.Environment, pagination.Response, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("hidden = ?", false).
		Find(&envs).Error; err != nil {
		return nil, pagination.Response{}, errors.WrapIf(err, "failed to list environments")
	}

	items, mapErr := mapper.MapSlice[Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, pagination.Response{}, errors.WrapIf(mapErr, "failed to map environments")
	}

	// nil = no restriction; non-nil restricts to the caller's accessible envs.
	if accessibleEnvIDs != nil {
		items = filterEnvironmentsByIDInternal(items, accessibleEnvIDs)
	}

	for i := range items {
		ApplyEnvironmentRuntimeState(&items[i])
	}

	config := pagination.Config[environment.Environment]{
		SearchAccessors: []pagination.SearchAccessor[environment.Environment]{
			func(env environment.Environment) (string, error) { return env.Name, nil },
			func(env environment.Environment) (string, error) { return env.ApiUrl, nil },
		},
		SortBindings: []pagination.SortBinding[environment.Environment]{
			{
				Key: "id",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
				},
			},
			{
				Key: "name",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
				},
			},
			{
				Key: "status",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.Status), strings.ToLower(b.Status))
				},
			},
			{
				Key: "enabled",
				Fn: func(a, b environment.Environment) int {
					if a.Enabled == b.Enabled {
						return 0
					}
					if a.Enabled {
						return 1
					}
					return -1
				},
			},
			{
				Key: "apiUrl",
				Fn: func(a, b environment.Environment) int {
					return strings.Compare(strings.ToLower(a.ApiUrl), strings.ToLower(b.ApiUrl))
				},
			},
		},
		FilterAccessors: []pagination.FilterAccessor[environment.Environment]{
			{
				Key: "status",
				Fn: func(item environment.Environment, filterValue string) bool {
					return strings.EqualFold(item.Status, strings.TrimSpace(filterValue))
				},
			},
			{
				Key: "enabled",
				Fn: func(item environment.Environment, filterValue string) bool {
					switch strings.ToLower(strings.TrimSpace(filterValue)) {
					case "true", "1":
						return item.Enabled
					case "false", "0":
						return !item.Enabled
					default:
						return true
					}
				},
			},
			{
				Key: "type",
				Fn:  environmentTypeMatchesInternal,
			},
		},
	}

	result := config.SearchOrderAndPaginate(items, params)
	paginationResp := pagination.BuildResponse(result.TotalCount, result.TotalAvailable, params)

	return result.Items, paginationResp, nil
}

func environmentTypeMatchesInternal(env environment.Environment, filterValue string) bool {
	return environmentTypeKeyInternal(env) == strings.ToLower(strings.TrimSpace(filterValue))
}

func environmentTypeKeyInternal(env environment.Environment) string {
	if !env.IsEdge {
		return "http"
	}
	transport := ""
	if env.Connected != nil && *env.Connected && env.EdgeTransport != nil {
		transport = *env.EdgeTransport
	} else if env.LastEdgeTransport != nil {
		// Disconnected or poll-only agents classify by the transport they
		// last used rather than collapsing into the generic edge bucket.
		transport = *env.LastEdgeTransport
	}
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case edge.EdgeTransportWebSocket:
		return "websocket"
	case edge.EdgeTransportGRPC:
		return "grpc"
	default:
		return "edge"
	}
}

func (s *EnvironmentService) ListVisibleEnvironments(ctx context.Context) ([]environment.Environment, error) {
	var envs []Environment
	if err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("hidden = ?", false).
		Order("created_at asc, id asc").
		Find(&envs).Error; err != nil {
		return nil, errors.WrapIf(err, "failed to list visible environments")
	}

	out, mapErr := mapper.MapSlice[Environment, environment.Environment](envs)
	if mapErr != nil {
		return nil, errors.WrapIf(mapErr, "failed to map environments")
	}

	for i := range out {
		ApplyEnvironmentRuntimeState(&out[i])
	}

	return out, nil
}

// ListRemoteEnvironmentIDs returns the IDs of enabled remote environments; it
// satisfies agg.RemoteEnvironmentLister for aggregated stream handlers.
func (s *EnvironmentService) ListRemoteEnvironmentIDs(ctx context.Context) ([]string, error) {
	envs, err := s.ListRemoteEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(envs))
	for _, env := range envs {
		ids = append(ids, env.ID)
	}
	return ids, nil
}

// ListRemoteEnvironments returns all non-local, enabled environments for syncing purposes.
func (s *EnvironmentService) ListRemoteEnvironments(ctx context.Context) ([]Environment, error) {
	var envs []Environment
	err := s.db.WithContext(ctx).
		Model(&Environment{}).
		Where("id != ?", "0").
		Where("enabled = ?", true).
		Where("hidden = ?", false).
		Find(&envs).Error
	if err != nil {
		return nil, errors.WrapIf(err, "failed to list remote environments")
	}
	s.remoteEnvs.replace(envs)
	return envs, nil
}

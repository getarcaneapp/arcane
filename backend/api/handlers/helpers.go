package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

// ActivityAppContext carries the app lifecycle context through handler registration.
type ActivityAppContext struct {
	ctx context.Context
}

// NewActivityAppContext wraps the app lifecycle context for handler constructors.
func NewActivityAppContext(ctx context.Context) ActivityAppContext {
	return ActivityAppContext{ctx: ctx}
}

// ContextInternal returns the wrapped app lifecycle context.
func (c ActivityAppContext) ContextInternal() context.Context {
	return c.contextInternal()
}

func (c ActivityAppContext) contextInternal() context.Context {
	return c.ctx
}

// buildPaginationParamsInternal converts query parameters to pagination.QueryParams.
// A limit of -1 means "show all items" (no pagination).
func buildPaginationParamsInternal(start, limit int, sortCol, sortDir, search string) pagination.QueryParams {
	// limit = -1 means "show all" and 0 means "no pagination"; only invalid values default to 20
	if limit < -1 {
		limit = 20
	}

	return pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: strings.TrimSpace(search),
		},
		SortParams: pagination.SortParams{
			Sort:  strings.TrimSpace(sortCol),
			Order: pagination.SortOrder(sortDir),
		},
		Params: pagination.Params{
			Start: start,
			Limit: limit,
		},
		Filters: make(map[string]string),
	}
}

// toPaginationResponseInternal converts the pagination package's response into the API response shape.
func toPaginationResponseInternal(p pagination.Response) base.PaginationResponse {
	return base.PaginationResponse{
		TotalPages:      p.TotalPages,
		TotalItems:      p.TotalItems,
		CurrentPage:     p.CurrentPage,
		ItemsPerPage:    p.ItemsPerPage,
		GrandTotalItems: p.GrandTotalItems,
	}
}

// requireUserInternal returns the authenticated user from the request context or a 401 error.
func requireUserInternal(ctx context.Context) (*models.User, error) {
	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists || user == nil {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}
	return user, nil
}

func registerSecuredInternal[I, O any](
	api huma.API,
	op huma.Operation,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	op.Security = defaultOperationSecurityInternal()
	op.Middlewares = humamw.RequirePermission(api, permission)
	huma.Register(api, op, handler)
}

func registerCustomizeSecuredInternal[I, O any](
	api huma.API,
	operationID string,
	method string,
	path string,
	summary string,
	description string,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	registerTaggedSecuredInternal(api, operationID, method, path, summary, description, "Customize", permission, handler)
}

func registerGitOpsSecuredInternal[I, O any](
	api huma.API,
	operationID string,
	method string,
	path string,
	summary string,
	description string,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	registerTaggedSecuredInternal(api, operationID, method, path, summary, description, "GitOps Syncs", permission, handler)
}

func registerTaggedSecuredInternal[I, O any](
	api huma.API,
	operationID string,
	method string,
	path string,
	summary string,
	description string,
	tag string,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	registerSecuredInternal(api, operationInternal(operationID, method, path, summary, description, tag), permission, handler)
}

func operationInternal(operationID, method, path, summary, description string, tags ...string) huma.Operation {
	return huma.Operation{
		OperationID: operationID,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Description: description,
		Tags:        tags,
	}
}

func currentActorInternal(ctx context.Context) models.User {
	actor := models.User{}
	if currentUser, exists := humamw.GetCurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}
	return actor
}

func mapOneAPIResponseInternal[S any, D any](source S, mappingMessage func(error) string) (base.ApiResponse[D], error) {
	out, err := mapper.MapOne[S, D](source)
	if err != nil {
		return base.ApiResponse[D]{}, huma.Error500InternalServerError(mappingMessage(err))
	}

	return base.ApiResponse[D]{
		Success: true,
		Data:    out,
	}, nil
}

func defaultOperationSecurityInternal() []map[string][]string {
	return []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}
}

func proxyRemoteJSONInternal[T any](
	ctx context.Context,
	environmentService *services.EnvironmentService,
	environmentID string,
	method string,
	path string,
	requestBody any,
) (*T, error) {
	body, err := marshalRemoteRequestBodyInternal(requestBody)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to marshal request body: " + err.Error())
	}

	var output T
	if err := environmentService.ProxyJSONRequest(ctx, environmentID, method, path, body, &output); err != nil {
		return nil, translateRemoteProxyErrorInternal(err)
	}

	return &output, nil
}

func marshalRemoteRequestBodyInternal(requestBody any) ([]byte, error) {
	switch value := requestBody.(type) {
	case nil:
		return nil, nil
	case []byte:
		return value, nil
	default:
		return json.Marshal(value)
	}
}

func translateRemoteProxyErrorInternal(err error) error {
	if transportErr, ok := errors.AsType[*remenv.TransportError](err); ok {
		return huma.Error502BadGateway("failed to proxy request to environment: " + transportErr.Error())
	}

	if statusErr, ok := errors.AsType[*remenv.StatusError](err); ok {
		return huma.NewError(statusErr.StatusCode, "environment returned error: "+string(statusErr.Body), nil)
	}

	if decodeErr, ok := errors.AsType[*remenv.DecodeError](err); ok {
		return huma.Error500InternalServerError("failed to decode environment response: " + decodeErr.Error())
	}

	return huma.Error500InternalServerError("failed to proxy request to environment: " + err.Error())
}

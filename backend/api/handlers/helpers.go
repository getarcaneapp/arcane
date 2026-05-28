package handlers

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/pkg/remenv"
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
	// limit = -1 means "show all", preserve it; zero or other negative values default to 20
	if limit < -1 {
		limit = 20
	}

	return pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: search,
		},
		SortParams: pagination.SortParams{
			Sort:  sortCol,
			Order: pagination.SortOrder(sortDir),
		},
		PaginationParams: pagination.PaginationParams{
			Start: start,
			Limit: limit,
		},
		Filters: make(map[string]string),
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
	var transportErr *remenv.TransportError
	if errors.As(err, &transportErr) {
		return huma.Error502BadGateway("failed to proxy request to environment: " + transportErr.Error())
	}

	var statusErr *remenv.StatusError
	if errors.As(err, &statusErr) {
		return huma.NewError(statusErr.StatusCode, "environment returned error: "+string(statusErr.Body), nil)
	}

	var decodeErr *remenv.DecodeError
	if errors.As(err, &decodeErr) {
		return huma.Error500InternalServerError("failed to decode environment response: " + decodeErr.Error())
	}

	return huma.Error500InternalServerError("failed to proxy request to environment: " + err.Error())
}

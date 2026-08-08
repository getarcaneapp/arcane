package handlerutil

import (
	"context"
	"mime/multipart"
	"strings"

	"emperror.dev/errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/auth"
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

// Context returns the wrapped app lifecycle context.
func (c ActivityAppContext) Context() context.Context {
	return c.ctx
}

// PaginationParams converts query parameters to pagination.QueryParams.
// A limit of -1 means "show all items" (no pagination).
func PaginationParams(start, limit int, sortCol, sortDir, search string) pagination.QueryParams {
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

// PaginationResponse converts the pagination package's response into the API response shape.
func PaginationResponse(p pagination.Response) base.PaginationResponse {
	return base.PaginationResponse{
		TotalPages:      p.TotalPages,
		TotalItems:      p.TotalItems,
		CurrentPage:     p.CurrentPage,
		ItemsPerPage:    p.ItemsPerPage,
		GrandTotalItems: p.GrandTotalItems,
	}
}

// RequireUser returns the authenticated user from the request context or a 401 error.
func RequireUser(ctx context.Context) (*models.User, error) {
	user, exists := models.CurrentUserFromContext(ctx)
	if !exists || user == nil {
		return nil, huma.Error401Unauthorized("Not authenticated")
	}
	return user, nil
}

func OpenUploadedFile(form multipart.Form) (multipart.File, *multipart.FileHeader, error) {
	files := form.File["file"]
	if len(files) == 0 {
		return nil, nil, huma.Error400BadRequest("No file uploaded")
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to read upload").Error())
	}

	return file, fileHeader, nil
}

func RegisterSecured[I, O any](
	api huma.API,
	op huma.Operation,
	permission string,
	handler func(context.Context, *I) (*O, error),
) {
	op.Security = DefaultOperationSecurity()
	middleware.RegisterWithPermission(api, op, permission, handler)
}

func Operation(operationID, method, path, summary, description string, tags ...string) huma.Operation {
	return huma.Operation{
		OperationID: operationID,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Description: description,
		Tags:        tags,
	}
}

func CurrentActor(ctx context.Context) models.User {
	actor := models.User{}
	if currentUser, exists := models.CurrentUserFromContext(ctx); exists && currentUser != nil {
		actor = *currentUser
	}
	return actor
}

func MapOneAPIResponse[S any, D any](source S, mappingMessage func(error) string) (base.ApiResponse[D], error) {
	out, err := mapper.MapOne[S, D](source)
	if err != nil {
		return base.ApiResponse[D]{}, huma.Error500InternalServerError(mappingMessage(err))
	}

	return base.ApiResponse[D]{
		Success: true,
		Data:    out,
	}, nil
}

func DefaultOperationSecurity() []map[string][]string {
	return []map[string][]string{
		{"BearerAuth": {}},
		{"ApiKeyAuth": {}},
	}
}

// SessionMetaFromContext builds the session metadata recorded on login.
func SessionMetaFromContext(ctx context.Context, userAgent string) auth.SessionMeta {
	return auth.SessionMeta{
		UserAgent: userAgent,
		IPAddress: middleware.GetRemoteAddrFromContext(ctx),
	}
}

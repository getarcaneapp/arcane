package handlerutil

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	"github.com/getarcaneapp/arcane/types/v2/auth"
	"github.com/getarcaneapp/arcane/types/v2/base"
)

// Out is the huma response envelope for endpoints returning base.ApiResponse[T].
// It replaces the per-endpoint `type XxxOutput struct { Body base.ApiResponse[T] }`
// wrappers; outputs carrying extra fields (headers, status) keep their own type.
type Out[T any] struct {
	Body base.ApiResponse[T]
}

// Page is the huma response envelope for endpoints returning base.Paginated[T].
type Page[T any] struct {
	Body base.Paginated[T]
}

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
		Search:  strings.TrimSpace(search),
		Sort:    strings.TrimSpace(sortCol),
		Order:   pagination.SortOrder(sortDir),
		Start:   start,
		Limit:   limit,
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
func RequireUser(ctx context.Context) (*common.User, error) {
	user, exists := common.CurrentUserFromContext(ctx)
	if !exists || user == nil {
		return nil, huma.Error401Unauthorized("Not authenticated")
	}
	return user, nil
}

// ParseMultipartJSONPart decodes one required JSON-valued multipart field.
func ParseMultipartJSONPart[T any](form multipart.Form, partName string) (T, error) {
	var result T
	values := form.Value[partName]
	if len(values) != 1 {
		return result, huma.Error400BadRequest("exactly one " + partName + " part is required")
	}
	if err := json.Unmarshal([]byte(values[0]), &result); err != nil {
		return result, huma.Error400BadRequest("invalid " + partName + " JSON")
	}
	return result, nil
}

// ReadWorkspaceUploads reads and validates text files from a workspace multipart request.
func ReadWorkspaceUploads(form multipart.Form, maxFileSizeBytes int64) (map[int][]byte, error) {
	headers := form.File["files"]
	uploads := make(map[int][]byte, len(headers))
	limitMessage := fmt.Sprintf("uploaded file exceeds configured %d MiB workspace limit", maxFileSizeBytes/(1024*1024))
	for index, header := range headers {
		if header.Size > maxFileSizeBytes {
			return nil, huma.Error413RequestEntityTooLarge(limitMessage)
		}
		file, err := header.Open()
		if err != nil {
			return nil, huma.Error400BadRequest("failed to open uploaded file")
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxFileSizeBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, huma.Error400BadRequest("failed to read uploaded file")
		}
		if closeErr != nil {
			return nil, huma.Error400BadRequest("failed to close uploaded file")
		}
		if int64(len(content)) > maxFileSizeBytes {
			return nil, huma.Error413RequestEntityTooLarge(limitMessage)
		}
		if err := workspacepkg.ValidateTextContent(content, maxFileSizeBytes); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		uploads[index] = content
	}
	return uploads, nil
}

// WorkspaceMultipartRequestBody describes the shared workspace multipart schema.
func WorkspaceMultipartRequestBody(manifestDescription string) *huma.RequestBody {
	return &huma.RequestBody{Content: map[string]*huma.MediaType{"multipart/form-data": {Schema: &huma.Schema{Type: "object", Properties: map[string]*huma.Schema{
		"manifest": {Type: "string", Description: manifestDescription},
		"files":    {Type: "array", Items: &huma.Schema{Type: "string", Format: "binary"}},
	}, Required: []string{"manifest"}}}}}
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

func CurrentActor(ctx context.Context) common.User {
	actor := common.User{}
	if currentUser, exists := common.CurrentUserFromContext(ctx); exists && currentUser != nil {
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

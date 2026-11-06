package handlerutil

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/samber/mo"

	"kit.getarcane.app"
	"kit.getarcane.app/mapping"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	Out[T any] struct {
		Body types.ApiResponse[T]
	}

	Page[T any] struct {
		Body types.Paginated[T]
	}

	ActivityAppContext struct {
		ctx context.Context
	}

	ProblemError struct {
		*huma.ErrorModel

		Details any `json:"details,omitempty"`
	}
)

func MessageOutput(message, activityID string) *Out[types.MessageResponse] {
	return &Out[types.MessageResponse]{Body: types.ApiResponse[types.MessageResponse]{
		Success: true,
		Data: types.MessageResponse{
			Message:    message,
			ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		},
	}}
}

func NewActivityAppContext(ctx context.Context) ActivityAppContext {
	return ActivityAppContext{ctx: ctx}
}

func (c ActivityAppContext) Context() context.Context {
	return c.ctx
}

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

func PaginationResponse(p pagination.Response) types.PaginationResponse {
	return types.PaginationResponse{
		TotalPages:      p.TotalPages,
		TotalItems:      p.TotalItems,
		CurrentPage:     p.CurrentPage,
		ItemsPerPage:    p.ItemsPerPage,
		GrandTotalItems: p.GrandTotalItems,
		HasNextPage:     p.HasNextPage,
	}
}

func RequireUser(ctx context.Context) (*types.Actor, error) {
	user, exists := CurrentUserFromContext(ctx)
	if !exists || user == nil {
		return nil, huma.Error401Unauthorized("Not authenticated")
	}
	return user, nil
}

func APIError(err error) huma.StatusError {
	apiErr := common.ToAPIError(err)
	return &ProblemError{
		ErrorModel: &huma.ErrorModel{
			Title:  http.StatusText(apiErr.HTTPStatus()),
			Status: apiErr.HTTPStatus(),
			Detail: apiErr.Message,
		},
		Details: apiErr.Details,
	}
}

func Error400BadRequestWithType(err error, problemType string) huma.StatusError {
	return &ProblemError{
		ErrorModel: &huma.ErrorModel{
			Type:   problemType,
			Title:  http.StatusText(http.StatusBadRequest),
			Status: http.StatusBadRequest,
			Detail: err.Error(),
		},
		Details: common.ToAPIError(err).Details,
	}
}

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
		uploads[index] = content
	}
	return uploads, nil
}

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

func CurrentActor(ctx context.Context) types.Actor {
	currentUser, _ := CurrentUserFromContext(ctx)
	return kit.FromPtr(currentUser)
}

func MapOneAPIResponse[S, D any](source S, mappingMessage func(error) string) (types.ApiResponse[D], error) {
	out, err := mapping.MapOne[S, D](source)
	if err != nil {
		return types.ApiResponse[D]{}, huma.Error500InternalServerError(mappingMessage(err))
	}

	return types.ApiResponse[D]{
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

func SessionMetaFromContext(ctx context.Context, userAgent string) types.SessionMeta {
	return types.SessionMeta{
		UserAgent: userAgent,
		IPAddress: middleware.GetRemoteAddrFromContext(ctx),
	}
}

func DownloadResponse(reader io.ReadCloser, size int64, filename string) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(ctx huma.Context) {
		defer func() { _ = reader.Close() }()
		ctx.SetHeader("Content-Type", "application/octet-stream")
		ctx.SetHeader("Content-Disposition", "attachment; filename="+filename)
		if size >= 0 {
			ctx.SetHeader("Content-Length", strconv.FormatInt(size, 10))
		}
		_, _ = io.Copy(ctx.BodyWriter(), reader)
	}}
}

func FeatureDisabledError(id types.FeatureID) *types.FeatureDisabledError {
	return &types.FeatureDisabledError{
		Status:  http.StatusForbidden,
		Title:   http.StatusText(http.StatusForbidden),
		Detail:  fmt.Sprintf("feature %s is disabled", id),
		Code:    string(common.APIErrorCodeFeatureDisabled),
		Feature: id,
	}
}

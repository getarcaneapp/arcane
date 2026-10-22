package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type AuditHandler struct {
	service *AuditService
}

type AuditFilterInput struct {
	ActorUserID   string    `query:"actorUserId" doc:"Only entries performed by this user"`
	Action        string    `query:"action" doc:"Only entries with this action, such as role.create or authz.denied"`
	Outcome       string    `query:"outcome" enum:"success,denied,error" doc:"Only entries with this outcome"`
	TargetType    string    `query:"targetType" doc:"Only entries for this target type"`
	EnvironmentID string    `query:"environmentId" doc:"Only entries for this environment"`
	From          time.Time `query:"from" doc:"Only entries recorded at or after this time (RFC 3339)"`
	To            time.Time `query:"to" doc:"Only entries recorded before this time (RFC 3339)"`
}

type ListAuditLogsInput struct {
	AuditFilterInput

	Search string `query:"search" doc:"Search by action, actor, target, or permission"`
	Sort   string `query:"sort" default:"createdAt" doc:"Column to sort by"`
	Order  string `query:"order" default:"desc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type ExportAuditLogsInput struct {
	AuditFilterInput

	Format string `query:"format" enum:"csv,ndjson" default:"csv" doc:"Export file format"`
}

type AuditLogIDInput struct {
	ID string `path:"id" doc:"Audit log entry ID"`
}

func (in AuditFilterInput) filter() types.AuditFilter {
	return types.AuditFilter{
		ActorUserID:   in.ActorUserID,
		Action:        in.Action,
		Outcome:       in.Outcome,
		TargetType:    in.TargetType,
		EnvironmentID: in.EnvironmentID,
		From:          kit.Ternary(in.From.IsZero(), nil, new(in.From)),
		To:            kit.Ternary(in.To.IsZero(), nil, new(in.To)),
	}
}

func (h *AuditHandler) List(ctx context.Context, input *ListAuditLogsInput) (*handlerutil.Page[types.AuditLog], error) {
	logs, page, err := h.service.List(ctx, handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search), input.filter())
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &handlerutil.Page[types.AuditLog]{
		Body: types.Paginated[types.AuditLog]{
			Success:    true,
			Data:       logs,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *AuditHandler) Get(ctx context.Context, input *AuditLogIDInput) (*handlerutil.Out[types.AuditLog], error) {
	entry, err := h.service.Get(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &handlerutil.Out[types.AuditLog]{
		Body: types.ApiResponse[types.AuditLog]{
			Success: true,
			Data:    entry,
		},
	}, nil
}

func (h *AuditHandler) Export(ctx context.Context, input *ExportAuditLogsInput) (*huma.StreamResponse, error) {
	filename := fmt.Sprintf("audit-logs-%s.%s", time.Now().UTC().Format("2006-01-02T15-04-05Z"), input.Format)
	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			humaCtx.SetHeader("Content-Type", kit.Ternary(input.Format == types.AuditExportFormatCSV, "text/csv; charset=utf-8", "application/x-ndjson"))
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
			if err := h.service.Export(ctx, input.filter(), input.Format, humaCtx.BodyWriter()); err != nil {
				slog.ErrorContext(ctx, "failed to stream audit log export", "format", input.Format, "error", err)
			}
		},
	}, nil
}

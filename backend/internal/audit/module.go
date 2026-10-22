package audit

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *AuditService
}

func New(service *AuditService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *AuditService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	h := &AuditHandler{service: m.Service()}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-audit-logs",
		Method:      http.MethodGet,
		Path:        "/audit/logs",
		Summary:     "List audit log entries",
		Description: "List recorded RBAC changes and authorization denials, newest first by default.",
		Tags:        []string{"Audit"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermAuditRead, h.List)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "export-audit-logs",
		Method:      http.MethodGet,
		Path:        "/audit/logs/export",
		Summary:     "Export audit log entries",
		Description: "Stream every audit log entry matching the filters as CSV or newline-delimited JSON, oldest first.",
		Tags:        []string{"Audit"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermAuditExport, h.Export)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-audit-log",
		Method:      http.MethodGet,
		Path:        "/audit/logs/{id}",
		Summary:     "Get an audit log entry",
		Tags:        []string{"Audit"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermAuditRead, h.Get)
}

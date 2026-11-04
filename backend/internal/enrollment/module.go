package enrollment

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
)

type (
	Module struct {
		service *EnrollmentService
	}
)

func New(service *EnrollmentService) *Module {
	return &Module{service: service}
}

func (m *Module) RegisterRoutes(api huma.API, cfg *config.Config) {
	if m == nil {
		RegisterEnrollment(api, nil, cfg)
		return
	}
	RegisterEnrollment(api, m.service, cfg)
}

func RegisterEnrollment(api huma.API, service *EnrollmentService, cfg *config.Config) {
	if cfg.AgentEnabled {
		return
	}
	h := &EnrollmentHandler{service: service}

	huma.Register(api, huma.Operation{
		OperationID:  "enroll-agent",
		Method:       http.MethodPost,
		Path:         "/agents/enroll",
		Summary:      "Enroll an agent",
		Description:  "An agent presents an enrollment token to register itself and receive its own agent token",
		Tags:         []string{"Enrollment"},
		MaxBodyBytes: 1024,
		Security:     []map[string][]string{},
	}, h.EnrollAgent)

	huma.Register(api, huma.Operation{
		OperationID: "enroll-agent-mtls",
		Method:      http.MethodPost,
		Path:        "/tunnel/mtls/enroll",
		Summary:     "Enroll an agent mTLS certificate",
		Tags:        []string{"Environments"},
		Hidden:      true,
		Security:    []map[string][]string{},
		Metadata:    map[string]any{authz.MetaAccess: authz.EndpointAccessAgentToken},
		Middlewares: huma.Middlewares{
			middleware.EchoMiddleware(middleware.PerIPRateLimit(10, 3)),
			middleware.EchoMiddleware(middleware.PerAgentTokenRateLimit(10, 3)),
			func(ctx huma.Context, next func(huma.Context)) {
				ctx.SetHeader("Cache-Control", "no-store")
				ctx.SetHeader("Pragma", "no-cache")
				next(huma.WithContext(ctx, context.WithValue(ctx.Context(), middleware.ContextKeyRemoteAddr, humaecho.Unwrap(ctx).RealIP())))
			},
		},
	}, h.EnrollMTLS)
}

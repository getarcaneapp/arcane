package enrollment

import (
	"cmp"
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	EnrollmentHandler struct {
		service *EnrollmentService
	}

	EnrollAgentInput struct {
		EnrollmentToken string `header:"X-Arcane-Enrollment-Token" doc:"Enrollment token"`
		AgentToken      string `header:"X-Arcane-Agent-Token" doc:"The agent's current token, if it has one"`
		Body            types.AgentEnrollRequest
	}

	EnrollMTLSInput struct {
		AgentToken    string `header:"X-Arcane-Agent-Token" doc:"Agent token"`
		APIKey        string `header:"X-Api-Key" doc:"Agent token, for proxies that strip custom headers"`
		Authorization string `header:"Authorization" doc:"Bearer agent token, for proxies that strip custom headers"`
		Body          agent.EnrollMTLSRequest
	}

	EnrollMTLSOutput struct {
		Body agent.EnrollMTLSResponse
	}
)

func (h *EnrollmentHandler) EnrollAgent(ctx context.Context, input *EnrollAgentInput) (*handlerutil.Out[types.AgentEnrollResponse], error) {
	enrolled, err := h.service.Enroll(ctx, EnrollParams{
		EnrollmentToken: input.EnrollmentToken,
		AgentToken:      input.AgentToken,
		Request:         input.Body,
	})
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &handlerutil.Out[types.AgentEnrollResponse]{
		Body: types.ApiResponse[types.AgentEnrollResponse]{
			Success: true,
			Data:    *enrolled,
		},
	}, nil
}

func (h *EnrollmentHandler) EnrollMTLS(ctx context.Context, input *EnrollMTLSInput) (*EnrollMTLSOutput, error) {
	files, err := h.service.EnrollMTLS(ctx, EnrollMTLSParams{
		AgentToken: cmp.Or(
			strings.TrimSpace(input.AgentToken),
			strings.TrimSpace(input.APIKey),
			agent.ExtractBearerToken(input.Authorization),
		),
		RemoteAddr: middleware.GetRemoteAddrFromContext(ctx),
		CSR:        input.Body.CSR,
	})
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &EnrollMTLSOutput{Body: agent.EnrollMTLSResponse{Files: files}}, nil
}

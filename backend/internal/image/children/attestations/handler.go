package attestations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/containerd/platforms"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

const statusClientClosedRequest = 499

// Handler serves image attestation endpoints.
type Handler struct {
	service *Service
}

type GetImageAttestationsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageName     string `path:"name" doc:"Image ID or image reference"`
	Platform      string `query:"platform" doc:"OCI platform selector, for example linux/amd64"`
	PredicateType string `query:"predicateType" doc:"Exact in-toto predicate type URI to include"`
	WithStatement bool   `query:"statement" default:"false" doc:"Include verbatim statement JSON bodies"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetImageAttestations returns in-toto attestation statements attached to an image.
func (h *Handler) GetImageAttestations(ctx context.Context, input *GetImageAttestationsInput) (*handlerutil.Out[types.ImageAttestationList], error) {
	imageName := strings.TrimSpace(input.ImageName)
	if imageName == "" {
		return nil, huma.Error400BadRequest("image name is required")
	}

	if input.Platform != "" {
		if _, parseErr := platforms.Parse(input.Platform); parseErr != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid platform %q", input.Platform))
		}
	}

	out, err := h.service.GetImageAttestations(ctx, imageName, Query{
		Platform:         strings.TrimSpace(input.Platform),
		PredicateType:    strings.TrimSpace(input.PredicateType),
		IncludeStatement: input.WithStatement,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, huma.NewError(statusClientClosedRequest, "request canceled")
		}
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		slog.ErrorContext(ctx, "failed to get image attestations", "image", imageName, "error", err)
		return nil, huma.Error500InternalServerError("failed to get image attestations")
	}

	return &handlerutil.Out[types.ImageAttestationList]{
		Body: types.ApiResponse[types.ImageAttestationList]{
			Success: true,
			Data:    *out,
		},
	}, nil
}

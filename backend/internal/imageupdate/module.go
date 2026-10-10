// Package imageupdate owns image update checks, persistence, and HTTP routes.
package imageupdate

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

type Module struct {
	service                  *ImageUpdateService
	getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*image.UpdateInfo, error)
}

// New wires image update routes and registers the arcane.image.updates gauges once.
func New(service *ImageUpdateService, getUpdateInfo func(context.Context, []string) (map[string]*image.UpdateInfo, error)) *Module {
	if service != nil {
		meter := otel.Meter(tracing.InstrumentationName)
		updates, updatesErr := meter.Int64ObservableGauge("arcane.image.updates",
			metric.WithDescription("Local images with an available update by update type"), metric.WithUnit("{image}"))
		checkErrors, checkErrorsErr := meter.Int64ObservableGauge("arcane.image.update.check_errors",
			metric.WithDescription("Local images whose last update check failed"), metric.WithUnit("{image}"))
		err := errors.Join(updatesErr, checkErrorsErr)
		if err == nil {
			_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
				summary, summaryErr := service.GetUpdateSummary(ctx)
				if summaryErr != nil {
					slog.DebugContext(ctx, "Skipping image update metrics", "error", summaryErr)
					return nil
				}
				o.ObserveInt64(updates, int64(summary.DigestUpdates), metric.WithAttributes(attribute.String("image.update.type", "digest")))
				o.ObserveInt64(updates, int64(summary.ImagesWithUpdates-summary.DigestUpdates), metric.WithAttributes(attribute.String("image.update.type", "tag")))
				o.ObserveInt64(checkErrors, int64(summary.ErrorsCount))
				return nil
			}, updates, checkErrors)
		}
		if err != nil {
			otel.Handle(err)
		}
	}
	return &Module{service: service, getUpdateInfoByImageRefs: getUpdateInfo}
}

func (m *Module) Service() *ImageUpdateService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterImageUpdates(api, nil, nil, appCtx)
		return
	}
	RegisterImageUpdates(api, m.service, m.getUpdateInfoByImageRefs, appCtx)
}

// RegisterImageUpdates registers image update endpoints.
func RegisterImageUpdates(
	api huma.API,
	imageUpdateSvc *ImageUpdateService,
	getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*image.UpdateInfo, error),
	appCtx handlerutil.ActivityAppContext,
) {
	h := &ImageUpdateHandler{
		imageUpdateService:       imageUpdateSvc,
		getUpdateInfoByImageRefs: getUpdateInfoByImageRefs,
		appCtx:                   appCtx.Context(),
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check",
		Summary:     "Check image update by reference",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdate)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdateByID)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id-post",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check/{imageId}",
		Summary:     "Check image update by ID (POST)",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdateByID)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-multiple-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-batch",
		Summary:     "Check multiple images",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckMultipleImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-all-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/image-updates/check-all",
		Summary:     "Check all images",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckAllImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-info-by-refs",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/by-refs",
		Summary:     "Get persisted update info for image references",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdateInfoByRefs)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-summary",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/image-updates/summary",
		Summary:     "Get update summary",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesRead, h.GetUpdateSummary)
}

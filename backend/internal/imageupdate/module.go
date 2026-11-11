// Package imageupdate owns image update checks, persistence, and HTTP routes.
package imageupdate

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/imageupdate/children/releasenotes"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Module struct {
		service                  *ImageUpdateService
		getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*types.ImageUpdateInfo, error)
	}
)

func New(service *ImageUpdateService, getUpdateInfo func(context.Context, []string) (map[string]*types.ImageUpdateInfo, error)) *Module {
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
		releasenotes.RegisterRoutes(api, releasenotes.NewHandler(nil))
		return
	}
	RegisterImageUpdates(api, m.service, m.getUpdateInfoByImageRefs, appCtx)
	releasenotes.RegisterRoutes(api, releasenotes.NewHandler(m.service.releaseNotes))
}

// RegisterImageUpdates registers image update endpoints.
func RegisterImageUpdates(
	api huma.API,
	imageUpdateSvc *ImageUpdateService,
	getUpdateInfoByImageRefs func(context.Context, []string) (map[string]*types.ImageUpdateInfo, error),
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
		Path:        "/agents/{id}/images/updates/check",
		Summary:     "Check image update by reference",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdate)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/images/updates/check/{imageId}",
		Summary:     "Check image update by ID",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdateByID)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "check-image-update-by-id-post",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/images/updates/check/{imageId}",
		Summary:     "Check image update by ID (POST)",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesCheck, h.CheckImageUpdateByID)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "check-images",
		Method:      http.MethodPost,
		Path:        "/agents/{id}/images/updates/check",
		Summary:     "Check all or multiple images",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"all":   authz.PermImageUpdatesCheck,
		"batch": authz.PermImageUpdatesCheck,
	}, h.CheckImages)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-info-by-refs",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/images/updates/refs",
		Summary:     "Get persisted update info for image references",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesList, h.GetUpdateInfoByRefs)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-update-summary",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/images/updates/summary",
		Summary:     "Get update summary",
		Tags:        []string{"Image Updates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermImageUpdatesList, h.GetUpdateSummary)
}

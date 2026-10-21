package patch

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// RegisterRoutes registers image patching routes.
func RegisterRoutes(api huma.API, imagePatchService *Service, appCtx handlerutil.ActivityAppContext) {
	h := &ImagePatchHandler{
		imagePatchService: imagePatchService,
		appCtx:            appCtx.Context(),
	}

	handlerutil.RegisterSecured(api,
		handlerutil.Operation("list-image-patches", http.MethodGet, "/environments/{id}/images/patches", "List image patches", "Retrieves the paginated image patch history for the environment", "Images"),
		authz.PermImagesList, h.ListImagePatches)
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "list-image-patch-targets",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/patches/targets",
		Summary:     "List image patch targets",
		Description: "Retrieves scanned images with fixable vulnerability counts and their latest patch run",
		Tags:        []string{"Images"},
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermVulnsRead, h.ListPatchTargets)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"patch-image",
			http.MethodPost,
			"/environments/{id}/images/{imageId}/patch",
			"Patch image",
			"Patches OS package vulnerabilities in the image using Copacetic, producing a new patched tag",
			"Images",
		),
		authz.PermImagesPatch, h.PatchImage)
}

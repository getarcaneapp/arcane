package patch

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, imagePatchService *Service, appCtx handlerutil.ActivityAppContext) {
	h := &ImagePatchHandler{
		imagePatchService: imagePatchService,
		appCtx:            appCtx.Context(),
	}

	handlerutil.RegisterSecured(api,
		handlerutil.Operation("list-image-patches", http.MethodGet, "/agents/{id}/images/patches", "List image patches", "Retrieves the paginated image patch history for the environment", "Images"),
		authz.PermImagesList, h.ListImagePatches)
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "list-image-patch-targets",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/images/patches/targets",
		Summary:     "List image patch targets",
		Description: "Retrieves scanned images with fixable vulnerability counts and their latest patch run",
		Tags:        []string{"Images"},
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermVulnsList, h.ListPatchTargets)
	handlerutil.RegisterSecured(api,
		handlerutil.Operation(
			"patch-image",
			http.MethodPost,
			"/agents/{id}/images/{imageId}/patch",
			"Patch image",
			"Patches OS package vulnerabilities in the image using Copacetic, producing a new patched tag",
			"Images",
		),
		authz.PermImagesPatch, h.PatchImage)
}

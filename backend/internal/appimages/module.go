package appimages

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

var imageUploadRequestBody = &huma.RequestBody{
	Required: true,
	Content: map[string]*huma.MediaType{
		"multipart/form-data": {
			Schema: &huma.Schema{
				Type: "object",
				Properties: map[string]*huma.Schema{
					"file": {Type: "string", Format: "binary"},
				},
			},
		},
	},
}

func RegisterAppImages(api huma.API, appImagesService *ApplicationImagesService) {
	h := &AppImagesHandler{
		appImagesService: appImagesService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-logo",
		Method:      http.MethodGet,
		Path:        "/app/images/logo",
		Summary:     "Get application logo",
		Description: "Get the application logo image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.GetLogo)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:  "upload-logo",
		Method:       http.MethodPut,
		Path:         "/app/images/logo/{variant}",
		Summary:      "Upload custom logo",
		Description:  "Upload a custom logo (PNG, JPEG, WebP or SVG, up to 2 MB) for the mark or full slot. Replaces any existing upload.",
		Tags:         []string{"Application Images"},
		Security:     handlerutil.DefaultOperationSecurity(),
		MaxBodyBytes: ImageMaxBytes + 1<<20,
		RequestBody:  imageUploadRequestBody,
	}, authz.PermSettingsUpdate, h.UploadLogo)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-logo",
		Method:      http.MethodDelete,
		Path:        "/app/images/logo/{variant}",
		Summary:     "Delete custom logo",
		Description: "Remove the custom logo for the mark or full slot, reverting to the built-in artwork.",
		Tags:        []string{"Application Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsUpdate, h.DeleteLogo)

	huma.Register(api, huma.Operation{
		OperationID: "get-favicon",
		Method:      http.MethodGet,
		Path:        "/app/images/favicon",
		Summary:     "Get application favicon",
		Description: "Get the application favicon image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.GetFavicon)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:  "upload-favicon",
		Method:       http.MethodPut,
		Path:         "/app/images/favicon",
		Summary:      "Upload custom favicon",
		Description:  "Upload a custom favicon (PNG, SVG or ICO, up to 2 MB). Replaces any existing upload.",
		Tags:         []string{"Application Images"},
		Security:     handlerutil.DefaultOperationSecurity(),
		MaxBodyBytes: ImageMaxBytes + 1<<20,
		RequestBody:  imageUploadRequestBody,
	}, authz.PermSettingsUpdate, h.UploadFavicon)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-favicon",
		Method:      http.MethodDelete,
		Path:        "/app/images/favicon",
		Summary:     "Delete custom favicon",
		Description: "Remove the custom favicon, reverting to the built-in artwork.",
		Tags:        []string{"Application Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsUpdate, h.DeleteFavicon)

	huma.Register(api, huma.Operation{
		OperationID: "get-default-profile",
		Method:      http.MethodGet,
		Path:        "/app/images/profile",
		Summary:     "Get default profile image",
		Description: "Get the default user profile image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.GetDefaultProfile)

	huma.Register(api, huma.Operation{
		OperationID: "get-pwa-icon",
		Method:      http.MethodGet,
		Path:        "/app/images/pwa/{filename}",
		Summary:     "Get PWA icon",
		Description: "Get a Progressive Web App icon image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.GetPWAIcon)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID:  "upload-pwa-icon",
		Method:       http.MethodPut,
		Path:         "/app/images/pwa",
		Summary:      "Upload custom PWA icon",
		Description:  "Upload a square PNG of at least 512x512 pixels (up to 2 MB); it is resized to every PWA icon size. Replaces any existing upload.",
		Tags:         []string{"Application Images"},
		Security:     handlerutil.DefaultOperationSecurity(),
		MaxBodyBytes: ImageMaxBytes + 1<<20,
		RequestBody:  imageUploadRequestBody,
	}, authz.PermSettingsUpdate, h.UploadPWAIcon)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-pwa-icon",
		Method:      http.MethodDelete,
		Path:        "/app/images/pwa",
		Summary:     "Delete custom PWA icon",
		Description: "Remove the custom PWA icon, reverting to the built-in artwork.",
		Tags:        []string{"Application Images"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSettingsUpdate, h.DeletePWAIcon)
}

package appimages

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// RegisterAppImages registers application image routes using Huma.
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

	huma.Register(api, huma.Operation{
		OperationID: "get-favicon",
		Method:      http.MethodGet,
		Path:        "/app/images/favicon",
		Summary:     "Get application favicon",
		Description: "Get the application favicon image",
		Tags:        []string{"Application Images"},
		Security:    []map[string][]string{},
	}, h.GetFavicon)

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
}

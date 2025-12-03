package types

import "fmt"

// ImageEndpointAction represents the type of action to perform on the images API endpoint.
// It is used to route requests to the appropriate API path.
type ImageEndpointAction string

// ImageEndpointAction constants define the available image API operations.
const (
	// ImageEndpointActionList lists all images.
	ImageEndpointActionList ImageEndpointAction = "list"
	// ImageEndpointActionGet retrieves details for a specific image.
	ImageEndpointActionGet ImageEndpointAction = "get"
	// ImageEndpointActionPull pulls an image from a registry.
	ImageEndpointActionPull ImageEndpointAction = "pull"
	// ImageEndpointActionDelete removes an image.
	ImageEndpointActionDelete ImageEndpointAction = "delete"
	// ImageEndpointActionPrune removes unused images.
	ImageEndpointActionPrune ImageEndpointAction = "prune"
	// ImageEndpointActionCounts retrieves image usage statistics.
	ImageEndpointActionCounts ImageEndpointAction = "counts"
	// ImageEndpointActionUpload uploads an image from a tar archive.
	ImageEndpointActionUpload ImageEndpointAction = "upload"
)

// ArcaneApiEndpoints holds the API endpoint path templates for the Arcane API.
// Endpoint paths may contain format specifiers (e.g., %s) for environment IDs.
type ArcaneApiEndpoints struct {
	VersionEndpoint    string
	ContainersEndpoint string
	ImagesEndpoint     string
}

// Endpoints contains the defined API endpoints
var Endpoints = ArcaneApiEndpoints{
	VersionEndpoint:    "/api/app-version",
	ContainersEndpoint: "/api/environments/%s/containers",
	ImagesEndpoint:     "/api/environments/%s/images",
}

// FormatContainers returns the containers endpoint path for the given environment ID.
// The returned path is suitable for listing and managing containers.
func (e ArcaneApiEndpoints) FormatContainers(envID string) string {
	return fmt.Sprintf(e.ContainersEndpoint, envID)
}

// UseImageEndpoint returns the appropriate API endpoint path for the given
// image action and environment ID. It handles routing to different image
// API endpoints based on the requested action.
func (e ArcaneApiEndpoints) UseImageEndpoint(action ImageEndpointAction, envID string) string {
	switch action {
	case ImageEndpointActionList:
		return fmt.Sprintf(e.ImagesEndpoint, envID)
	case ImageEndpointActionGet:
		return fmt.Sprintf(e.ImagesEndpoint, envID)
	case ImageEndpointActionPull:
		return fmt.Sprintf(e.ImagesEndpoint+"/pull", envID)
	case ImageEndpointActionDelete:
		return fmt.Sprintf(e.ImagesEndpoint, envID)
	case ImageEndpointActionPrune:
		return fmt.Sprintf(e.ImagesEndpoint+"/prune", envID)
	case ImageEndpointActionCounts:
		return fmt.Sprintf(e.ImagesEndpoint+"/counts", envID)
	case ImageEndpointActionUpload:
		return fmt.Sprintf(e.ImagesEndpoint+"/upload", envID)
	default:
		return ""
	}
}

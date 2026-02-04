package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/vulnerability"
)

// VulnerabilityHandler provides Huma-based vulnerability scanning endpoints.
type VulnerabilityHandler struct {
	vulnerabilityService *services.VulnerabilityService
}

// --- Huma Input/Output Types ---

type ScanImageInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID to scan"`
}

type ScanImageOutput struct {
	Body base.ApiResponse[vulnerability.ScanResult]
}

type GetScanResultInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type GetScanResultOutput struct {
	Body base.ApiResponse[vulnerability.ScanResult]
}

type GetScanSummaryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
}

type GetScanSummaryOutput struct {
	Body base.ApiResponse[vulnerability.ScanSummary]
}

type GetScannerStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ScannerStatus struct {
	// Available indicates if the vulnerability scanner (Trivy) is available
	Available bool `json:"available"`

	// Version is the version of the scanner if available
	Version string `json:"version,omitempty"`
}

type GetScannerStatusOutput struct {
	Body base.ApiResponse[ScannerStatus]
}

// RegisterVulnerability registers vulnerability scanning routes using Huma.
func RegisterVulnerability(api huma.API, vulnerabilityService *services.VulnerabilityService) {
	h := &VulnerabilityHandler{
		vulnerabilityService: vulnerabilityService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "scan-image-vulnerabilities",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/{imageId}/vulnerabilities/scan",
		Summary:     "Scan image for vulnerabilities",
		Description: "Initiates a vulnerability scan for the specified image using Trivy",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ScanImage)

	huma.Register(api, huma.Operation{
		OperationID: "get-image-vulnerabilities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}/vulnerabilities",
		Summary:     "Get vulnerability scan result",
		Description: "Retrieves the most recent vulnerability scan result for an image",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetScanResult)

	huma.Register(api, huma.Operation{
		OperationID: "get-image-vulnerability-summary",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}/vulnerabilities/summary",
		Summary:     "Get vulnerability scan summary",
		Description: "Retrieves just the summary of vulnerabilities for an image (for list views)",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetScanSummary)

	huma.Register(api, huma.Operation{
		OperationID: "get-vulnerability-scanner-status",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/vulnerabilities/scanner-status",
		Summary:     "Get vulnerability scanner status",
		Description: "Check if the vulnerability scanner (Trivy) is available and get its version",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetScannerStatus)
}

// ScanImage initiates a vulnerability scan for an image.
func (h *VulnerabilityHandler) ScanImage(ctx context.Context, input *ScanImageInput) (*ScanImageOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	result, err := h.vulnerabilityService.ScanImage(ctx, input.ImageID, *user)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanError{Err: err}).Error())
	}

	return &ScanImageOutput{
		Body: base.ApiResponse[vulnerability.ScanResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// GetScanResult retrieves the vulnerability scan result for an image.
func (h *VulnerabilityHandler) GetScanResult(ctx context.Context, input *GetScanResultInput) (*GetScanResultOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	result, err := h.vulnerabilityService.GetScanResult(ctx, input.ImageID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanRetrievalError{Err: err}).Error())
	}

	if result == nil {
		return nil, huma.Error404NotFound((&common.VulnerabilityScanNotFoundError{}).Error())
	}

	return &GetScanResultOutput{
		Body: base.ApiResponse[vulnerability.ScanResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}

// GetScanSummary retrieves just the vulnerability summary for an image.
func (h *VulnerabilityHandler) GetScanSummary(ctx context.Context, input *GetScanSummaryInput) (*GetScanSummaryOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	summary, err := h.vulnerabilityService.GetScanSummary(ctx, input.ImageID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanRetrievalError{Err: err}).Error())
	}

	if summary == nil {
		return nil, huma.Error404NotFound((&common.VulnerabilityScanNotFoundError{}).Error())
	}

	return &GetScanSummaryOutput{
		Body: base.ApiResponse[vulnerability.ScanSummary]{
			Success: true,
			Data:    *summary,
		},
	}, nil
}

// GetScannerStatus checks if the vulnerability scanner is available.
func (h *VulnerabilityHandler) GetScannerStatus(ctx context.Context, input *GetScannerStatusInput) (*GetScannerStatusOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	version := h.vulnerabilityService.GetTrivyVersion()
	available := version != ""

	return &GetScannerStatusOutput{
		Body: base.ApiResponse[ScannerStatus]{
			Success: true,
			Data: ScannerStatus{
				Available: available,
				Version:   version,
			},
		},
	}, nil
}

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

type GetScanSummariesInput struct {
	EnvironmentID string                             `path:"id" doc:"Environment ID"`
	Body          vulnerability.ScanSummariesRequest `doc:"Batch scan summary request"`
}

type GetScanSummariesOutput struct {
	Body base.ApiResponse[vulnerability.ScanSummariesResponse]
}

type ListImageVulnerabilitiesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ImageID       string `path:"imageId" doc:"Image ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Sort field"`
	Order         string `query:"order" doc:"Sort order"`
	Start         int    `query:"start" doc:"Start offset"`
	Limit         int    `query:"limit" doc:"Limit"`
	Page          int    `query:"page" doc:"Page number"`
	Severity      string `query:"severity" doc:"Comma-separated severity filter"`
}

type ListImageVulnerabilitiesOutput struct {
	Body base.Paginated[vulnerability.Vulnerability]
}

type GetEnvironmentSummaryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentSummaryOutput struct {
	Body base.ApiResponse[vulnerability.EnvironmentVulnerabilitySummary]
}

type ListAllVulnerabilitiesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Sort field"`
	Order         string `query:"order" doc:"Sort order"`
	Start         int    `query:"start" doc:"Start offset"`
	Limit         int    `query:"limit" doc:"Limit"`
	Page          int    `query:"page" doc:"Page number"`
	Severity      string `query:"severity" doc:"Comma-separated severity filter"`
	ImageName     string `query:"imageName" doc:"Filter by image/repo name (substring)"`
}

type ListAllVulnerabilitiesOutput struct {
	Body base.Paginated[vulnerability.VulnerabilityWithImage]
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
		OperationID: "get-image-vulnerability-summaries",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/images/vulnerabilities/summaries",
		Summary:     "Get vulnerability scan summaries",
		Description: "Retrieves scan summaries for a list of images (batch)",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetScanSummaries)

	huma.Register(api, huma.Operation{
		OperationID: "list-image-vulnerabilities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}/vulnerabilities/list",
		Summary:     "List image vulnerabilities",
		Description: "Retrieves paginated vulnerabilities for an image",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListImageVulnerabilities)

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

	huma.Register(api, huma.Operation{
		OperationID: "get-environment-vulnerability-summary",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/vulnerabilities/summary",
		Summary:     "Get environment vulnerability summary",
		Description: "Retrieves aggregated vulnerability counts across all images in the environment",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetEnvironmentSummary)

	huma.Register(api, huma.Operation{
		OperationID: "list-environment-vulnerabilities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/vulnerabilities/all",
		Summary:     "List environment vulnerabilities",
		Description: "Retrieves paginated vulnerabilities across all scanned images in the environment",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListAllVulnerabilities)

	huma.Register(api, huma.Operation{
		OperationID: "ignore-vulnerability",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/vulnerabilities/ignore",
		Summary:     "Ignore a vulnerability",
		Description: "Creates an ignore record for a specific vulnerability",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.IgnoreVulnerability)

	huma.Register(api, huma.Operation{
		OperationID: "unignore-vulnerability",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/vulnerabilities/ignore/{ignoreId}",
		Summary:     "Unignore a vulnerability",
		Description: "Removes an ignore record for a vulnerability",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UnignoreVulnerability)

	huma.Register(api, huma.Operation{
		OperationID: "list-ignored-vulnerabilities",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/vulnerabilities/ignored",
		Summary:     "List ignored vulnerabilities",
		Description: "Retrieves a list of all ignored vulnerabilities for the environment",
		Tags:        []string{"Vulnerabilities"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.ListIgnoredVulnerabilities)
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

	result, err := h.vulnerabilityService.ScanImage(ctx, input.EnvironmentID, input.ImageID, *user)
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

// GetScanSummaries retrieves scan summaries for a list of image IDs.
func (h *VulnerabilityHandler) GetScanSummaries(ctx context.Context, input *GetScanSummariesInput) (*GetScanSummariesOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	imageIDs := input.Body.ImageIDs
	if len(imageIDs) == 0 {
		return &GetScanSummariesOutput{
			Body: base.ApiResponse[vulnerability.ScanSummariesResponse]{
				Success: true,
				Data: vulnerability.ScanSummariesResponse{
					Summaries: map[string]*vulnerability.ScanSummary{},
				},
			},
		}, nil
	}

	summaries, err := h.vulnerabilityService.GetScanSummariesByImageIDs(ctx, imageIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanError{Err: err}).Error())
	}

	return &GetScanSummariesOutput{
		Body: base.ApiResponse[vulnerability.ScanSummariesResponse]{
			Success: true,
			Data: vulnerability.ScanSummariesResponse{
				Summaries: summaries,
			},
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

// ListImageVulnerabilities returns a paginated list of vulnerabilities for an image.
func (h *VulnerabilityHandler) ListImageVulnerabilities(ctx context.Context, input *ListImageVulnerabilitiesInput) (*ListImageVulnerabilitiesOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if params.Limit == 0 {
		params.Limit = 20
	}
	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}

	items, paginationResp, err := h.vulnerabilityService.ListVulnerabilities(ctx, input.ImageID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanRetrievalError{Err: err}).Error())
	}

	if items == nil {
		items = []vulnerability.Vulnerability{}
	}

	return &ListImageVulnerabilitiesOutput{
		Body: base.Paginated[vulnerability.Vulnerability]{
			Success: true,
			Data:    items,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// GetEnvironmentSummary returns aggregated vulnerability info for the current environment.
func (h *VulnerabilityHandler) GetEnvironmentSummary(ctx context.Context, input *GetEnvironmentSummaryInput) (*GetEnvironmentSummaryOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	summary, err := h.vulnerabilityService.GetEnvironmentSummary(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanRetrievalError{Err: err}).Error())
	}

	if summary == nil {
		summary = &vulnerability.EnvironmentVulnerabilitySummary{}
	}

	return &GetEnvironmentSummaryOutput{
		Body: base.ApiResponse[vulnerability.EnvironmentVulnerabilitySummary]{
			Success: true,
			Data:    *summary,
		},
	}, nil
}

// ListAllVulnerabilities returns a paginated list of vulnerabilities across all images.
func (h *VulnerabilityHandler) ListAllVulnerabilities(ctx context.Context, input *ListAllVulnerabilitiesInput) (*ListAllVulnerabilitiesOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if params.Limit == 0 {
		params.Limit = 20
	}
	if input.Severity != "" {
		params.Filters["severity"] = input.Severity
	}
	if input.ImageName != "" {
		params.Filters["imageName"] = input.ImageName
	}

	items, paginationResp, err := h.vulnerabilityService.ListAllVulnerabilities(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.VulnerabilityScanRetrievalError{Err: err}).Error())
	}

	if items == nil {
		items = []vulnerability.VulnerabilityWithImage{}
	}

	return &ListAllVulnerabilitiesOutput{
		Body: base.Paginated[vulnerability.VulnerabilityWithImage]{
			Success: true,
			Data:    items,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// GetScannerStatus checks if the vulnerability scanner is available.
func (h *VulnerabilityHandler) GetScannerStatus(ctx context.Context, input *GetScannerStatusInput) (*GetScannerStatusOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	version := h.vulnerabilityService.GetTrivyVersion(ctx)
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

type IgnoreVulnerabilityInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          vulnerability.IgnorePayload
}

type IgnoreVulnerabilityOutput struct {
	Body base.ApiResponse[vulnerability.IgnoredVulnerability]
}

// IgnoreVulnerability creates an ignore record for a vulnerability.
func (h *VulnerabilityHandler) IgnoreVulnerability(ctx context.Context, input *IgnoreVulnerabilityInput) (*IgnoreVulnerabilityOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	payload := &input.Body
	payload.CreatedBy = user.ID

	ignore, err := h.vulnerabilityService.IgnoreVulnerability(ctx, input.EnvironmentID, payload)
	if err != nil {
		if err.Error() == "vulnerability is already ignored" {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &IgnoreVulnerabilityOutput{
		Body: base.ApiResponse[vulnerability.IgnoredVulnerability]{
			Success: true,
			Data: vulnerability.IgnoredVulnerability{
				ID:               ignore.ID,
				EnvironmentID:    ignore.EnvironmentID,
				ImageID:          ignore.ImageID,
				VulnerabilityID:  ignore.VulnerabilityID,
				PkgName:          ignore.PkgName,
				InstalledVersion: ignore.InstalledVersion,
				Reason:           ignore.Reason,
				CreatedBy:        ignore.CreatedBy,
				CreatedAt:        ignore.CreatedAt,
			},
		},
	}, nil
}

type UnignoreVulnerabilityInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	IgnoreID      string `path:"ignoreId" doc:"Ignore record ID"`
}

type UnignoreVulnerabilityOutput struct {
	Body base.ApiResponse[struct{}]
}

// UnignoreVulnerability removes an ignore record for a vulnerability.
func (h *VulnerabilityHandler) UnignoreVulnerability(ctx context.Context, input *UnignoreVulnerabilityInput) (*UnignoreVulnerabilityOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.vulnerabilityService.UnignoreVulnerability(ctx, input.EnvironmentID, input.IgnoreID); err != nil {
		if err.Error() == "ignore record not found" {
			return nil, huma.Error404NotFound(err.Error())
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &UnignoreVulnerabilityOutput{
		Body: base.ApiResponse[struct{}]{
			Success: true,
		},
	}, nil
}

type ListIgnoredVulnerabilitiesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Sort field"`
	Order         string `query:"order" doc:"Sort order"`
	Start         int    `query:"start" doc:"Start offset"`
	Limit         int    `query:"limit" doc:"Limit"`
	Page          int    `query:"page" doc:"Page number"`
}

type ListIgnoredVulnerabilitiesOutput struct {
	Body base.Paginated[vulnerability.IgnoredVulnerability]
}

// ListIgnoredVulnerabilities returns a list of ignored vulnerabilities.
func (h *VulnerabilityHandler) ListIgnoredVulnerabilities(ctx context.Context, input *ListIgnoredVulnerabilitiesInput) (*ListIgnoredVulnerabilitiesOutput, error) {
	if h.vulnerabilityService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if params.Limit == 0 {
		params.Limit = 20
	}

	items, paginationResp, err := h.vulnerabilityService.ListIgnoredVulnerabilities(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	if items == nil {
		items = []vulnerability.IgnoredVulnerability{}
	}

	return &ListIgnoredVulnerabilitiesOutput{
		Body: base.Paginated[vulnerability.IgnoredVulnerability]{
			Success: true,
			Data:    items,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

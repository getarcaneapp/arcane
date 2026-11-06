package types

import (
	"time"
)

type (
	PatchMode string

	PatchStatus string

	PatchOptions struct {
		Suffix     string `json:"suffix,omitempty" doc:"Suffix appended to the source tag for the patched image"`
		PatchedTag string `json:"patchedTag,omitempty" doc:"Explicit tag for the patched image"`
		// When empty, all outdated OS packages are updated.
		ScanID         string `json:"scanId,omitempty" doc:"Vulnerability scan ID to patch from; empty patches all outdated packages"`
		TimeoutSeconds int    `json:"timeoutSeconds,omitempty" doc:"Timeout for the patch operation in seconds"`
		IgnoreErrors   bool   `json:"ignoreErrors,omitempty" doc:"Continue patching when individual package updates fail"`
	}

	PatchScanSummary struct {
		ScanTime     time.Time `json:"scanTime"`
		Status       string    `json:"status"`
		FixableCount int       `json:"fixableCount"`
		TotalCount   int       `json:"totalCount"`
	}

	PatchTarget struct {
		ScanTime      time.Time         `json:"scanTime"`
		LastPatch     *PatchRecord      `json:"lastPatch,omitempty"`
		LastPatchScan *PatchScanSummary `json:"lastPatchScan,omitempty"`
		EnvironmentTag

		ImageID      string `json:"imageId"`
		ImageRef     string `json:"imageRef"`
		FixableCount int    `json:"fixableCount"`
		TotalCount   int    `json:"totalCount"`
		LocalOnly    bool   `json:"localOnly,omitempty"`
	}

	PatchRecord struct {
		CreatedAt       time.Time   `json:"createdAt"`
		PackagesUpdated *int        `json:"packagesUpdated,omitempty"`
		Error           *string     `json:"error,omitempty"`
		ActivityID      *string     `json:"activityId,omitempty"`
		DurationMs      *int64      `json:"durationMs,omitempty"`
		UpdatedAt       *time.Time  `json:"updatedAt,omitempty"`
		ID              string      `json:"id"`
		EnvironmentID   string      `json:"environmentId"`
		OriginalImageID string      `json:"originalImageId"`
		OriginalRef     string      `json:"originalRef"`
		OriginalDigest  string      `json:"originalDigest,omitempty"`
		PatchedRef      string      `json:"patchedRef"`
		Mode            PatchMode   `json:"mode"`
		Status          PatchStatus `json:"status"`
	}
)

const (
	PatchModeUpdateAll   PatchMode   = "update-all"
	PatchModeReport      PatchMode   = "report"
	PatchStatusPatching  PatchStatus = "patching"
	PatchStatusCompleted PatchStatus = "completed"
	PatchStatusFailed    PatchStatus = "failed"
)

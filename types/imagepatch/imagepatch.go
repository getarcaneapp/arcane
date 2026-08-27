// Package imagepatch contains shared types for Copacetic-based image patching.
package imagepatch

import "time"

// PatchMode describes how package updates for a patch run were determined.
type PatchMode string

const (
	// PatchModeUpdateAll patches every outdated OS package in the image.
	PatchModeUpdateAll PatchMode = "update-all"
	// PatchModeReport patches only packages flagged by a vulnerability scan report.
	PatchModeReport PatchMode = "report"
)

// PatchStatus is the lifecycle state of a patch run.
type PatchStatus string

const (
	PatchStatusPatching  PatchStatus = "patching"
	PatchStatusCompleted PatchStatus = "completed"
	PatchStatusFailed    PatchStatus = "failed"
)

// PatchOptions are the caller-provided options for patching an image.
type PatchOptions struct {
	// Suffix overrides the configured patched-tag suffix (e.g. "patched").
	Suffix string `json:"suffix,omitempty" doc:"Suffix appended to the source tag for the patched image"`
	// PatchedTag fully overrides the patched tag (takes precedence over Suffix).
	PatchedTag string `json:"patchedTag,omitempty" doc:"Explicit tag for the patched image"`
	// TimeoutSeconds overrides the configured patch timeout.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty" doc:"Timeout for the patch operation in seconds"`
	// ScanID selects a stored vulnerability scan whose report drives the patch.
	// When empty, all outdated OS packages are updated.
	ScanID string `json:"scanId,omitempty" doc:"Vulnerability scan ID to patch from; empty patches all outdated packages"`
	// IgnoreErrors continues patching remaining packages when one fails.
	IgnoreErrors bool `json:"ignoreErrors,omitempty" doc:"Continue patching when individual package updates fail"`
}

// PatchScanSummary is the scan outcome of a patched image, used to verify a
// patch actually removed the fixable vulnerabilities.
type PatchScanSummary struct {
	Status       string    `json:"status"`
	FixableCount int       `json:"fixableCount"`
	TotalCount   int       `json:"totalCount"`
	ScanTime     time.Time `json:"scanTime"`
}

// PatchTarget describes a scanned image from the patching point of view: how
// many of its vulnerabilities are fixable and what the latest patch run did.
type PatchTarget struct {
	ImageID      string    `json:"imageId"`
	ImageRef     string    `json:"imageRef"`
	FixableCount int       `json:"fixableCount"`
	TotalCount   int       `json:"totalCount"`
	ScanTime     time.Time `json:"scanTime"`
	// LocalOnly marks images that were built locally and never pushed or
	// pulled; they have no registry source, so they cannot be patched.
	LocalOnly     bool              `json:"localOnly,omitempty"`
	LastPatch     *PatchRecord      `json:"lastPatch,omitempty"`
	LastPatchScan *PatchScanSummary `json:"lastPatchScan,omitempty"`
}

// PatchRecord describes one image patch run.
type PatchRecord struct {
	ID              string      `json:"id"`
	EnvironmentID   string      `json:"environmentId"`
	OriginalImageID string      `json:"originalImageId"`
	OriginalRef     string      `json:"originalRef"`
	OriginalDigest  string      `json:"originalDigest,omitempty"`
	PatchedRef      string      `json:"patchedRef"`
	Mode            PatchMode   `json:"mode"`
	Status          PatchStatus `json:"status"`
	PackagesUpdated *int        `json:"packagesUpdated,omitempty"`
	Error           *string     `json:"error,omitempty"`
	ActivityID      *string     `json:"activityId,omitempty"`
	DurationMs      *int64      `json:"durationMs,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       *time.Time  `json:"updatedAt,omitempty"`
}

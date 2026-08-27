package imagepatch

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/types/v2/imagepatch"
)

// ImagePatchRecord stores one Copacetic image patch run.
type ImagePatchRecord struct {
	database.BaseModel

	EnvironmentID   string  `json:"environmentId" gorm:"column:environment_id;index:idx_image_patches_env_image"`
	OriginalImageID string  `json:"originalImageId" gorm:"column:original_image_id;index:idx_image_patches_env_image"`
	OriginalRef     string  `json:"originalRef" gorm:"column:original_ref"`
	OriginalDigest  string  `json:"originalDigest,omitempty" gorm:"column:original_digest"`
	PatchedRef      string  `json:"patchedRef" gorm:"column:patched_ref"`
	Mode            string  `json:"mode" gorm:"column:mode"`
	Status          string  `json:"status" gorm:"column:status" sortable:"true"`
	PackagesUpdated *int    `json:"packagesUpdated,omitempty" gorm:"column:packages_updated"`
	Error           *string `json:"error,omitempty" gorm:"column:error"`
	ActivityID      *string `json:"activityId,omitempty" gorm:"column:activity_id"`
	DurationMs      *int64  `json:"durationMs,omitempty" gorm:"column:duration_ms"`
}

func (r *ImagePatchRecord) TableName() string {
	return "image_patches"
}

func (r *ImagePatchRecord) ToDto() imagepatch.PatchRecord {
	return imagepatch.PatchRecord{
		ID:              r.ID,
		EnvironmentID:   r.EnvironmentID,
		OriginalImageID: r.OriginalImageID,
		OriginalRef:     r.OriginalRef,
		OriginalDigest:  r.OriginalDigest,
		PatchedRef:      r.PatchedRef,
		Mode:            imagepatch.PatchMode(r.Mode),
		Status:          imagepatch.PatchStatus(r.Status),
		PackagesUpdated: r.PackagesUpdated,
		Error:           r.Error,
		ActivityID:      r.ActivityID,
		DurationMs:      r.DurationMs,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

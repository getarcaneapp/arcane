package models

import "time"

type ImageBuildStatus string

const (
	ImageBuildStatusRunning ImageBuildStatus = "running"
	ImageBuildStatusSuccess ImageBuildStatus = "success"
	ImageBuildStatusFailed  ImageBuildStatus = "failed"
)

type ImageBuild struct {
	EnvironmentID   string           `json:"environmentId" gorm:"column:environment_id;index"`
	UserID          *string          `json:"userId,omitempty" gorm:"column:user_id"`
	Username        *string          `json:"username,omitempty" gorm:"column:username"`
	Status          ImageBuildStatus `json:"status" gorm:"column:status;index" sortable:"true"`
	Provider        string           `json:"provider,omitempty" gorm:"column:provider"`
	ContextDir      string           `json:"contextDir" gorm:"column:context_dir"`
	Dockerfile      string           `json:"dockerfile,omitempty" gorm:"column:dockerfile"`
	Target          string           `json:"target,omitempty" gorm:"column:target"`
	Tags            StringSlice      `json:"tags,omitempty" gorm:"column:tags;type:text"`
	Platforms       StringSlice      `json:"platforms,omitempty" gorm:"column:platforms;type:text"`
	BuildArgs       JSON             `json:"buildArgs,omitempty" gorm:"column:build_args;type:text"`
	Push            bool             `json:"push" gorm:"column:push"`
	Load            bool             `json:"load" gorm:"column:load"`
	Digest          *string          `json:"digest,omitempty" gorm:"column:digest"`
	ErrorMessage    *string          `json:"errorMessage,omitempty" gorm:"column:error_message"`
	Output          *string          `json:"output,omitempty" gorm:"column:output;type:text"`
	OutputTruncated bool             `json:"outputTruncated" gorm:"column:output_truncated;default:false"`
	CompletedAt     *time.Time       `json:"completedAt,omitempty" gorm:"column:completed_at" sortable:"true"`
	DurationMs      *int64           `json:"durationMs,omitempty" gorm:"column:duration_ms"`
	BaseModel
}

func (ImageBuild) TableName() string {
	return "image_builds"
}

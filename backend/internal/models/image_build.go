package models

import "time"

type ImageBuildStatus string

const (
	ImageBuildStatusRunning ImageBuildStatus = "running"
	ImageBuildStatusSuccess ImageBuildStatus = "success"
	ImageBuildStatusFailed  ImageBuildStatus = "failed"
)

type ImageBuild struct {
	BaseModel

	BuildArgs       JSON             `json:"buildArgs,omitempty" gorm:"column:build_args;type:text"`
	Labels          JSON             `json:"labels,omitempty" gorm:"column:labels;type:text"`
	Ulimits         JSON             `json:"ulimits,omitempty" gorm:"column:ulimits;type:text"`
	UserID          *string          `json:"userId,omitempty" gorm:"column:user_id"`
	Username        *string          `json:"username,omitempty" gorm:"column:username"`
	Digest          *string          `json:"digest,omitempty" gorm:"column:digest"`
	ErrorMessage    *string          `json:"errorMessage,omitempty" gorm:"column:error_message"`
	Output          *string          `json:"output,omitempty" gorm:"column:output;type:text"`
	CompletedAt     *time.Time       `json:"completedAt,omitempty" gorm:"column:completed_at" sortable:"true"`
	DurationMs      *int64           `json:"durationMs,omitempty" gorm:"column:duration_ms"`
	EnvironmentID   string           `json:"environmentId" gorm:"column:environment_id;index"`
	Status          ImageBuildStatus `json:"status" gorm:"column:status;index" sortable:"true"`
	Provider        string           `json:"provider,omitempty" gorm:"column:provider"`
	ContextDir      string           `json:"contextDir" gorm:"column:context_dir"`
	Dockerfile      string           `json:"dockerfile,omitempty" gorm:"column:dockerfile"`
	Target          string           `json:"target,omitempty" gorm:"column:target"`
	BuildNetwork    string           `json:"network,omitempty" gorm:"column:build_network"`
	Isolation       string           `json:"isolation,omitempty" gorm:"column:isolation"`
	Tags            StringSlice      `json:"tags,omitempty" gorm:"column:tags;type:text"`
	Platforms       StringSlice      `json:"platforms,omitempty" gorm:"column:platforms;type:text"`
	CacheFrom       StringSlice      `json:"cacheFrom,omitempty" gorm:"column:cache_from;type:text"`
	CacheTo         StringSlice      `json:"cacheTo,omitempty" gorm:"column:cache_to;type:text"`
	Entitlements    StringSlice      `json:"entitlements,omitempty" gorm:"column:entitlements;type:text"`
	ExtraHosts      StringSlice      `json:"extraHosts,omitempty" gorm:"column:extra_hosts;type:text"`
	ShmSize         int64            `json:"shmSize,omitempty" gorm:"column:shm_size"`
	NoCache         bool             `json:"noCache" gorm:"column:no_cache;default:false"`
	Pull            bool             `json:"pull" gorm:"column:pull;default:false"`
	Privileged      bool             `json:"privileged" gorm:"column:privileged;default:false"`
	Push            bool             `json:"push" gorm:"column:push"`
	Load            bool             `json:"load" gorm:"column:load"`
	OutputTruncated bool             `json:"outputTruncated" gorm:"column:output_truncated;default:false"`
}

func (ImageBuild) TableName() string {
	return "image_builds"
}

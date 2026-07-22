package image

import "time"

// BuildRecord represents a historical image build entry.
type BuildRecord struct {
	CreatedAt       time.Time         `json:"createdAt" sortable:"true"`
	BuildArgs       map[string]string `json:"buildArgs,omitempty"`
	UserID          *string           `json:"userId,omitempty"`
	Username        *string           `json:"username,omitempty"`
	Digest          *string           `json:"digest,omitempty"`
	ErrorMessage    *string           `json:"errorMessage,omitempty"`
	Output          *string           `json:"output,omitempty"`
	CompletedAt     *time.Time        `json:"completedAt,omitempty" sortable:"true"`
	DurationMs      *int64            `json:"durationMs,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Ulimits         map[string]string `json:"ulimits,omitempty"`
	ID              string            `json:"id" sortable:"true"`
	EnvironmentID   string            `json:"environmentId"`
	Status          string            `json:"status" sortable:"true"`
	Provider        string            `json:"provider,omitempty"`
	ContextDir      string            `json:"contextDir"`
	Dockerfile      string            `json:"dockerfile,omitempty"`
	Target          string            `json:"target,omitempty"`
	Network         string            `json:"network,omitempty"`
	Isolation       string            `json:"isolation,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Platforms       []string          `json:"platforms,omitempty"`
	CacheFrom       []string          `json:"cacheFrom,omitempty"`
	CacheTo         []string          `json:"cacheTo,omitempty"`
	Entitlements    []string          `json:"entitlements,omitempty"`
	ExtraHosts      []string          `json:"extraHosts,omitempty"`
	ShmSize         int64             `json:"shmSize,omitempty"`
	NoCache         bool              `json:"noCache"`
	Pull            bool              `json:"pull"`
	Privileged      bool              `json:"privileged"`
	Push            bool              `json:"push"`
	Load            bool              `json:"load"`
	OutputTruncated bool              `json:"outputTruncated"`
}

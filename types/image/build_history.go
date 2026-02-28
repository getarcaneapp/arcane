package image

import "time"

// BuildRecord represents a historical image build entry.
type BuildRecord struct {
	ID              string            `json:"id" sortable:"true"`
	EnvironmentID   string            `json:"environmentId"`
	UserID          *string           `json:"userId,omitempty"`
	Username        *string           `json:"username,omitempty"`
	Status          string            `json:"status" sortable:"true"`
	Provider        string            `json:"provider,omitempty"`
	ContextDir      string            `json:"contextDir"`
	Dockerfile      string            `json:"dockerfile,omitempty"`
	Target          string            `json:"target,omitempty"`
	Tags            []string          `json:"tags,omitempty"`
	Platforms       []string          `json:"platforms,omitempty"`
	BuildArgs       map[string]string `json:"buildArgs,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	CacheFrom       []string          `json:"cacheFrom,omitempty"`
	CacheTo         []string          `json:"cacheTo,omitempty"`
	NoCache         bool              `json:"noCache"`
	Pull            bool              `json:"pull"`
	Network         string            `json:"network,omitempty"`
	Isolation       string            `json:"isolation,omitempty"`
	ShmSize         int64             `json:"shmSize,omitempty"`
	Ulimits         map[string]string `json:"ulimits,omitempty"`
	Entitlements    []string          `json:"entitlements,omitempty"`
	Privileged      bool              `json:"privileged"`
	ExtraHosts      []string          `json:"extraHosts,omitempty"`
	Push            bool              `json:"push"`
	Load            bool              `json:"load"`
	Digest          *string           `json:"digest,omitempty"`
	ErrorMessage    *string           `json:"errorMessage,omitempty"`
	Output          *string           `json:"output,omitempty"`
	OutputTruncated bool              `json:"outputTruncated"`
	CompletedAt     *time.Time        `json:"completedAt,omitempty" sortable:"true"`
	DurationMs      *int64            `json:"durationMs,omitempty"`
	CreatedAt       time.Time         `json:"createdAt" sortable:"true"`
}

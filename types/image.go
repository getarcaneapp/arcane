package types

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/moby/moby/api/types/image"
)

// ImageListResponse is the paginated image list together with the upload limit the
// environment enforces, so list consumers need no settings access.
type ImageListResponse struct {
	Success    bool               `json:"success" doc:"Whether the request was successful"`
	Data       []ImageSummary     `json:"data" doc:"Array of items for the current page"`
	Pagination PaginationResponse `json:"pagination" doc:"Pagination metadata"`

	// MaxImageUploadSize is the maximum image archive upload size in MB.
	//
	// Required: true
	MaxImageUploadSize int `json:"maxImageUploadSize" doc:"Maximum image archive upload size in MB"`

	FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
}

type ImageUpdateInfo struct {
	// CheckTime is the time when the update check was performed.
	//
	// Required: true
	CheckTime time.Time `json:"checkTime"`

	// UpdateType describes the type of update (e.g., major, minor, patch).
	//
	// Required: true
	UpdateType string `json:"updateType"`

	// CurrentVersion is the current version of the image.
	//
	// Required: true
	CurrentVersion string `json:"currentVersion"`

	// LatestVersion is the latest available version of the image.
	//
	// Required: true
	LatestVersion string `json:"latestVersion"`

	// CurrentDigest is the digest (hash) of the current image.
	//
	// Required: true
	CurrentDigest string `json:"currentDigest"`

	// LatestDigest is the digest (hash) of the latest available image.
	//
	// Required: true
	LatestDigest string `json:"latestDigest"`

	// Error contains any error message from the update check.
	//
	// Required: true
	Error string `json:"error"`

	// AuthMethod is the authentication method used.
	//
	// Required: false
	AuthMethod string `json:"authMethod,omitempty"`

	// AuthUsername is the username used for authentication.
	//
	// Required: false
	AuthUsername string `json:"authUsername,omitempty"`

	// AuthRegistry is the registry used for authentication.
	//
	// Required: false
	AuthRegistry string `json:"authRegistry,omitempty"`

	// ResponseTimeMs is the response time in milliseconds.
	//
	// Required: true
	ResponseTimeMs int `json:"responseTimeMs"`

	// HasUpdate indicates if an update is available for the image.
	//
	// Required: true
	HasUpdate bool `json:"hasUpdate"`

	// UsedCredential indicates if credentials were used for the update check.
	//
	// Required: false
	UsedCredential bool `json:"usedCredential,omitempty"`
}

type ImageUsedBy struct {
	// Type indicates the usage source (e.g., project, container).
	//
	// Required: true
	Type string `json:"type"`

	// Name is the project or container name.
	//
	// Required: true
	Name string `json:"name"`

	// ID is the identifier of the project or container (if available).
	//
	// Required: false
	ID string `json:"id,omitempty"`
}

type ImageSummary struct {
	EnvironmentTag

	// ID is the unique identifier of the image.
	//
	// Required: true
	ID string `json:"id" sortable:"true"`

	// RepoTags is a list of tag references, excluding digest-pinned names.
	//
	// Required: true
	RepoTags []string `json:"repoTags"`

	// RepoDigests is a list of content-addressable digests of the image.
	//
	// Required: true
	RepoDigests []string `json:"repoDigests"`

	// Created is the Unix timestamp when the image was created.
	//
	// Required: true
	Created int64 `json:"created" sortable:"true"`

	// Size is the total size of the image including all layers.
	//
	// Required: true
	Size int64 `json:"size" sortable:"true"`

	// VirtualSize is the virtual size of the image.
	//
	// Required: true
	VirtualSize int64 `json:"virtualSize"`

	// Labels contains user-defined metadata for the image.
	//
	// Required: true
	Labels map[string]any `json:"labels"`

	// InUse indicates if the image is currently in use by a container.
	//
	// Required: true
	InUse bool `json:"inUse" sortable:"true"`

	// UsedBy lists projects or containers currently using this image.
	//
	// Required: false
	UsedBy []ImageUsedBy `json:"usedBy,omitempty"`

	// Repo is the repository name of the image.
	//
	// Required: true
	Repo string `json:"repo" sortable:"true"`

	// Tag is the tag of the image.
	//
	// Required: true
	Tag string `json:"tag" sortable:"true"`

	// UpdateInfo contains information about available updates for the image.
	//
	// Required: false
	UpdateInfo *ImageUpdateInfo `json:"updateInfo,omitempty"`

	// VulnerabilityScan contains the latest vulnerability scan summary for the image.
	//
	// Required: false
	VulnerabilityScan *VulnerabilityScanSummary `json:"vulnerabilityScan,omitempty"`

	// PinnedReferences contains exact digest-pinned references used by containers.
	//
	// Required: false
	PinnedReferences []string `json:"pinnedReferences,omitempty"`
}

type ImageAttestationList struct {
	ImageRef      string             `json:"imageRef"`
	SubjectDigest string             `json:"subjectDigest"`
	Platform      string             `json:"platform,omitempty"`
	Attestations  []ImageAttestation `json:"attestations"`
}

type ImageAttestation struct {
	Digest        string                    `json:"digest"`
	MediaType     string                    `json:"mediaType"`
	ArtifactType  string                    `json:"artifactType,omitempty"`
	PredicateType string                    `json:"predicateType"`
	StatementType string                    `json:"statementType,omitempty"`
	Subject       []ImageAttestationSubject `json:"subject"`
	Platform      string                    `json:"platform,omitempty"`
	Size          int64                     `json:"size"`
	Statement     json.RawMessage           `json:"statement,omitempty"`
}

type ImageAttestationSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type ImagePruneReport struct {
	// ImagesDeleted is a list of image IDs that were deleted.
	//
	// Required: true
	ImagesDeleted []string `json:"imagesDeleted"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed int64 `json:"spaceReclaimed"`
}

// ImageTagRequest is used to add a new repository tag to an existing image.
type ImageTagRequest struct {
	// Repository is the target repository name.
	//
	// Required: true
	Repository string `json:"repository" doc:"Target repository name"`

	// Tag is the target tag. Docker defaults to latest when omitted.
	//
	// Required: false
	Tag string `json:"tag,omitempty" doc:"Target tag"`
}

// ImageHistoryItem describes a single image history layer entry.
type ImageHistoryItem struct {
	ID        string   `json:"id"`
	Created   int64    `json:"created"`
	CreatedBy string   `json:"createdBy"`
	Tags      []string `json:"tags"`
	Size      int64    `json:"size"`
	Comment   string   `json:"comment"`
}

// ImageSearchResult describes an image registry search result.
type ImageSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"starCount"`
	Official    bool   `json:"official"`
	Automated   bool   `json:"automated"`
}

// NewImagePruneReport creates a PruneReport from a Docker image prune report.
// It extracts deleted and untagged image IDs from the Docker API response,
// combining both types into a single list and converting space reclaimed to int64.
func NewImagePruneReport(src image.PruneReport) ImagePruneReport {
	// Safely convert uint64 to int64, capping at MaxInt64 to prevent overflow
	var spaceReclaimed int64
	if src.SpaceReclaimed > uint64(math.MaxInt64) {
		spaceReclaimed = math.MaxInt64
	} else {
		spaceReclaimed = int64(src.SpaceReclaimed)
	}

	out := ImagePruneReport{
		ImagesDeleted:  make([]string, 0, len(src.ImagesDeleted)),
		SpaceReclaimed: spaceReclaimed,
	}
	for _, d := range src.ImagesDeleted {
		if d.Deleted != "" {
			out.ImagesDeleted = append(out.ImagesDeleted, d.Deleted)
		} else if d.Untagged != "" {
			out.ImagesDeleted = append(out.ImagesDeleted, d.Untagged)
		}
	}
	return out
}

type ImageUsageCounts struct {
	// Inuse is the number of images currently in use.
	//
	// Required: true
	Inuse int `json:"imagesInuse"`

	// Unused is the number of images not in use.
	//
	// Required: true
	Unused int `json:"imagesUnused"`

	// Total is the total number of images.
	//
	// Required: true
	Total int `json:"totalImages"`

	// TotalSize is the total size of all images in bytes.
	//
	// Required: true
	TotalSize int64 `json:"totalImageSize"`
}

type ImageLoadResult struct {
	// Stream contains the output stream from the load operation.
	//
	// Required: true
	Stream string `json:"stream"`
}

type ImageDetailSummary struct {
	// ID is the unique identifier of the image.
	//
	// Required: true
	ID string `json:"id"`

	// RepoTags is a list of tag references, excluding digest-pinned names.
	//
	// Required: true
	RepoTags []string `json:"repoTags"`

	// RepoDigests is a list of content-addressable digests of the image.
	//
	// Required: true
	RepoDigests []string `json:"repoDigests"`

	// PinnedReferences contains exact digest-pinned references used by containers.
	//
	// Required: false
	PinnedReferences []string `json:"pinnedReferences,omitempty"`

	// Comment is a comment associated with the image.
	//
	// Required: true
	Comment string `json:"comment"`

	// Created is the creation timestamp of the image.
	//
	// Required: true
	Created string `json:"created"`

	// Author is the author of the image.
	//
	// Required: true
	Author string `json:"author"`

	// Config contains the configuration of the image.
	//
	// Required: true
	Config struct {
		// ExposedPorts are the ports exposed by the image.
		ExposedPorts map[string]struct{} `json:"exposedPorts,omitempty"`
		// Env are the environment variables set in the image.
		Env []string `json:"env,omitempty"`
		// Cmd is the default command to run in the container.
		Cmd []string `json:"cmd,omitempty"`
		// Volumes are the volumes defined in the image.
		Volumes map[string]struct{} `json:"volumes,omitempty"`
		// WorkingDir is the working directory in the container.
		WorkingDir string `json:"workingDir,omitempty"`
		// ArgsEscaped indicates if the arguments are escaped.
		ArgsEscaped bool `json:"argsEscaped,omitempty"`
	} `json:"config"`

	// Architecture is the architecture for which the image was built.
	//
	// Required: true
	Architecture string `json:"architecture"`

	// Os is the operating system for which the image was built.
	//
	// Required: true
	Os string `json:"os"`

	// Size is the total size of the image.
	//
	// Required: true
	Size int64 `json:"size"`

	// GraphDriver contains information about the graph driver.
	//
	// Required: true
	GraphDriver struct {
		// Data contains driver-specific data.
		Data any `json:"data"`
		// Name is the name of the graph driver.
		Name string `json:"name"`
	} `json:"graphDriver"`

	// RootFs contains information about the root filesystem.
	//
	// Required: true
	RootFs struct {
		// Type is the type of the root filesystem.
		Type string `json:"type"`
		// Layers are the layers of the image.
		Layers []string `json:"layers"`
	} `json:"rootFs"`

	// Metadata contains metadata about the image.
	//
	// Required: true
	Metadata struct {
		// LastTagTime is the time when the image was last tagged.
		LastTagTime string `json:"lastTagTime"`
	} `json:"metadata"`

	// Descriptor is the OCI descriptor of the image.
	//
	// Required: true
	Descriptor struct {
		// MediaType is the media type of the descriptor.
		MediaType string `json:"mediaType"`
		// Digest is the digest of the descriptor.
		Digest string `json:"digest"`
		// Size is the size of the descriptor.
		Size int64 `json:"size"`
	} `json:"descriptor"`
}

// ImagePullOptions contains options for pulling an image.
type ImagePullOptions struct {
	// ImageName is the name of the image to pull.
	//
	// Required: true
	ImageName string `json:"imageName" minLength:"1" doc:"Name of the image to pull (e.g., nginx)"`

	// Tag is the tag of the image to pull. Defaults to 'latest'.
	//
	// Required: false
	Tag string `json:"tag,omitempty" doc:"Tag of the image to pull (e.g., latest)"`

	// Auth for authenticating with private registries (legacy field name).
	//
	// Required: false
	Auth *ContainerRegistryCredential `json:"auth,omitempty"`

	// Credentials for authenticating with private registries.
	//
	// Required: false
	Credentials []ContainerRegistryCredential `json:"credentials,omitempty"`
}

// GetFullImageName returns the image name with tag.
func (p ImagePullOptions) GetFullImageName() string {
	if p.Tag != "" && p.Tag != "latest" {
		return p.ImageName + ":" + p.Tag
	}
	if p.Tag == "latest" && !strings.Contains(p.ImageName, ":") {
		return p.ImageName + ":latest"
	}
	return p.ImageName
}

// GetCredentials returns credentials from either the Auth or Credentials field.
func (p ImagePullOptions) GetCredentials() []ContainerRegistryCredential {
	if len(p.Credentials) > 0 {
		return p.Credentials
	}
	if p.Auth != nil {
		return []ContainerRegistryCredential{*p.Auth}
	}
	return nil
}

// NewImageDetailSummary creates a DetailSummary from a Docker image inspect response.
// It converts the Docker API types to the application's DetailSummary type,
// handling nested structs and converting exposed ports from Docker's nat.PortSet
// to string keys. The descriptor is derived from the first repo digest if available.
func NewImageDetailSummary(src *image.InspectResponse) ImageDetailSummary {
	var out ImageDetailSummary
	if src == nil {
		return out
	}

	out.ID = src.ID
	out.RepoTags = append(out.RepoTags, src.RepoTags...)
	out.RepoDigests = append(out.RepoDigests, src.RepoDigests...)
	out.Comment = src.Comment
	out.Created = src.Created
	out.Author = src.Author

	if src.Config != nil {
		if len(src.Config.ExposedPorts) > 0 {
			out.Config.ExposedPorts = make(map[string]struct{}, len(src.Config.ExposedPorts))
			for p := range src.Config.ExposedPorts {
				out.Config.ExposedPorts[p] = struct{}{}
			}
		}
		if len(src.Config.Env) > 0 {
			out.Config.Env = append(out.Config.Env, src.Config.Env...)
		}
		if len(src.Config.Cmd) > 0 {
			out.Config.Cmd = append(out.Config.Cmd, src.Config.Cmd...)
		}
		if len(src.Config.Volumes) > 0 {
			out.Config.Volumes = make(map[string]struct{}, len(src.Config.Volumes))
			for v := range src.Config.Volumes {
				out.Config.Volumes[v] = struct{}{}
			}
		}
		out.Config.WorkingDir = src.Config.WorkingDir
		out.Config.ArgsEscaped = src.Config.ArgsEscaped //nolint:staticcheck,nolintlint // Mirror Docker inspect data; deprecated only for new image builders.
	}

	out.Architecture = src.Architecture
	out.Os = src.Os
	out.Size = src.Size

	if src.GraphDriver != nil {
		out.GraphDriver.Name = src.GraphDriver.Name
		if src.GraphDriver.Data != nil {
			out.GraphDriver.Data = src.GraphDriver.Data
		}
	}

	out.RootFs.Type = src.RootFS.Type
	if len(src.RootFS.Layers) > 0 {
		out.RootFs.Layers = append(out.RootFs.Layers, src.RootFS.Layers...)
	}

	if !src.Metadata.LastTagTime.IsZero() {
		out.Metadata.LastTagTime = src.Metadata.LastTagTime.Format(time.RFC3339Nano)
	}

	// Best-effort descriptor from first digest
	out.Descriptor.MediaType = "application/vnd.oci.image.index.v1+json"
	out.Descriptor.Size = src.Size
	if len(src.RepoDigests) > 0 {
		parts := strings.SplitN(src.RepoDigests[0], "@", 2)
		if len(parts) == 2 {
			out.Descriptor.Digest = parts[1]
		}
	}

	return out
}

// ImageBuildRecord represents a historical image build entry.
//
//nolint:recvcheck // Rows read the environment by value and set it through a pointer.
type ImageBuildRecord struct {
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
	EnvironmentName string            `json:"environmentName,omitempty"`
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

func (r ImageBuildRecord) Environment() EnvironmentRef {
	return EnvironmentRef{ID: r.EnvironmentID, Name: r.EnvironmentName}
}

func (r *ImageBuildRecord) SetEnvironment(ref EnvironmentRef) {
	r.EnvironmentID, r.EnvironmentName = ref.ID, ref.Name
}

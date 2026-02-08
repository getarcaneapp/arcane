package image

import (
	"math"
	"strings"
	"time"

	containerregistry "github.com/getarcaneapp/arcane/types/containerregistry"
	"github.com/getarcaneapp/arcane/types/vulnerability"
	"github.com/moby/moby/api/types/image"
)

type UpdateInfo struct {
	// HasUpdate indicates if an update is available for the image.
	//
	// Required: true
	HasUpdate bool `json:"hasUpdate"`

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

	// CheckTime is the time when the update check was performed.
	//
	// Required: true
	CheckTime time.Time `json:"checkTime"`

	// ResponseTimeMs is the response time in milliseconds.
	//
	// Required: true
	ResponseTimeMs int `json:"responseTimeMs"`

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

	// UsedCredential indicates if credentials were used for the update check.
	//
	// Required: false
	UsedCredential bool `json:"usedCredential,omitempty"`
}

type UsedBy struct {
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

type Summary struct {
	// ID is the unique identifier of the image.
	//
	// Required: true
	ID string `json:"id" sortable:"true"`

	// RepoTags is a list of tags referring to this image.
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
	UsedBy []UsedBy `json:"usedBy,omitempty"`

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
	UpdateInfo *UpdateInfo `json:"updateInfo,omitempty"`

	// VulnerabilityScan contains the latest vulnerability scan summary for the image.
	//
	// Required: false
	VulnerabilityScan *vulnerability.ScanSummary `json:"vulnerabilityScan,omitempty"`
}

type PruneReport struct {
	// ImagesDeleted is a list of image IDs that were deleted.
	//
	// Required: true
	ImagesDeleted []string `json:"imagesDeleted"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed int64 `json:"spaceReclaimed"`
}

// NewPruneReport creates a PruneReport from a Docker image prune report.
// It extracts deleted and untagged image IDs from the Docker API response,
// combining both types into a single list and converting space reclaimed to int64.
func NewPruneReport(src image.PruneReport) PruneReport {
	// Safely convert uint64 to int64, capping at MaxInt64 to prevent overflow
	spaceReclaimed := int64(0)
	if src.SpaceReclaimed > uint64(math.MaxInt64) {
		spaceReclaimed = math.MaxInt64
	} else {
		spaceReclaimed = int64(src.SpaceReclaimed)
	}

	out := PruneReport{
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

type UsageCounts struct {
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

type LoadResult struct {
	// Stream contains the output stream from the load operation.
	//
	// Required: true
	Stream string `json:"stream"`
}

// ProgressDetail provides byte progress information for stream events.
type ProgressDetail struct {
	Current int64 `json:"current,omitempty"`
	Total   int64 `json:"total,omitempty"`
}

// ProgressEvent is the standardized NDJSON envelope for pull/build/deploy streams.
type ProgressEvent struct {
	Type           string          `json:"type,omitempty"`  // pull|build|deploy
	Phase          string          `json:"phase,omitempty"` // begin|complete|... (optional)
	Service        string          `json:"service,omitempty"`
	Status         string          `json:"status,omitempty"`
	ID             string          `json:"id,omitempty"`
	ProgressDetail *ProgressDetail `json:"progressDetail,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// BuildRequest contains options for building an image with BuildKit.
type BuildRequest struct {
	// ContextDir is the build context directory on the server.
	//
	// Required: true
	ContextDir string `json:"contextDir" minLength:"1" doc:"Build context directory"`

	// Dockerfile is the path to the Dockerfile (relative to context or absolute).
	//
	// Required: false
	Dockerfile string `json:"dockerfile,omitempty" doc:"Dockerfile path"`

	// Tags are image tags to apply.
	//
	// Required: false
	Tags []string `json:"tags,omitempty" doc:"Image tags"`

	// Target is the Dockerfile target stage.
	//
	// Required: false
	Target string `json:"target,omitempty" doc:"Target stage"`

	// BuildArgs are build arguments to pass to the Dockerfile.
	//
	// Required: false
	BuildArgs map[string]string `json:"buildArgs,omitempty" doc:"Build arguments"`

	// Labels are image metadata labels applied at build time.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty" doc:"Build labels"`

	// CacheFrom defines cache sources to use during build.
	//
	// Required: false
	CacheFrom []string `json:"cacheFrom,omitempty" doc:"Build cache sources"`

	// CacheTo defines cache export targets from build outputs.
	//
	// Required: false
	CacheTo []string `json:"cacheTo,omitempty" doc:"Build cache targets"`

	// NoCache disables layer cache usage for Dockerfile instructions.
	//
	// Required: false
	NoCache bool `json:"noCache,omitempty" doc:"Disable build cache"`

	// Pull forces pulling referenced base images.
	//
	// Required: false
	Pull bool `json:"pull,omitempty" doc:"Always pull referenced base images"`

	// Network sets the build-time network mode for RUN instructions.
	//
	// Required: false
	Network string `json:"network,omitempty" doc:"Build network mode"`

	// Isolation sets platform-specific container isolation mode during build.
	//
	// Required: false
	Isolation string `json:"isolation,omitempty" doc:"Build isolation mode"`

	// ShmSize sets /dev/shm size in bytes for build containers.
	//
	// Required: false
	ShmSize int64 `json:"shmSize,omitempty" doc:"Build shared memory size in bytes"`

	// Ulimits configures ulimit values for build containers.
	//
	// Required: false
	Ulimits map[string]string `json:"ulimits,omitempty" doc:"Build ulimits"`

	// Entitlements sets extra BuildKit entitlements allowed for the build.
	//
	// Required: false
	Entitlements []string `json:"entitlements,omitempty" doc:"Build entitlements"`

	// Privileged enables elevated build privileges where supported.
	//
	// Required: false
	Privileged bool `json:"privileged,omitempty" doc:"Enable privileged build"`

	// ExtraHosts adds host-to-IP mappings at build time.
	//
	// Required: false
	ExtraHosts []string `json:"extraHosts,omitempty" doc:"Build extra host mappings"`

	// Platforms are target platforms (e.g., linux/amd64,linux/arm64).
	//
	// Required: false
	Platforms []string `json:"platforms,omitempty" doc:"Target platforms"`

	// Push controls whether the image should be pushed to a registry.
	//
	// Required: false
	Push bool `json:"push,omitempty" doc:"Push image"`

	// Load controls whether the image should be loaded into the local Docker daemon.
	//
	// Required: false
	Load bool `json:"load,omitempty" doc:"Load image into local Docker"`

	// Provider overrides the build provider (local|depot).
	//
	// Required: false
	Provider string `json:"provider,omitempty" doc:"Build provider override"`
}

// BuildResult provides basic build output metadata.
type BuildResult struct {
	Provider string   `json:"provider"`
	Tags     []string `json:"tags,omitempty"`
	Digest   string   `json:"digest,omitempty"`
}

type DetailSummary struct {
	// ID is the unique identifier of the image.
	//
	// Required: true
	ID string `json:"id"`

	// RepoTags is a list of tags referring to this image.
	//
	// Required: true
	RepoTags []string `json:"repoTags"`

	// RepoDigests is a list of content-addressable digests of the image.
	//
	// Required: true
	RepoDigests []string `json:"repoDigests"`

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

// PullOptions contains options for pulling an image.
type PullOptions struct {
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
	Auth *containerregistry.Credential `json:"auth,omitempty"`

	// Credentials for authenticating with private registries.
	//
	// Required: false
	Credentials []containerregistry.Credential `json:"credentials,omitempty"`
}

// GetFullImageName returns the image name with tag.
func (p PullOptions) GetFullImageName() string {
	if p.Tag != "" && p.Tag != "latest" {
		return p.ImageName + ":" + p.Tag
	}
	if p.Tag == "latest" && !strings.Contains(p.ImageName, ":") {
		return p.ImageName + ":latest"
	}
	return p.ImageName
}

// GetCredentials returns credentials from either the Auth or Credentials field.
func (p PullOptions) GetCredentials() []containerregistry.Credential {
	if len(p.Credentials) > 0 {
		return p.Credentials
	}
	if p.Auth != nil {
		return []containerregistry.Credential{*p.Auth}
	}
	return nil
}

// NewDetailSummary creates a DetailSummary from a Docker image inspect response.
// It converts the Docker API types to the application's DetailSummary type,
// handling nested structs and converting exposed ports from Docker's nat.PortSet
// to string keys. The descriptor is derived from the first repo digest if available.
func NewDetailSummary(src *image.InspectResponse) DetailSummary {
	var out DetailSummary

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
				out.Config.ExposedPorts[string(p)] = struct{}{}
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
		out.Config.ArgsEscaped = src.Config.ArgsEscaped //nolint:staticcheck // Required for Docker Windows image compatibility
	}

	out.Architecture = src.Architecture
	out.Os = src.Os
	out.Size = src.Size

	out.GraphDriver.Name = src.GraphDriver.Name
	if src.GraphDriver.Data != nil {
		out.GraphDriver.Data = src.GraphDriver.Data
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

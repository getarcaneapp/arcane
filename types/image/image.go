package image

import (
	"strings"
	"time"

	"github.com/docker/docker/api/types/image"
	containerregistry "go.getarcane.app/types/containerregistry"
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
	Labels map[string]interface{} `json:"labels"`

	// InUse indicates if the image is currently in use by a container.
	//
	// Required: true
	InUse bool `json:"inUse" sortable:"true"`

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
	out := PruneReport{
		ImagesDeleted:  make([]string, 0, len(src.ImagesDeleted)),
		SpaceReclaimed: int64(src.SpaceReclaimed),
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
		Data interface{} `json:"data"`
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
	ImageName string `json:"imageName" binding:"required"`

	// Credentials for authenticating with private registries.
	//
	// Required: false
	Credentials []containerregistry.Credential `json:"credentials,omitempty"`
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

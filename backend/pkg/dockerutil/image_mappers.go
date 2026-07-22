package docker

import (
	"math"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/moby/moby/api/types/image"
)

// NewImagePruneReport creates an API prune report from a Docker image prune report.
func NewImagePruneReport(src image.PruneReport) imagetypes.PruneReport {
	var spaceReclaimed int64
	if src.SpaceReclaimed > uint64(math.MaxInt64) {
		spaceReclaimed = math.MaxInt64
	} else {
		spaceReclaimed = int64(src.SpaceReclaimed)
	}

	out := imagetypes.PruneReport{
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

// FullImageName returns the image name and requested tag.
func FullImageName(p imagetypes.PullOptions) string {
	if p.Tag != "" && p.Tag != "latest" {
		return p.ImageName + ":" + p.Tag
	}
	if p.Tag == "latest" && !strings.Contains(p.ImageName, ":") {
		return p.ImageName + ":latest"
	}
	return p.ImageName
}

// PullCredentials returns credentials from either the current or legacy field.
func PullCredentials(p imagetypes.PullOptions) []containerregistry.Credential {
	if len(p.Credentials) > 0 {
		return p.Credentials
	}
	if p.Auth != nil {
		return []containerregistry.Credential{*p.Auth}
	}
	return nil
}

// NewImageDetailSummary creates an API image detail summary from a Docker inspect response.
func NewImageDetailSummary(src *image.InspectResponse) imagetypes.DetailSummary {
	var out imagetypes.DetailSummary
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

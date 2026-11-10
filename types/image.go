package types

import (
	"encoding/json/jsontext"
	"math"
	"strings"
	"time"

	"github.com/moby/moby/api/types/image"
)

type (
	ImageListResponse struct {
		Data []ImageSummary `json:"data" doc:"Array of items for the current page"`

		FailedEnvironments []EnvironmentFailure `json:"failedEnvironments,omitempty" doc:"Environments whose results are missing from a multi-environment response"`
		Pagination         PaginationResponse   `json:"pagination" doc:"Pagination metadata"`

		MaxImageUploadSize int `json:"maxImageUploadSize" doc:"Maximum image archive upload size in MB"`

		Success bool `json:"success" doc:"Whether the request was successful"`
	}

	ImageUpdateInfo struct {
		CheckTime      time.Time `json:"checkTime"`
		UpdateType     string    `json:"updateType"`
		CurrentVersion string    `json:"currentVersion"`
		LatestVersion  string    `json:"latestVersion"`
		CurrentDigest  string    `json:"currentDigest"`
		LatestDigest   string    `json:"latestDigest"`

		CurrentCreated *time.Time `json:"currentCreated,omitempty"`

		LatestCreated *time.Time `json:"latestCreated,omitempty"`
		Error         string     `json:"error"`
		AuthMethod    string     `json:"authMethod,omitempty"`
		AuthUsername  string     `json:"authUsername,omitempty"`
		AuthRegistry  string     `json:"authRegistry,omitempty"`

		ResponseTimeMs int  `json:"responseTimeMs"`
		HasUpdate      bool `json:"hasUpdate"`
		UsedCredential bool `json:"usedCredential,omitempty"`
	}

	ImageUsedBy struct {
		Type string `json:"type"`
		Name string `json:"name"`
		ID   string `json:"id,omitempty"`
	}

	HardenedImageSupportStatus string

	// HardenedImageInfo describes a Docker Hardened Image from its com.docker.dhi.* labels.
	HardenedImageInfo struct {
		// Shell and PackageManager are empty when the label says the image has none, and nil when the label is missing.
		Shell          *string                    `json:"shell,omitzero" doc:"Shell in the image; empty when it has none"`
		PackageManager *string                    `json:"packageManager,omitzero" doc:"Package manager in the image; empty when it has none"`
		Name           string                     `json:"name" doc:"Hardened image name, such as dhi/aspnetcore"`
		Title          string                     `json:"title,omitempty" doc:"Display title"`
		Version        string                     `json:"version,omitempty" doc:"Image version"`
		Definition     string                     `json:"definition,omitempty" doc:"Catalog definition path"`
		Distro         string                     `json:"distro,omitempty" doc:"Base distribution"`
		Variant        string                     `json:"variant,omitempty" doc:"Image variant, such as runtime or dev"`
		Flavor         string                     `json:"flavor,omitempty" doc:"Image flavor"`
		Entitlement    string                     `json:"entitlement,omitempty" doc:"Catalog entitlement"`
		URL            string                     `json:"url,omitempty" doc:"Catalog page URL"`
		ChainID        string                     `json:"chainId,omitempty" doc:"Build chain ID"`
		Created        string                     `json:"created,omitempty" doc:"Build time (RFC 3339)"`
		ReleaseDate    string                     `json:"releaseDate,omitempty" doc:"Release date (YYYY-MM-DD)"`
		EndOfLifeDate  string                     `json:"endOfLifeDate,omitempty" doc:"End-of-life date (YYYY-MM-DD)"`
		SupportStatus  HardenedImageSupportStatus `json:"supportStatus" enum:"supported,ending_soon,end_of_life,unknown" doc:"Support status from the end-of-life date"`
		Compliance     []string                   `json:"compliance,omitempty" doc:"Compliance frameworks, such as cis"`
	}

	ImageSummary struct {
		Labels            map[string]any            `json:"labels"`
		UpdateInfo        *ImageUpdateInfo          `json:"updateInfo,omitempty"`
		VulnerabilityScan *VulnerabilityScanSummary `json:"vulnerabilityScan,omitempty"`
		Hardened          *HardenedImageInfo        `json:"hardened,omitempty"`

		EnvironmentTag
		ID               string        `json:"id" sortable:"true"`
		Repo             string        `json:"repo" sortable:"true"`
		Tag              string        `json:"tag" sortable:"true"`
		RepoTags         []string      `json:"repoTags"`
		RepoDigests      []string      `json:"repoDigests"`
		UsedBy           []ImageUsedBy `json:"usedBy,omitempty"`
		PinnedReferences []string      `json:"pinnedReferences,omitempty"`

		Created     int64 `json:"created" sortable:"true"`
		Size        int64 `json:"size" sortable:"true"`
		VirtualSize int64 `json:"virtualSize"`
		InUse       bool  `json:"inUse" sortable:"true"`
	}

	ImageAttestationList struct {
		ImageRef      string             `json:"imageRef"`
		SubjectDigest string             `json:"subjectDigest"`
		Platform      string             `json:"platform,omitempty"`
		Attestations  []ImageAttestation `json:"attestations"`
	}

	ImageReleaseNotes struct {
		ImageRef    string `json:"imageRef"`
		Version     string `json:"version,omitempty"`
		Status      string `json:"status"`
		SourceURL   string `json:"sourceUrl,omitempty"`
		ReleaseURL  string `json:"releaseUrl,omitempty"`
		ReleaseName string `json:"releaseName,omitempty"`
		Notes       string `json:"notes,omitempty"`
		PublishedAt string `json:"publishedAt,omitempty"`
	}

	ImageAttestation struct {
		Digest        string                    `json:"digest"`
		MediaType     string                    `json:"mediaType"`
		ArtifactType  string                    `json:"artifactType,omitempty"`
		PredicateType string                    `json:"predicateType"`
		StatementType string                    `json:"statementType,omitempty"`
		Platform      string                    `json:"platform,omitempty"`
		Subject       []ImageAttestationSubject `json:"subject"`
		Statement     jsontext.Value            `json:"statement,omitempty"`
		Size          int64                     `json:"size"`
	}

	ImageAttestationSubject struct {
		Digest map[string]string `json:"digest"`
		Name   string            `json:"name"`
	}

	ImagePruneReport struct {
		ImagesDeleted []string `json:"imagesDeleted"`

		SpaceReclaimed int64 `json:"spaceReclaimed"`
	}

	ImageTagRequest struct {
		Repository string `json:"repository" doc:"Target repository name"`

		Tag string `json:"tag,omitempty" doc:"Target tag"`
	}

	ImageHistoryItem struct {
		ID        string   `json:"id"`
		CreatedBy string   `json:"createdBy"`
		Comment   string   `json:"comment"`
		Tags      []string `json:"tags"`
		Created   int64    `json:"created"`
		Size      int64    `json:"size"`
	}

	ImageSearchResult struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		StarCount   int    `json:"starCount"`
		Official    bool   `json:"official"`
		Automated   bool   `json:"automated"`
	}

	ImageUsageCounts struct {
		Inuse  int `json:"imagesInuse"`
		Unused int `json:"imagesUnused"`
		Total  int `json:"totalImages"`

		HardenedEndingSoon int `json:"hardenedEndingSoon" doc:"Hardened images whose end of life is near"`
		HardenedEndOfLife  int `json:"hardenedEndOfLife" doc:"Hardened images past their end of life"`

		TotalSize int64 `json:"totalImageSize"`
	}

	ImageLoadResult struct {
		Stream string `json:"stream"`
	}

	ImageDetailSummary struct {
		GraphDriver struct {
			Data any    `json:"data"`
			Name string `json:"name"`
		} `json:"graphDriver"`
		Os           string `json:"os"`
		Created      string `json:"created"`
		Author       string `json:"author"`
		Architecture string `json:"architecture"`
		ID           string `json:"id"`
		Metadata     struct {
			LastTagTime string `json:"lastTagTime"`
		} `json:"metadata"`
		Comment    string `json:"comment"`
		Descriptor struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		} `json:"descriptor"`
		RootFs struct {
			Type   string   `json:"type"`
			Layers []string `json:"layers"`
		} `json:"rootFs"`
		RepoTags         []string           `json:"repoTags"`
		RepoDigests      []string           `json:"repoDigests"`
		PinnedReferences []string           `json:"pinnedReferences,omitempty"`
		Hardened         *HardenedImageInfo `json:"hardened,omitempty"`
		UpdateInfo       *ImageUpdateInfo   `json:"updateInfo,omitempty"`
		Config           struct {
			ExposedPorts map[string]struct{} `json:"exposedPorts,omitempty"`
			Volumes      map[string]struct{} `json:"volumes,omitempty"`
			WorkingDir   string              `json:"workingDir,omitempty"`
			Env          []string            `json:"env,omitempty"`
			Cmd          []string            `json:"cmd,omitempty"`
			ArgsEscaped  bool                `json:"argsEscaped,omitempty"`
		} `json:"config"`
		Size int64 `json:"size"`
	}

	ImagePullOptions struct {
		ImageName string `json:"imageName" minLength:"1" doc:"Name of the image to pull (e.g., nginx)"`

		Tag         string                        `json:"tag,omitempty" doc:"Tag of the image to pull (e.g., latest)"`
		Auth        *ContainerRegistryCredential  `json:"auth,omitempty"`
		Credentials []ContainerRegistryCredential `json:"credentials,omitempty"`
	}

	//nolint:recvcheck // Rows read the environment by value and set it through a pointer.
	ImageBuildRecord struct {
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
)

const (
	HardenedImageSupported      HardenedImageSupportStatus = "supported"
	HardenedImageEndingSoon     HardenedImageSupportStatus = "ending_soon"
	HardenedImageEndOfLife      HardenedImageSupportStatus = "end_of_life"
	HardenedImageSupportUnknown HardenedImageSupportStatus = "unknown"
)

func NewImagePruneReport(src image.PruneReport) ImagePruneReport {
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

func (p ImagePullOptions) GetFullImageName() string {
	if p.Tag != "" && p.Tag != "latest" {
		return p.ImageName + ":" + p.Tag
	}
	if p.Tag == "latest" && !strings.Contains(p.ImageName, ":") {
		return p.ImageName + ":latest"
	}
	return p.ImageName
}

func (p ImagePullOptions) GetCredentials() []ContainerRegistryCredential {
	if len(p.Credentials) > 0 {
		return p.Credentials
	}
	if p.Auth != nil {
		return []ContainerRegistryCredential{*p.Auth}
	}
	return nil
}

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

func (r ImageBuildRecord) Environment() EnvironmentRef {
	return EnvironmentRef{ID: r.EnvironmentID, Name: r.EnvironmentName}
}

func (r *ImageBuildRecord) SetEnvironment(ref EnvironmentRef) {
	r.EnvironmentID, r.EnvironmentName = ref.ID, ref.Name
}

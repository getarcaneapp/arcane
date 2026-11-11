package types

import (
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type (
	DashboardDockerSnapshot struct {
		Volumes    *client.VolumeListResult  `json:"volumes"`
		Containers []dockercontainer.Summary `json:"containers"`
		Images     []dockerimage.Summary     `json:"images"`
		Networks   []dockernetwork.Summary   `json:"networks"`
	}

	DashboardSection[T any] struct {
		Data  T      `json:"data"`
		Error string `json:"error,omitzero"`
	}

	DashboardSummary struct {
		Containers      *DashboardSection[ContainerStatusCounts]            `json:"containers,omitzero"`
		Projects        *DashboardSection[ProjectStatusCounts]              `json:"projects,omitzero"`
		Images          *DashboardSection[ImageUsageCounts]                 `json:"images,omitzero"`
		Vulnerabilities *DashboardSection[*EnvironmentVulnerabilitySummary] `json:"vulnerabilities,omitzero"`
		Risk            *DashboardSection[*RiskOverview]                    `json:"risk,omitzero"`
		DiskUsage       *DashboardSection[SystemDiskUsage]                  `json:"diskUsage,omitzero"`
		TopMemory       *DashboardSection[[]ContainerSummary]               `json:"topMemory,omitzero"`
		Version         *DashboardSection[*VersionInfo]                     `json:"version,omitzero"`
	}
)

const (
	DashboardSectionContainers      = "containers"
	DashboardSectionProjects        = "projects"
	DashboardSectionImages          = "images"
	DashboardSectionVulnerabilities = "vulnerabilities"
	DashboardSectionRisk            = "risk"
	DashboardSectionDiskUsage       = "diskUsage"
	DashboardSectionTopMemory       = "topMemory"
	DashboardSectionVersion         = "version"
)

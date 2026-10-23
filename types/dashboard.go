package types

import (
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type DashboardDockerSnapshot struct {
	Containers []dockercontainer.Summary `json:"containers"`
	Images     []dockerimage.Summary     `json:"images"`
	Networks   []dockernetwork.Summary   `json:"networks"`
	Volumes    *client.VolumeListResult  `json:"volumes"`
}

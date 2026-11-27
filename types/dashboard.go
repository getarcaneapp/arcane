package types

import (
	mobyContainer "github.com/moby/moby/api/types/container"
	mobyImage "github.com/moby/moby/api/types/image"
	mobyNetwork "github.com/moby/moby/api/types/network"
	mobyClient "github.com/moby/moby/client"
)

type (
	DashboardDockerSnapshot struct {
		Volumes    *mobyClient.VolumeListResult `json:"volumes"`
		Containers []mobyContainer.Summary      `json:"containers"`
		Images     []mobyImage.Summary          `json:"images"`
		Networks   []mobyNetwork.Summary        `json:"networks"`
	}
)

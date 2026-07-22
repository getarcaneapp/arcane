package docker

import (
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/moby/moby/api/types/volume"
)

// NewVolumeSummary creates an API volume summary from a Docker volume.
func NewVolumeSummary(v volume.Volume) volumetypes.Volume {
	mountpoint := v.Mountpoint
	if v.Options["type"] == "none" && v.Options["device"] != "" {
		mountpoint = v.Options["device"]
	}

	dto := volumetypes.Volume{
		ID:         v.Name,
		Name:       v.Name,
		Driver:     v.Driver,
		Mountpoint: mountpoint,
		Scope:      v.Scope,
		Options:    v.Options,
		Labels:     v.Labels,
		CreatedAt:  v.CreatedAt,
		Containers: make([]string, 0),
	}

	if v.UsageData != nil {
		dto.InUse = v.UsageData.RefCount >= 1
		dto.UsageData = v.UsageData
		dto.Size = v.UsageData.Size
	}

	return dto
}

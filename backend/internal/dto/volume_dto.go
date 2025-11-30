package dto

import "github.com/docker/docker/api/types/volume"

type VolumeUsageData struct {
	Size     int64 `json:"size"`
	RefCount int64 `json:"refCount"`
}

type VolumeDto struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Scope      string            `json:"scope"`
	Options    map[string]string `json:"options"`
	Labels     map[string]string `json:"labels"`
	CreatedAt  string            `json:"createdAt"`
	InUse      bool              `json:"inUse"`
	UsageData  *VolumeUsageData  `json:"usageData,omitempty"`
	Size       int64             `json:"size"`
	Containers []string          `json:"containers"`
}

func NewVolumeDto(v volume.Volume) VolumeDto {
	dto := VolumeDto{
		ID:         v.Name,
		Name:       v.Name,
		Driver:     v.Driver,
		Mountpoint: v.Mountpoint,
		Scope:      v.Scope,
		Options:    v.Options,
		Labels:     v.Labels,
		CreatedAt:  v.CreatedAt,
		Containers: make([]string, 0),
	}

	if v.UsageData != nil {
		dto.UsageData = &VolumeUsageData{
			Size:     v.UsageData.Size,
			RefCount: v.UsageData.RefCount,
		}
		dto.Size = v.UsageData.Size
		if v.UsageData.RefCount >= 1 {
			dto.InUse = true
		} else {
			dto.InUse = false
		}

	}

	return dto
}

type VolumeUsageCounts struct {
	Inuse  int `json:"volumesInuse"`
	Unused int `json:"volumesUnused"`
	Total  int `json:"totalVolumes"`
}

type VolumePruneReportDto struct {
	VolumesDeleted []string `json:"volumesDeleted"`
	SpaceReclaimed uint64   `json:"spaceReclaimed"`
}

type CreateVolumeDto struct {
	Name    string            `json:"name" binding:"required"`
	Driver  string            `json:"driver"`
	Labels  map[string]string `json:"labels"`
	Options map[string]string `json:"options"`
}

type VolumeUsageDto struct {
	InUse      bool     `json:"inUse"`
	Containers []string `json:"containers"`
}

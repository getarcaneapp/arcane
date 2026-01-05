package volume

import "github.com/docker/docker/api/types/volume"

// Volume represents a Docker volume.
type Volume struct {
	// ID is the unique identifier of the volume.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the volume.
	//
	// Required: true
	Name string `json:"name"`

	// Driver is the volume driver used.
	//
	// Required: true
	Driver string `json:"driver"`

	// Mountpoint is the path where the volume is mounted.
	//
	// Required: true
	Mountpoint string `json:"mountpoint"`

	// Scope of the volume (local, global, etc).
	//
	// Required: true
	Scope string `json:"scope"`

	// Options contains driver-specific options.
	//
	// Required: true
	Options map[string]string `json:"options"`

	// Labels contains user-defined metadata for the volume.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// CreatedAt is the time when the volume was created.
	//
	// Required: true
	CreatedAt string `json:"createdAt"`

	// InUse indicates if the volume is currently in use by a container.
	//
	// Required: true
	InUse bool `json:"inUse"`

	// UsageData contains size and reference count information.
	//
	// Required: false
	UsageData *volume.UsageData `json:"usageData,omitempty"`

	// Size is the size of the volume in bytes.
	//
	// Required: false
	Size int64 `json:"size"`

	// Containers is a list of container IDs using this volume.
	//
	// Required: true
	Containers []string `json:"containers"`
}

// UsageCounts contains counts of volumes by usage status.
type UsageCounts struct {
	// Inuse is the number of volumes currently in use.
	//
	// Required: true
	Inuse int `json:"inuse"`

	// Unused is the number of volumes not in use.
	//
	// Required: true
	Unused int `json:"unused"`

	// Total is the total number of volumes.
	//
	// Required: true
	Total int `json:"total"`
}

// PruneReport is the result of a volume prune operation.
type PruneReport struct {
	// VolumesDeleted is a list of volume names that were deleted.
	//
	// Required: true
	VolumesDeleted []string `json:"volumesDeleted"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed uint64 `json:"spaceReclaimed"`
}

// Create is used to create a new volume.
type Create struct {
	// Name of the volume.
	//
	// Required: true
	Name string `json:"name" minLength:"1" doc:"Name of the volume"`

	// Driver is the volume driver to use.
	//
	// Required: false
	Driver string `json:"driver,omitempty" doc:"Volume driver (e.g., local, nfs)"`

	// DriverOpts contains driver-specific options.
	//
	// Required: false
	DriverOpts map[string]string `json:"driverOpts,omitempty" doc:"Driver-specific options"`

	// Labels contains user-defined metadata for the volume.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty" doc:"User-defined labels"`
}

// NewSummary creates a Volume from a docker volume.Volume, calculating InUse
// based on whether the volume has a reference count of 1 or more.
//
// InUse is set to true if the volume's UsageData.RefCount is >= 1, false otherwise.
func NewSummary(v volume.Volume) Volume {
	dto := Volume{
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
		dto.InUse = v.UsageData.RefCount >= 1
		dto.UsageData = v.UsageData
		dto.Size = v.UsageData.Size
	}

	return dto
}

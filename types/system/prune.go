package system

import "encoding/json"

type PruneContainerMode string

const (
	PruneContainerModeNone      PruneContainerMode = "none"
	PruneContainerModeStopped   PruneContainerMode = "stopped"
	PruneContainerModeOlderThan PruneContainerMode = "olderThan"
)

type PruneImageMode string

const (
	PruneImageModeNone      PruneImageMode = "none"
	PruneImageModeDangling  PruneImageMode = "dangling"
	PruneImageModeAll       PruneImageMode = "all"
	PruneImageModeOlderThan PruneImageMode = "olderThan"
)

type PruneVolumeMode string

const (
	PruneVolumeModeNone      PruneVolumeMode = "none"
	PruneVolumeModeAnonymous PruneVolumeMode = "anonymous"
	PruneVolumeModeAll       PruneVolumeMode = "all"
)

type PruneNetworkMode string

const (
	PruneNetworkModeNone      PruneNetworkMode = "none"
	PruneNetworkModeUnused    PruneNetworkMode = "unused"
	PruneNetworkModeOlderThan PruneNetworkMode = "olderThan"
)

type PruneBuildCacheMode string

const (
	PruneBuildCacheModeNone      PruneBuildCacheMode = "none"
	PruneBuildCacheModeUnused    PruneBuildCacheMode = "unused"
	PruneBuildCacheModeAll       PruneBuildCacheMode = "all"
	PruneBuildCacheModeOlderThan PruneBuildCacheMode = "olderThan"
)

type PruneContainersOptions struct {
	Mode  PruneContainerMode `json:"mode"`
	Until string             `json:"until,omitempty"`
}

type PruneImagesOptions struct {
	Mode  PruneImageMode `json:"mode"`
	Until string         `json:"until,omitempty"`
}

type PruneVolumesOptions struct {
	Mode PruneVolumeMode `json:"mode"`
}

type PruneNetworksOptions struct {
	Mode  PruneNetworkMode `json:"mode"`
	Until string           `json:"until,omitempty"`
}

type PruneBuildCacheOptions struct {
	Mode  PruneBuildCacheMode `json:"mode"`
	Until string              `json:"until,omitempty"`
}

// PruneAllRequest is used to request pruning of Docker system resources.
type PruneAllRequest struct {
	Containers *PruneContainersOptions `json:"containers,omitempty"`
	Images     *PruneImagesOptions     `json:"images,omitempty"`
	Volumes    *PruneVolumesOptions    `json:"volumes,omitempty"`
	Networks   *PruneNetworksOptions   `json:"networks,omitempty"`
	BuildCache *PruneBuildCacheOptions `json:"buildCache,omitempty"`
}

type pruneAllRequestWireInternal struct {
	Containers json.RawMessage `json:"containers,omitempty"`
	Images     json.RawMessage `json:"images,omitempty"`
	Volumes    json.RawMessage `json:"volumes,omitempty"`
	Networks   json.RawMessage `json:"networks,omitempty"`
	BuildCache json.RawMessage `json:"buildCache,omitempty"`
	Dangling   *bool           `json:"dangling,omitempty"`
}

func (r *PruneAllRequest) UnmarshalJSON(data []byte) error {
	var wire pruneAllRequestWireInternal
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	*r = PruneAllRequest{}

	containers, err := decodePruneContainersOptionsInternal(wire.Containers)
	if err != nil {
		return err
	}
	r.Containers = containers

	images, err := decodePruneImagesOptionsInternal(wire.Images, wire.Dangling)
	if err != nil {
		return err
	}
	r.Images = images

	volumes, err := decodePruneVolumesOptionsInternal(wire.Volumes, wire.Dangling)
	if err != nil {
		return err
	}
	r.Volumes = volumes

	networks, err := decodePruneNetworksOptionsInternal(wire.Networks)
	if err != nil {
		return err
	}
	r.Networks = networks

	buildCache, err := decodePruneBuildCacheOptionsInternal(wire.BuildCache, wire.Dangling)
	if err != nil {
		return err
	}
	r.BuildCache = buildCache

	return nil
}

func decodePruneContainersOptionsInternal(raw json.RawMessage) (*PruneContainersOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		return &PruneContainersOptions{Mode: PruneContainerModeStopped}, nil
	}

	var options PruneContainersOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneImagesOptionsInternal(raw json.RawMessage, dangling *bool) (*PruneImagesOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		mode := PruneImageModeDangling
		if dangling != nil && !*dangling {
			mode = PruneImageModeAll
		}
		return &PruneImagesOptions{Mode: mode}, nil
	}

	var options PruneImagesOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneVolumesOptionsInternal(raw json.RawMessage, dangling *bool) (*PruneVolumesOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		mode := PruneVolumeModeAnonymous
		if dangling != nil && !*dangling {
			mode = PruneVolumeModeAll
		}
		return &PruneVolumesOptions{Mode: mode}, nil
	}

	var options PruneVolumesOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneNetworksOptionsInternal(raw json.RawMessage) (*PruneNetworksOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		return &PruneNetworksOptions{Mode: PruneNetworkModeUnused}, nil
	}

	var options PruneNetworksOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

func decodePruneBuildCacheOptionsInternal(raw json.RawMessage, dangling *bool) (*PruneBuildCacheOptions, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil {
		if !enabled {
			return nil, nil
		}
		mode := PruneBuildCacheModeUnused
		if dangling != nil && !*dangling {
			mode = PruneBuildCacheModeAll
		}
		return &PruneBuildCacheOptions{Mode: mode}, nil
	}

	var options PruneBuildCacheOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	return &options, nil
}

// PruneAllResult is the result of a prune operation on Docker system resources.
type PruneAllResult struct {
	// ContainersPruned is a list of container IDs that were pruned.
	//
	// Required: false
	ContainersPruned []string `json:"containersPruned,omitempty"`

	// ImagesDeleted is a list of image IDs that were deleted.
	//
	// Required: false
	ImagesDeleted []string `json:"imagesDeleted,omitempty"`

	// VolumesDeleted is a list of volume IDs that were deleted.
	//
	// Required: false
	VolumesDeleted []string `json:"volumesDeleted,omitempty"`

	// NetworksDeleted is a list of network IDs that were deleted.
	//
	// Required: false
	NetworksDeleted []string `json:"networksDeleted,omitempty"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed uint64 `json:"spaceReclaimed"`

	// ContainerSpaceReclaimed is the amount of space reclaimed from containers in bytes.
	//
	// Required: false
	ContainerSpaceReclaimed uint64 `json:"containerSpaceReclaimed,omitempty"`

	// ImageSpaceReclaimed is the amount of space reclaimed from images in bytes.
	//
	// Required: false
	ImageSpaceReclaimed uint64 `json:"imageSpaceReclaimed,omitempty"`

	// VolumeSpaceReclaimed is the amount of space reclaimed from volumes in bytes.
	//
	// Required: false
	VolumeSpaceReclaimed uint64 `json:"volumeSpaceReclaimed,omitempty"`

	// BuildCacheSpaceReclaimed is the amount of space reclaimed from build cache in bytes.
	//
	// Required: false
	BuildCacheSpaceReclaimed uint64 `json:"buildCacheSpaceReclaimed,omitempty"`

	// Success indicates if the prune operation was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Errors is a list of any errors encountered during the prune operation.
	//
	// Required: false
	Errors []string `json:"errors,omitempty"`
}

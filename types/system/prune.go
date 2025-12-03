package system

// PruneAllRequest is used to request pruning of Docker system resources.
type PruneAllRequest struct {
	// Containers indicates if containers should be pruned.
	//
	// Required: true
	Containers bool `json:"containers"`

	// Images indicates if images should be pruned.
	//
	// Required: true
	Images bool `json:"images"`

	// Volumes indicates if volumes should be pruned.
	//
	// Required: true
	Volumes bool `json:"volumes"`

	// Networks indicates if networks should be pruned.
	//
	// Required: true
	Networks bool `json:"networks"`

	// BuildCache indicates if build cache should be pruned.
	//
	// Required: true
	BuildCache bool `json:"buildCache"`

	// Dangling indicates if only dangling resources should be pruned.
	//
	// Required: true
	Dangling bool `json:"dangling"`
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

	// Success indicates if the prune operation was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Errors is a list of any errors encountered during the prune operation.
	//
	// Required: false
	Errors []string `json:"errors,omitempty"`
}

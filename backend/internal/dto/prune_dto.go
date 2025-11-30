package dto

type PruneSystemDto struct {
	Containers bool `json:"containers"`
	Images     bool `json:"images"`
	Volumes    bool `json:"volumes"`
	Networks   bool `json:"networks"`
	BuildCache bool `json:"buildCache"`
	Dangling   bool `json:"dangling"`
}

type PruneAllResult struct {
	ContainersPruned []string `json:"containersPruned,omitempty"`
	ImagesDeleted    []string `json:"imagesDeleted,omitempty"`
	VolumesDeleted   []string `json:"volumesDeleted,omitempty"`
	NetworksDeleted  []string `json:"networksDeleted,omitempty"`
	SpaceReclaimed   uint64   `json:"spaceReclaimed"`
	Success          bool     `json:"success"`
	Errors           []string `json:"errors,omitempty"`
}

// Alias for swagger docs
type PruneResultDto = PruneAllResult

type ContainerBulkActionResultDto struct {
	Started int      `json:"started,omitempty"`
	Stopped int      `json:"stopped,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

type UpgradeCheckResultDto struct {
	CanUpgrade bool   `json:"canUpgrade"`
	Error      bool   `json:"error"`
	Message    string `json:"message"`
}

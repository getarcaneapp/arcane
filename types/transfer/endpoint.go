package transfer

// Endpoint protocol: every environment (including the manager's local one)
// serves these operations; the manager relays between two of them.

// DataSource selects what an endpoint archives.
type DataSource struct {
	Kind      ResourceKind `json:"kind" required:"true" enum:"volume,project_dir"`
	Volume    string       `json:"volume,omitempty"`
	ProjectID string       `json:"projectId,omitempty"`
}

// DataTarget selects where an endpoint extracts an archive. Volumes are
// created by the import and labelled with the transfer identity; project
// directories are created under the projects directory.
type DataTarget struct {
	Kind       ResourceKind `json:"kind" required:"true" enum:"volume,project_dir"`
	Volume     string       `json:"volume,omitempty"`
	ProjectDir string       `json:"projectDir,omitempty" doc:"Directory name under the projects directory"`
}

// ExportRequest stages a source as a compressed archive on the endpoint.
type ExportRequest struct {
	TransferID string     `json:"transferId" required:"true"`
	Source     DataSource `json:"source" required:"true"`
}

// Export identifies a staged archive; the manager relays it by byte range.
type Export struct {
	ExportID string `json:"exportId"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// ExportRange is one byte range of a staged archive.
type ExportRange struct {
	Data []byte `json:"data"`
}

// ImportRequest extracts a completed upload session into a target after
// checking it against the source hash.
type ImportRequest struct {
	TransferID string     `json:"transferId" required:"true"`
	Target     DataTarget `json:"target" required:"true"`
	UploadID   string     `json:"uploadId" required:"true" doc:"Completed upload session of kind volume-backup"`
	SHA256     string     `json:"sha256" required:"true"`
}

// HoldRequest reserves one resource on the endpoint for a transfer.
type HoldRequest struct {
	TransferID string `json:"transferId" required:"true"`
	Kind       Kind   `json:"kind" required:"true" enum:"project,volume"`
	Resource   string `json:"resource" required:"true" doc:"Volume name or project ID"`
}

// StopConsumersRequest stops the listed containers gracefully (no SIGKILL).
type StopConsumersRequest struct {
	TransferID         string     `json:"transferId" required:"true"`
	Consumers          []Consumer `json:"consumers"`
	GracePeriodSeconds int        `json:"gracePeriodSeconds,omitempty"`
}

// ConsumerFailure explains why one container could not be stopped or started.
type ConsumerFailure struct {
	ContainerID string `json:"containerId"`
	Name        string `json:"name"`
	Error       string `json:"error"`
}

// StopConsumersResponse reports what stopped; on failure everything already
// stopped has been restarted where possible.
type StopConsumersResponse struct {
	Stopped []Consumer        `json:"stopped"`
	Failed  []ConsumerFailure `json:"failed,omitempty"`
}

// RestoreConsumersRequest restarts recorded consumers that were running.
type RestoreConsumersRequest struct {
	TransferID string     `json:"transferId" required:"true"`
	Consumers  []Consumer `json:"consumers"`
}

// RestoreConsumersResponse reports which consumers were restarted.
type RestoreConsumersResponse struct {
	Restored []string          `json:"restored"`
	Failed   []ConsumerFailure `json:"failed,omitempty"`
}

// VolumeInspection is the endpoint view of one named volume.
type VolumeInspection struct {
	Name      string            `json:"name"`
	Exists    bool              `json:"exists"`
	Driver    string            `json:"driver,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Anonymous bool              `json:"anonymous"`
	Internal  bool              `json:"internal" doc:"Arcane internal resource"`
	Consumers []Consumer        `json:"consumers"`
	SizeBytes int64             `json:"sizeBytes" doc:"-1 when unknown"`
	FileCount int64             `json:"fileCount" doc:"-1 when unknown"`
	Hold      *Hold             `json:"hold,omitempty"`
}

// ProjectServiceInspection is one compose service as seen on the endpoint.
type ProjectServiceInspection struct {
	Name          string   `json:"name"`
	ContainerName string   `json:"containerName,omitempty"`
	ContainerID   string   `json:"containerId,omitempty"`
	Image         string   `json:"image,omitempty"`
	BuildOnly     bool     `json:"buildOnly"`
	State         string   `json:"state,omitempty"`
	Running       bool     `json:"running"`
	DependsOn     []string `json:"dependsOn,omitempty"`
	Ports         []string `json:"ports,omitempty"`
	Networks      []string `json:"networks,omitempty"`
	Devices       []string `json:"devices,omitempty"`
	StopTimeout   int      `json:"stopTimeout,omitempty"`
}

// ProjectVolumeInspection is one compose volume declaration.
type ProjectVolumeInspection struct {
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	External     bool              `json:"external"`
	ExplicitName bool              `json:"explicitName"`
	Driver       string            `json:"driver,omitempty"`
	Options      map[string]string `json:"options,omitempty"`
	Exists       bool              `json:"exists"`
	SizeBytes    int64             `json:"sizeBytes"`
	FileCount    int64             `json:"fileCount"`
	Hold         *Hold             `json:"hold,omitempty"`
}

// ProjectBindInspection is one bind mount used by the project.
type ProjectBindInspection struct {
	Service       string `json:"service"`
	Source        string `json:"source"`
	Target        string `json:"target"`
	ReadOnly      bool   `json:"readOnly"`
	InsideProject bool   `json:"insideProject"`
	DockerSocket  bool   `json:"dockerSocket"`
	SizeBytes     int64  `json:"sizeBytes"`
	FileCount     int64  `json:"fileCount"`
}

// ProjectInspection is the endpoint view of one project.
type ProjectInspection struct {
	ProjectID        string                     `json:"projectId"`
	Name             string                     `json:"name"`
	Path             string                     `json:"path"`
	ComposeFile      string                     `json:"composeFile"`
	Status           string                     `json:"status"`
	Archived         bool                       `json:"archived"`
	GitOpsManaged    bool                       `json:"gitOpsManaged"`
	Services         []ProjectServiceInspection `json:"services"`
	Volumes          []ProjectVolumeInspection  `json:"volumes"`
	Binds            []ProjectBindInspection    `json:"binds"`
	ExternalNetworks []string                   `json:"externalNetworks,omitempty"`
	Profiles         []string                   `json:"profiles,omitempty"`
	OutsideFiles     []string                   `json:"outsideFiles,omitempty" doc:"Referenced files outside the project directory"`
	Images           []string                   `json:"images"`
	Containers       []Consumer                 `json:"containers"`
	DirSizeBytes     int64                      `json:"dirSizeBytes"`
	DirFileCount     int64                      `json:"dirFileCount"`
	Hold             *Hold                      `json:"hold,omitempty"`
}

// DestinationCheckRequest asks an endpoint whether a project could land there.
type DestinationCheckRequest struct {
	ProjectName    string   `json:"projectName" required:"true"`
	VolumeNames    []string `json:"volumeNames,omitempty"`
	ContainerNames []string `json:"containerNames,omitempty"`
	Ports          []string `json:"ports,omitempty"`
	Networks       []string `json:"networks,omitempty"`
	Images         []string `json:"images,omitempty"`
}

// DestinationCheckResponse lists collisions and missing prerequisites.
type DestinationCheckResponse struct {
	ProjectNameInUse   bool     `json:"projectNameInUse"`
	DirectoryInUse     bool     `json:"directoryInUse"`
	VolumesInUse       []string `json:"volumesInUse,omitempty"`
	ContainerNamesUsed []string `json:"containerNamesUsed,omitempty"`
	PortsInUse         []string `json:"portsInUse,omitempty"`
	MissingNetworks    []string `json:"missingNetworks,omitempty"`
	FreeBytes          int64    `json:"freeBytes" doc:"-1 when unknown"`
}

// ProjectRewrite describes the identity changes applied to imported files.
type ProjectRewrite struct {
	Name           string            `json:"name" required:"true"`
	SourcePath     string            `json:"sourcePath,omitempty" doc:"Absolute source project directory; bind sources under it move to the destination directory"`
	VolumeMappings map[string]string `json:"volumeMappings,omitempty"`
}

// RegisterProjectRequest turns an imported directory into a managed project.
type RegisterProjectRequest struct {
	TransferID string         `json:"transferId" required:"true"`
	ProjectDir string         `json:"projectDir" required:"true"`
	Rewrite    ProjectRewrite `json:"rewrite" required:"true"`
}

// RegisterProjectResponse returns the destination project identity.
type RegisterProjectResponse struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

// ProjectActionRequest addresses a transfer-owned or held project.
type ProjectActionRequest struct {
	TransferID string `json:"transferId" required:"true"`
}

// RemoveRequest deletes a volume or project under the transfer's hold.
type RemoveRequest struct {
	TransferID  string `json:"transferId" required:"true"`
	RemoveFiles bool   `json:"removeFiles,omitempty" doc:"Projects only"`
}

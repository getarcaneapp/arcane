// Package transfer defines the contracts for moving Compose projects and
// named volumes between Arcane environments: the manager-facing plan and
// transfer records, and the endpoint protocol every environment implements.
package transfer

import (
	"time"

	"github.com/getarcaneapp/arcane/types/v2/upload"
)

const (
	// ProtocolVersion is the endpoint protocol both environments must speak.
	ProtocolVersion = 1

	// ChunkSize bounds every relayed archive chunk so each hop stays an
	// ordinary buffered request on every transport.
	ChunkSize int64 = upload.DefaultChunkSize

	// ExportIdleTimeout removes staged exports the manager stopped reading.
	ExportIdleTimeout = 10 * time.Minute

	// DefaultStopGracePeriod bounds a graceful-only stop when a container has
	// no configured stop timeout.
	DefaultStopGracePeriod = 30 * time.Second

	// HoldKeyPrefix prefixes the per-node KV keys that reserve a resource.
	HoldKeyPrefix = "transfer_hold:"

	KindProject Kind = "project"
	KindVolume  Kind = "volume"

	ModeCopy Mode = "copy"
	ModeMove Mode = "move"

	StatusQueued         Status = "queued"
	StatusRunning        Status = "running"
	StatusSucceeded      Status = "succeeded"
	StatusFailed         Status = "failed"
	StatusCanceled       Status = "canceled"
	StatusNeedsAttention Status = "needs_attention"
	StatusRolledBack     Status = "rolled_back"

	PhasePending    Phase = "pending"
	PhaseRevalidate Phase = "revalidate"
	PhaseReserve    Phase = "reserve"
	PhasePrepare    Phase = "prepare"
	PhaseStop       Phase = "stop"
	PhaseCopy       Phase = "copy"
	PhaseCutover    Phase = "cutover"
	PhaseRecover    Phase = "recover"
	PhaseFinished   Phase = "finished"

	ResourceVolume     ResourceKind = "volume"
	ResourceProjectDir ResourceKind = "project_dir"

	ResourcePending  ResourceStatus = "pending"
	ResourceCopying  ResourceStatus = "copying"
	ResourceVerified ResourceStatus = "verified"
	ResourceFailed   ResourceStatus = "failed"

	AckConsumersStop      = "consumers_stop"
	AckMetadataLimits     = "metadata_limits"
	AckMoveLeavesSource   = "move_leaves_source_stopped"
	AckVolumeMoveNoAttach = "volume_move_no_attach"
	AckExternalCutover    = "external_cutover"
	AckSameDaemon         = "same_daemon"
)

type (
	Kind           string
	Mode           string
	Status         string
	Phase          string
	ResourceKind   string
	ResourceStatus string
)

// Terminal reports whether no further automatic work happens for the status.
func (s Status) Terminal() bool {
	switch s { //nolint:exhaustive // active and attention states are the non-terminal default
	case StatusSucceeded, StatusFailed, StatusCanceled, StatusRolledBack:
		return true
	default:
		return false
	}
}

// Capabilities describes what an endpoint supports; a 404 on the capabilities
// route means the endpoint predates transfers entirely.
type Capabilities struct {
	Protocol  int    `json:"protocol"`
	ChunkSize int64  `json:"chunkSize"`
	DaemonID  string `json:"daemonId"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Consumer is a container that mounts a transferred volume or belongs to a
// transferred project, recorded before the transfer stops it.
type Consumer struct {
	ContainerID    string `json:"containerId"`
	Name           string `json:"name"`
	Running        bool   `json:"running"`
	RestartPolicy  string `json:"restartPolicy,omitempty"`
	ComposeProject string `json:"composeProject,omitempty"`
	ComposeService string `json:"composeService,omitempty"`
	StopTimeout    int    `json:"stopTimeout,omitempty"`
}

// Hold is a durable reservation on one resource held by one transfer.
type Hold struct {
	TransferID string    `json:"transferId"`
	Kind       Kind      `json:"kind"`
	Resource   string    `json:"resource"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Blocker is a hard preflight failure.
type Blocker struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Resource string `json:"resource,omitempty"`
}

// ReviewItem is a preflight finding the user must see; Required items must be
// acknowledged by code before a transfer is created.
type ReviewItem struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Resource string `json:"resource,omitempty"`
	Required bool   `json:"required"`
}

// PlannedResource is one data set the transfer copies.
type PlannedResource struct {
	Key             string       `json:"key"`
	Kind            ResourceKind `json:"kind"`
	SourceName      string       `json:"sourceName"`
	DestinationName string       `json:"destinationName"`
	EstimatedBytes  int64        `json:"estimatedBytes"`
	EstimatedFiles  int64        `json:"estimatedFiles"`
}

// Request is what the user asks for; preflight turns it into a Plan.
type Request struct {
	Kind                     Kind              `json:"kind" required:"true" enum:"project,volume"`
	Mode                     Mode              `json:"mode" required:"true" enum:"copy,move"`
	DestinationEnvironmentID string            `json:"destinationEnvironmentId" required:"true"`
	ProjectID                string            `json:"projectId,omitempty"`
	VolumeName               string            `json:"volumeName,omitempty"`
	DestinationName          string            `json:"destinationName" required:"true" doc:"Destination project or volume name"`
	VolumeMappings           map[string]string `json:"volumeMappings,omitempty" doc:"Source volume name to destination volume name for explicitly named or external project volumes"`
}

// Plan is the reviewed preflight result. PlanHash covers every field except
// itself; create rejects a stale hash.
type Plan struct {
	Request                    Request             `json:"request"`
	SourceEnvironmentID        string              `json:"sourceEnvironmentId"`
	SourceEnvironmentName      string              `json:"sourceEnvironmentName"`
	DestinationEnvironmentName string              `json:"destinationEnvironmentName"`
	SourceCapabilities         Capabilities        `json:"sourceCapabilities"`
	DestinationCapabilities    Capabilities        `json:"destinationCapabilities"`
	Resources                  []PlannedResource   `json:"resources"`
	Consumers                  []Consumer          `json:"consumers"`
	DestinationServices        []string            `json:"destinationServices,omitempty"`
	ServiceDependencies        map[string][]string `json:"serviceDependencies,omitempty" doc:"Compose depends_on per service, used to order stops"`
	Blockers                   []Blocker           `json:"blockers"`
	Reviews                    []ReviewItem        `json:"reviews"`
	RequiredAcknowledgements   []string            `json:"requiredAcknowledgements"`
	EstimatedBytes             int64               `json:"estimatedBytes"`
	EstimatedFiles             int64               `json:"estimatedFiles"`
	RequiresDowntime           bool                `json:"requiresDowntime"`
	PlanHash                   string              `json:"planHash"`
}

// Blocked reports whether the plan has any hard blocker.
func (p Plan) Blocked() bool { return len(p.Blockers) > 0 }

// CreateRequest submits an approved plan.
type CreateRequest struct {
	IdempotencyKey   string   `json:"idempotencyKey" required:"true" minLength:"8" maxLength:"128"`
	PlanHash         string   `json:"planHash" required:"true"`
	Request          Request  `json:"request" required:"true"`
	Acknowledgements []string `json:"acknowledgements,omitempty"`
}

// ResourceProgress is the persisted per-resource checkpoint.
type ResourceProgress struct {
	Key              string         `json:"key"`
	Kind             ResourceKind   `json:"kind"`
	SourceName       string         `json:"sourceName"`
	DestinationName  string         `json:"destinationName"`
	Status           ResourceStatus `json:"status"`
	Attempt          int            `json:"attempt"`
	BytesTransferred int64          `json:"bytesTransferred"`
	BytesTotal       int64          `json:"bytesTotal"`
	SHA256           string         `json:"sha256,omitempty" doc:"Hash of the relayed archive"`
	Error            string         `json:"error,omitempty"`
}

// RecoveryReport describes what automatic recovery managed to restore.
type RecoveryReport struct {
	SourceRestored     bool     `json:"sourceRestored"`
	DestinationRemoved bool     `json:"destinationRemoved"`
	Incomplete         []string `json:"incomplete,omitempty"`
	DestinationUnknown bool     `json:"destinationUnknown"`
}

// Transfer is the API view of a transfer record.
type Transfer struct {
	ID                          string             `json:"id"`
	Kind                        Kind               `json:"kind"`
	Mode                        Mode               `json:"mode"`
	SourceEnvironmentID         string             `json:"sourceEnvironmentId"`
	DestinationEnvironmentID    string             `json:"destinationEnvironmentId"`
	Status                      Status             `json:"status"`
	Phase                       Phase              `json:"phase"`
	Attempt                     int                `json:"attempt"`
	Plan                        Plan               `json:"plan"`
	Resources                   []ResourceProgress `json:"resources"`
	RecordedConsumers           []Consumer         `json:"recordedConsumers,omitempty"`
	DestinationProjectID        string             `json:"destinationProjectId,omitempty"`
	DestinationStartupAttempted bool               `json:"destinationStartupAttempted"`
	SourceHeld                  bool               `json:"sourceHeld"`
	CancelRequested             bool               `json:"cancelRequested"`
	Recovery                    *RecoveryReport    `json:"recovery,omitempty"`
	ActivityID                  string             `json:"activityId,omitempty"`
	Error                       string             `json:"error,omitempty"`
	RequestedBy                 string             `json:"requestedBy"`
	CreatedAt                   time.Time          `json:"createdAt"`
	UpdatedAt                   *time.Time         `json:"updatedAt,omitempty"`
	FinishedAt                  *time.Time         `json:"finishedAt,omitempty"`
}

// RollbackRequest restores the recorded source state after a Move.
type RollbackRequest struct {
	AcknowledgeDestinationWritesDiscarded bool `json:"acknowledgeDestinationWritesDiscarded" required:"true"`
}

// CleanupRequest selects what to delete on the source after a successful Move.
// Nothing is selected by default.
type CleanupRequest struct {
	RemoveSourceProject bool     `json:"removeSourceProject,omitempty"`
	RemoveSourceFiles   bool     `json:"removeSourceFiles,omitempty"`
	RemoveSourceVolumes []string `json:"removeSourceVolumes,omitempty"`
}

// CleanupSkip explains why one selected resource was not removed.
type CleanupSkip struct {
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
}

// CleanupResponse reports what cleanup removed and what it refused.
type CleanupResponse struct {
	Removed []string      `json:"removed"`
	Skipped []CleanupSkip `json:"skipped"`
}

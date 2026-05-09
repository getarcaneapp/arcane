// Package mobile implements the gRPC services exposed to paired mobile clients
// (iOS, etc.). Dependencies flow in via callback functions wired up by the
// bootstrap layer — this package never imports from internal/ so it stays
// reusable and testable in isolation, mirroring pkg/libarcane/edge/.
package mobile

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors callbacks may return; the server maps these to gRPC status
// codes via error_mapping.go.
var (
	ErrInvalidCode      = errors.New("invalid pairing code")
	ErrCodeExpired      = errors.New("pairing code expired")
	ErrCodeRedeemed     = errors.New("pairing code already redeemed")
	ErrInvalidToken     = errors.New("invalid device token")
	ErrDeviceRevoked    = errors.New("device has been revoked")
	ErrDeviceNotFound   = errors.New("device not found")
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrRateLimited      = errors.New("too many pairing attempts")
	ErrEnvironmentLocal = errors.New("only the local environment is supported")
	ErrNotFound         = errors.New("resource not found")
)

// Device is the transport DTO for a paired device.
type Device struct {
	ID          string
	Name        string
	DeviceID    string
	AppVersion  string
	OsVersion   string
	DeviceModel string
	PairedAt    time.Time
	LastSeenAt  *time.Time
}

// RedeemInput is what the server passes to the CodeRedeemer callback.
type RedeemInput struct {
	Code        string
	DeviceID    string
	DeviceName  string
	AppVersion  string
	OsVersion   string
	DeviceModel string
}

// RedeemOutput is what the CodeRedeemer callback returns on success.
type RedeemOutput struct {
	DeviceToken string
	Device      Device
	Username    string
	ServerURL   string
}

// ServerInfo is the payload of MobileService.GetServerInfo.
type ServerInfo struct {
	ServerVersion    string
	ServerRevision   string
	DockerVersion    string
	DockerAPIVersion string
	OS               string
	Arch             string
	EnvironmentCount int32
}

// ContainerCounts is a per-state count summary.
type ContainerCounts struct {
	Running int32
	Stopped int32
	Paused  int32
	Total   int32
}

// ListContainersInput is what the server passes to the ContainerLister callback.
type ListContainersInput struct {
	EnvironmentID   string
	IncludeAll      bool
	IncludeInternal bool
	Search          string
	Limit           int32
	Offset          int32
	GroupBy         string
}

// ListContainersOutput is what the ContainerLister callback returns. The
// containers payload is opaque JSON so existing Swift Codable types decode
// it unchanged.
type ListContainersOutput struct {
	ContainersJSON []byte
	Counts         ContainerCounts
	Total          int64
}

// VolumeSize is a single entry in GetVolumeSizesResponse.
type VolumeSize struct {
	Name      string
	SizeBytes int64
	RefCount  int64
}

// ---------- Callback function types ----------

// Pairing-adjacent
type (
	TokenValidator    func(ctx context.Context, rawToken string) (userID, deviceID string, err error)
	CodeRedeemer      func(ctx context.Context, in RedeemInput) (RedeemOutput, error)
	DeviceLookup      func(ctx context.Context, deviceID string) (Device, error)
	DeviceRevoker     func(ctx context.Context, deviceID string) error
	LastSeenTouch     func(ctx context.Context, deviceID string)
	ServerInfoFetcher func(ctx context.Context) (ServerInfo, error)
)

// JSONFetcher returns a JSON payload for an envID-scoped endpoint. Used by
// the many RPCs that return opaque JSON to iOS (DockerInfo, ListVolumes,
// ListNetworks, etc.).
type JSONFetcher func(ctx context.Context, envID string) ([]byte, error)

// ResourceJSONFetcher fetches JSON for a single resource within an env.
type ResourceJSONFetcher func(ctx context.Context, envID, id string) ([]byte, error)

// ContainerLister lists containers for the given environment.
type ContainerLister func(ctx context.Context, in ListContainersInput) (ListContainersOutput, error)

// SimpleEnvAction performs a no-payload mutating action on a single resource.
type SimpleEnvAction func(ctx context.Context, envID, id string) error

// DeleteContainerAction is the per-container delete signature.
type DeleteContainerAction func(ctx context.Context, envID, id string, force, removeVolumes bool) error

// CreateResourceAction creates a resource from a JSON spec and returns the
// created resource as JSON.
type CreateResourceAction func(ctx context.Context, envID string, spec []byte) ([]byte, error)

// DeleteVolumeAction is the volume delete signature (force flag).
type DeleteVolumeAction func(ctx context.Context, envID, name string, force bool) error

// VolumeSizesFetcher returns per-volume size and ref-count info.
type VolumeSizesFetcher func(ctx context.Context, envID string) ([]VolumeSize, error)

// AppVersionFetcher returns the global app version JSON (same as GET /app-version).
type AppVersionFetcher func(ctx context.Context) ([]byte, error)

// Generic shapes used by the bulk JSON-passthrough RPCs (Tier 2/3).

// EnvIDFetcher is a fetcher keyed by env ID only.
type EnvIDFetcher = JSONFetcher

// EnvIDIDFetcher returns JSON for an envID + id pair (e.g., InspectImage).
type EnvIDIDFetcher = ResourceJSONFetcher

// EnvIDQueryFetcher returns JSON for an envID + raw query string.
type EnvIDQueryFetcher func(ctx context.Context, envID, query string) ([]byte, error)

// EnvIDIDQueryFetcher returns JSON for envID + id + query.
type EnvIDIDQueryFetcher func(ctx context.Context, envID, id, query string) ([]byte, error)

// EnvIDBodyFetcher takes a JSON body and returns JSON.
type EnvIDBodyFetcher func(ctx context.Context, envID string, body []byte) ([]byte, error)

// EnvIDIDBodyFetcher takes (envID, id, body).
type EnvIDIDBodyFetcher func(ctx context.Context, envID, id string, body []byte) ([]byte, error)

// EnvIDIDAction is a no-payload action keyed by (envID, id).
type EnvIDIDAction = SimpleEnvAction

// IDFetcher returns JSON for a global resource ID.
type IDFetcher func(ctx context.Context, id string) ([]byte, error)

// IDBodyFetcher takes (id, body).
type IDBodyFetcher func(ctx context.Context, id string, body []byte) ([]byte, error)

// IDAction is a no-payload action keyed by id.
type IDAction func(ctx context.Context, id string) error

// BodyFetcher takes a body, returns JSON.
type BodyFetcher func(ctx context.Context, body []byte) ([]byte, error)

// EmptyFetcher returns JSON with no input parameters.
type EmptyFetcher func(ctx context.Context) ([]byte, error)

// RenameContainerAction renames a container.
type RenameContainerAction func(ctx context.Context, envID, id, newName string) error

// PullImageStreamer streams pull progress to a frame writer.
type PullImageStreamer func(ctx context.Context, envID, ref string, authJSON []byte, send func(chunk []byte) error) error

// LogStreamer streams logs to a frame writer.
type LogStreamer func(ctx context.Context, envID, id string, opts LogOptions, send func(chunk []byte) error) error

// StatsStreamer streams stats samples to a frame writer.
type StatsStreamer func(ctx context.Context, envID, id string, send func(chunk []byte) error) error

// SystemStatsStreamer streams aggregated system stats.
type SystemStatsStreamer func(ctx context.Context, envID string, intervalMs int32, send func(chunk []byte) error) error

// LogOptions are the options for log streaming.
type LogOptions struct {
	Follow     bool
	Tail       string
	Timestamps bool
	Stdout     bool
	Stderr     bool
	Since      string
	Until      string
}

// TerminalSession is the duplex callback for the bidi terminal RPC. The
// implementation reads input frames from `recv` and writes output via `send`.
type TerminalSession func(ctx context.Context, in TerminalSessionInput, recv func() ([]byte, error), send func([]byte) error) error

// TerminalSessionInput is what the server hands the TerminalSession callback
// after processing the initial Start frame.
type TerminalSessionInput struct {
	EnvID       string
	ContainerID string
	Shell       string
	Cols        uint32
	Rows        uint32
}

// Callbacks bundles every callback the MobileServer needs. The bootstrap
// layer constructs this struct, wiring each field to the corresponding
// internal/services method.
type Callbacks struct {
	// Pairing / auth
	ValidateToken TokenValidator
	RedeemCode    CodeRedeemer
	LookupDevice  DeviceLookup
	RevokeDevice  DeviceRevoker
	TouchLastSeen LastSeenTouch

	// System / version
	ServerInfo GetServerInfo
	DockerInfo JSONFetcher
	AppVersion AppVersionFetcher

	// Containers
	ListContainer     ContainerLister
	InspectContainer  ResourceJSONFetcher
	StartContainer    SimpleEnvAction
	StopContainer     SimpleEnvAction
	RestartContainer  SimpleEnvAction
	RedeployContainer SimpleEnvAction
	DeleteContainer   DeleteContainerAction
	PruneContainers   JSONFetcher

	// Volumes
	ListVolumes    JSONFetcher
	GetVolumeSizes VolumeSizesFetcher
	CreateVolume   CreateResourceAction
	DeleteVolume   DeleteVolumeAction
	PruneVolumes   JSONFetcher

	// Networks
	ListNetworks  JSONFetcher
	CreateNetwork CreateResourceAction
	DeleteNetwork SimpleEnvAction
	PruneNetworks JSONFetcher

	// Projects (read + mutations)
	ListProjects   JSONFetcher
	GetProject     ResourceJSONFetcher
	CreateProject  EnvIDBodyFetcher
	UpdateProject  EnvIDIDBodyFetcher
	DeleteProject  EnvIDIDAction
	StartProject   EnvIDIDAction
	StopProject    EnvIDIDAction
	DestroyProject EnvIDIDAction

	// Container extras
	PauseContainer   EnvIDIDAction
	UnpauseContainer EnvIDIDAction
	KillContainer    EnvIDIDAction
	RenameContainer  RenameContainerAction

	// Images
	ListImages   EnvIDFetcher
	InspectImage EnvIDIDFetcher
	DeleteImage  EnvIDIDAction
	PruneImages  EnvIDFetcher

	// Image updates
	GetImageUpdateSummary EnvIDFetcher
	GetImageUpdatesByRefs func(ctx context.Context, envID string, refs []string) ([]byte, error)
	CheckImageUpdates     EnvIDQueryFetcher
	CheckAllImageUpdates  EnvIDIDAction // signature is (ctx, envID, "") — id ignored
	CheckImageUpdate      EnvIDIDFetcher

	// Vulnerabilities
	GetVulnerabilityScannerStatus EnvIDFetcher
	GetImageVulnerabilitySummary  EnvIDIDFetcher
	ListImageVulnerabilities      EnvIDIDQueryFetcher
	ScanImageVulnerabilities      EnvIDIDFetcher
	GetAllVulnerabilitiesSummary  EnvIDFetcher
	GetVulnerabilityImageOptions  EnvIDQueryFetcher
	ListAllVulnerabilities        EnvIDQueryFetcher
	IgnoreVulnerability           EnvIDBodyFetcher
	DeleteVulnerabilityIgnore     EnvIDIDAction

	// Environments
	ListEnvironments  EmptyFetcher
	CreateEnvironment BodyFetcher
	TestEnvironment   EnvIDFetcher

	// Settings
	GetSettings    EnvIDFetcher
	UpdateSettings EnvIDBodyFetcher
	GetOidcStatus  EmptyFetcher

	// Notifications
	GetNotificationSettings    EnvIDFetcher
	SaveNotificationProvider   EnvIDBodyFetcher
	DeleteNotificationProvider EnvIDIDAction
	TestNotificationProvider   EnvIDIDBodyFetcher
	GetApprise                 EnvIDFetcher
	UpdateApprise              EnvIDBodyFetcher
	TestApprise                EnvIDBodyFetcher

	// Webhooks
	ListWebhooks  EnvIDFetcher
	CreateWebhook EnvIDBodyFetcher
	UpdateWebhook EnvIDIDBodyFetcher
	DeleteWebhook EnvIDIDAction

	// Users (global)
	ListUsers  EmptyFetcher
	CreateUser BodyFetcher
	UpdateUser IDBodyFetcher
	DeleteUser IDAction

	// API keys (global)
	ListApiKeys  EmptyFetcher
	CreateApiKey BodyFetcher
	DeleteApiKey IDAction

	// Container registries (global)
	ListContainerRegistries EmptyFetcher
	CreateContainerRegistry BodyFetcher
	UpdateContainerRegistry IDBodyFetcher
	DeleteContainerRegistry IDAction

	// Templates (global)
	ListTemplates          EmptyFetcher
	GetTemplateContent     IDFetcher
	ListTemplateRegistries EmptyFetcher
	CreateTemplateRegistry BodyFetcher
	UpdateTemplateRegistry IDBodyFetcher
	DeleteTemplateRegistry IDAction

	// System
	PruneSystem EnvIDBodyFetcher

	// Streaming
	StreamContainerLogs  LogStreamer
	StreamContainerStats StatsStreamer
	StreamProjectLogs    LogStreamer
	StreamSystemStats    SystemStatsStreamer
	StreamPullImage      PullImageStreamer
	TerminalSession      TerminalSession
}

// GetServerInfo is the previous name for ServerInfoFetcher; exported as an
// alias so existing wiring keeps compiling while we add new callbacks.
type GetServerInfo = ServerInfoFetcher

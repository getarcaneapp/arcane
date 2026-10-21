package types

import (
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

// ContainerSetAutoUpdateRequest configures container auto-update.
type ContainerSetAutoUpdateRequest struct {
	Enabled bool `json:"enabled" doc:"Whether auto-update is enabled for this container"`
}

// ContainerRestartPolicyCreate represents restart policy options for container creation.
type ContainerRestartPolicyCreate struct {
	// Name of the restart policy.
	//
	// Required: false
	Name string `json:"name,omitempty"`

	// MaximumRetryCount is only used when name is on-failure.
	//
	// Required: false
	MaximumRetryCount int `json:"maximumRetryCount,omitempty"`
}

// ContainerPortBindingCreate represents host port bindings for container creation.
type ContainerPortBindingCreate struct {
	// HostIP is the IP address to bind to on the host.
	//
	// Required: false
	HostIP string `json:"hostIp,omitempty"`

	// HostPort is the port on the host.
	//
	// Required: false
	HostPort string `json:"hostPort,omitempty"`
}

// ContainerHostConfigCreate represents host configuration for container creation.
type ContainerHostConfigCreate struct {
	// Binds is a list of volume bindings.
	//
	// Required: false
	Binds []string `json:"binds,omitempty"`

	// PortBindings maps container ports to host bindings.
	//
	// Required: false
	PortBindings map[string][]ContainerPortBindingCreate `json:"portBindings,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy *ContainerRestartPolicyCreate `json:"restartPolicy,omitempty"`

	// NetworkMode for the container.
	//
	// Required: false
	NetworkMode string `json:"networkMode,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged *bool `json:"privileged,omitempty"`

	// AutoRemove indicates if the container is removed when stopped.
	//
	// Required: false
	AutoRemove *bool `json:"autoRemove,omitempty"`

	// Memory limit in bytes.
	//
	// Required: false
	Memory int64 `json:"memory,omitempty"`

	// MemorySwap limits total memory usage (memory + swap) in bytes.
	//
	// Required: false
	MemorySwap int64 `json:"memorySwap,omitempty"`

	// NanoCPUs is CPU allocation in nano CPUs.
	//
	// Required: false
	NanoCPUs int64 `json:"nanoCpus,omitempty"`

	// CPUShares is the relative CPU share weight.
	//
	// Required: false
	CPUShares int64 `json:"cpuShares,omitempty"`

	// ReadonlyRootfs makes the root filesystem read-only.
	//
	// Required: false
	ReadonlyRootfs *bool `json:"readonlyRootfs,omitempty"`

	// PublishAllPorts publishes all exposed ports to random host ports.
	//
	// Required: false
	PublishAllPorts *bool `json:"publishAllPorts,omitempty"`

	// CapAdd is a list of kernel capabilities to add.
	//
	// Required: false
	CapAdd []string `json:"capAdd,omitempty"`

	// CapDrop is a list of kernel capabilities to drop.
	//
	// Required: false
	CapDrop []string `json:"capDrop,omitempty"`

	// Mounts is a list of typed mounts (bind or volume).
	//
	// Required: false
	Mounts []ContainerMountCreate `json:"mounts,omitempty"`
}

// ContainerEndpointSettingsCreate represents network endpoint settings for container creation.
type ContainerEndpointSettingsCreate struct {
	// Aliases for the container on this network.
	//
	// Required: false
	Aliases []string `json:"aliases,omitempty"`

	// IPv4Address is a static IPv4 address for the container on this network.
	//
	// Required: false
	IPv4Address string `json:"ipv4Address,omitempty"`

	// IPv6Address is a static IPv6 address for the container on this network.
	//
	// Required: false
	IPv6Address string `json:"ipv6Address,omitempty"`
}

// ContainerMountCreate represents a typed mount (bind or volume) for container creation.
type ContainerMountCreate struct {
	// Type of the mount (bind or volume).
	//
	// Required: true
	Type string `json:"type"`

	// Source is the host path (bind) or volume name (volume).
	//
	// Required: true
	Source string `json:"source"`

	// Target path inside the container.
	//
	// Required: true
	Target string `json:"target"`

	// ReadOnly mounts the target read-only.
	//
	// Required: false
	ReadOnly bool `json:"readOnly,omitempty"`
}

// ContainerHealthcheckCreate represents a container healthcheck configuration for
// create/edit requests. Duration values are expressed in seconds on the wire.
type ContainerHealthcheckCreate struct {
	// Test is the probe command. ["NONE"] disables an image-defined healthcheck.
	//
	// Required: false
	Test []string `json:"test,omitempty"`

	// Interval between probes, in seconds.
	//
	// Required: false
	Interval int64 `json:"interval,omitempty"`

	// Timeout for a single probe, in seconds.
	//
	// Required: false
	Timeout int64 `json:"timeout,omitempty"`

	// StartPeriod is the initialization grace period, in seconds.
	//
	// Required: false
	StartPeriod int64 `json:"startPeriod,omitempty"`

	// StartInterval is the probe interval during the start period, in seconds.
	//
	// Required: false
	StartInterval int64 `json:"startInterval,omitempty"`

	// Retries is the number of consecutive failures before unhealthy.
	//
	// Required: false
	Retries int `json:"retries,omitempty"`
}

// ContainerNetworkingConfigCreate represents network configuration for container creation.
type ContainerNetworkingConfigCreate struct {
	// EndpointsConfig maps network names to endpoint settings.
	//
	// Required: false
	EndpointsConfig map[string]ContainerEndpointSettingsCreate `json:"endpointsConfig,omitempty"`
}

// Container is the writable representation of a container.
type Container struct {
	// Name of the container.
	//
	// Required: false
	Name string `json:"name" required:"false"`

	// Image to use for the container.
	//
	// Required: false
	Image string `json:"image" required:"false"`

	// Command to run in the container.
	//
	// Required: false
	Command []string `json:"command,omitempty"`

	// Cmd is an alias for Command.
	//
	// Required: false
	Cmd []string `json:"cmd,omitempty"`

	// Entrypoint for the container.
	//
	// Required: false
	Entrypoint []string `json:"entrypoint,omitempty"`

	// WorkingDir is the working directory inside the container.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`

	// User to run the container as.
	//
	// Required: false
	User string `json:"user,omitempty"`

	// Environment variables for the container.
	//
	// Required: false
	Environment []string `json:"environment,omitempty"`

	// Env is an alias for Environment.
	//
	// Required: false
	Env []string `json:"env,omitempty"`

	// Labels to set on the container.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty"`

	// ExposedPorts are ports exposed by the container.
	//
	// Required: false
	ExposedPorts map[string]struct{} `json:"exposedPorts,omitempty"`

	// Healthcheck overrides the image healthcheck. Test ["NONE"] disables it.
	//
	// Required: false
	Healthcheck *ContainerHealthcheckCreate `json:"healthcheck,omitempty"`

	// HostConfig holds advanced host-level settings.
	//
	// Required: false
	HostConfig *ContainerHostConfigCreate `json:"hostConfig,omitempty"`

	// NetworkingConfig defines network endpoints.
	//
	// Required: false
	NetworkingConfig *ContainerNetworkingConfigCreate `json:"networkingConfig,omitempty"`

	// Hostname for the container.
	//
	// Required: false
	Hostname string `json:"hostname,omitempty"`

	// Domainname for the container.
	//
	// Required: false
	Domainname string `json:"domainname,omitempty"`

	// AttachStdout attaches stdout.
	//
	// Required: false
	AttachStdout bool `json:"attachStdout,omitempty"`

	// AttachStderr attaches stderr.
	//
	// Required: false
	AttachStderr bool `json:"attachStderr,omitempty"`

	// AttachStdin attaches stdin.
	//
	// Required: false
	AttachStdin bool `json:"attachStdin,omitempty"`

	// Tty allocates a pseudo-TTY.
	//
	// Required: false
	Tty bool `json:"tty,omitempty"`

	// OpenStdin keeps stdin open.
	//
	// Required: false
	OpenStdin bool `json:"openStdin,omitempty"`

	// StdinOnce closes stdin after first attach.
	//
	// Required: false
	StdinOnce bool `json:"stdinOnce,omitempty"`

	// NetworkDisabled disables networking.
	//
	// Required: false
	NetworkDisabled bool `json:"networkDisabled,omitempty"`

	// Ports is a map of port bindings.
	//
	// Required: false
	Ports map[string]string `json:"ports,omitempty"`

	// Volumes is a list of volume mounts.
	//
	// Required: false
	Volumes []string `json:"volumes,omitempty"`

	// Networks is a list of networks to connect to.
	//
	// Required: false
	Networks []string `json:"networks,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy string `json:"restartPolicy,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged bool `json:"privileged,omitempty"`

	// AutoRemove indicates if the container should be removed when stopped.
	//
	// Required: false
	AutoRemove bool `json:"autoRemove,omitempty"`

	// Memory limit for the container in bytes.
	//
	// Required: false
	Memory int64 `json:"memory,omitempty"`

	// CPUs is the number of CPUs to allocate.
	//
	// Required: false
	CPUs float64 `json:"cpus,omitempty"`

	// Credentials for pulling images from private registries.
	//
	// Required: false
	Credentials []ContainerRegistryCredential `json:"credentials,omitempty" writeOnly:"true"`
}

// ContainerCommitRequest is used to create an image from a container's current filesystem.
type ContainerCommitRequest struct {
	// Repository is the target image repository.
	//
	// Required: false
	Repository string `json:"repository,omitempty" doc:"Target image repository"`

	// Tag is the target image tag.
	//
	// Required: false
	Tag string `json:"tag,omitempty" doc:"Target image tag"`

	// Comment records why the image was committed.
	//
	// Required: false
	Comment string `json:"comment,omitempty" doc:"Commit comment"`

	// Author records who created the committed image.
	//
	// Required: false
	Author string `json:"author,omitempty" doc:"Commit author"`

	// Changes contains Dockerfile-style changes to apply during commit.
	//
	// Required: false
	Changes []string `json:"changes,omitempty" doc:"Dockerfile changes to apply"`

	// NoPause disables the default pause during commit.
	//
	// Required: false
	NoPause bool `json:"noPause,omitempty" doc:"Do not pause the container during commit"`
}

// ContainerCommitResult identifies the image created by a container commit.
type ContainerCommitResult struct {
	ID string `json:"id"`
}

// ContainerGenerateComposeRequest selects the containers to convert into a compose project.
type ContainerGenerateComposeRequest struct {
	ContainerIDs []string `json:"containerIds" minItems:"1" doc:"IDs of the containers to convert"`
}

// ContainerGenerateComposeResponse carries the compose file generated from containers.
type ContainerGenerateComposeResponse struct {
	ComposeContent string `json:"composeContent"`
}

// ContainerStatusCounts contains counts of containers by status.
type ContainerStatusCounts struct {
	// RunningContainers is the number of running containers.
	//
	// Required: true
	RunningContainers int `json:"runningContainers"`

	// StoppedContainers is the number of stopped containers.
	//
	// Required: true
	StoppedContainers int `json:"stoppedContainers"`

	// TotalContainers is the total number of containers.
	//
	// Required: true
	TotalContainers int `json:"totalContainers"`
}

// ContainerActionResult represents the result of a container action (start/stop/etc).
type ContainerActionResult struct {
	// Started is a list of container IDs that were started.
	//
	// Required: false
	Started []string `json:"started,omitempty"`

	// Stopped is a list of container IDs that were stopped.
	//
	// Required: false
	Stopped []string `json:"stopped,omitempty"`

	// Failed is a list of container IDs that failed.
	//
	// Required: false
	Failed []string `json:"failed,omitempty"`

	// Success indicates if the overall action was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Errors is a list of error messages encountered.
	//
	// Required: false
	Errors []string `json:"errors,omitempty"`

	// ActivityID is the background activity that tracked this action.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
}

// ContainerPort represents a port binding for a container.
type ContainerPort struct {
	// IP address the port is bound to.
	//
	// Required: false
	IP string `json:"ip,omitempty"`

	// PrivatePort is the port inside the container.
	//
	// Required: true
	PrivatePort int `json:"privatePort"`

	// PublicPort is the port on the host.
	//
	// Required: false
	PublicPort int `json:"publicPort,omitempty"`

	// Type is the protocol type (tcp/udp).
	//
	// Required: true
	Type string `json:"type"`
}

// ContainerMount represents a volume mount for a container.
type ContainerMount struct {
	// Type of the mount (bind, volume, tmpfs).
	//
	// Required: true
	Type string `json:"type"`

	// Name of the volume (for volume mounts).
	//
	// Required: false
	Name string `json:"name,omitempty"`

	// Source path on the host.
	//
	// Required: false
	Source string `json:"source,omitempty"`

	// Destination path in the container.
	//
	// Required: true
	Destination string `json:"destination"`

	// Driver is the volume driver (for volume mounts).
	//
	// Required: false
	Driver string `json:"driver,omitempty"`

	// Mode specifies mount permissions.
	//
	// Required: false
	Mode string `json:"mode,omitempty"`

	// RW indicates if the mount is read-write.
	//
	// Required: false
	RW bool `json:"rw,omitempty"`

	// Propagation mode for the mount.
	//
	// Required: false
	Propagation string `json:"propagation,omitempty"`
}

// ContainerNetworkEndpoint represents network endpoint settings for a container.
type ContainerNetworkEndpoint struct {
	// IPAMConfig contains IP address management configuration.
	//
	// Required: false
	IPAMConfig any `json:"ipamConfig,omitempty"`

	// Links to other containers.
	//
	// Required: false
	Links []string `json:"links,omitempty"`

	// Aliases for the container on this network.
	//
	// Required: false
	Aliases []string `json:"aliases,omitempty"`

	// MacAddress of the container on this network.
	//
	// Required: false
	MacAddress string `json:"macAddress,omitempty"`

	// DriverOpts contains driver-specific options.
	//
	// Required: false
	DriverOpts map[string]string `json:"driverOpts,omitempty"`

	// GwPriority is the gateway priority.
	//
	// Required: false
	GwPriority int `json:"gwPriority,omitempty"`

	// NetworkID is the ID of the network.
	//
	// Required: false
	NetworkID string `json:"networkId,omitempty"`

	// EndpointID is the ID of this endpoint.
	//
	// Required: false
	EndpointID string `json:"endpointId,omitempty"`

	// Gateway address for the network.
	//
	// Required: false
	Gateway string `json:"gateway,omitempty"`

	// IPAddress assigned to the container.
	//
	// Required: false
	IPAddress string `json:"ipAddress,omitempty"`

	// IPPrefixLen is the IP prefix length.
	//
	// Required: false
	IPPrefixLen int `json:"ipPrefixLen,omitempty"`

	// IPv6Gateway address for the network.
	//
	// Required: false
	IPv6Gateway string `json:"ipv6Gateway,omitempty"`

	// GlobalIPv6Address assigned to the container.
	//
	// Required: false
	GlobalIPv6Address string `json:"globalIPv6Address,omitempty"`

	// GlobalIPv6PrefixLen is the IPv6 prefix length.
	//
	// Required: false
	GlobalIPv6PrefixLen int `json:"globalIPv6PrefixLen,omitempty"`

	// DNSNames are DNS names for this endpoint.
	//
	// Required: false
	DNSNames []string `json:"dnsNames,omitempty"`
}

// ContainerNetworkSettings contains network configuration for a container.
type ContainerNetworkSettings struct {
	// Networks is a map of network name to endpoint settings.
	//
	// Required: true
	Networks map[string]ContainerNetworkEndpoint `json:"networks"`
}

// ContainerState represents the state of a container.
type ContainerState struct {
	// Status is the current status of the container.
	//
	// Required: true
	Status string `json:"status"`

	// Running indicates if the container is running.
	//
	// Required: true
	Running bool `json:"running"`

	// ExitCode is the exit code of the container process.
	//
	// Required: false
	ExitCode int `json:"exitCode,omitempty"`

	// StartedAt is when the container was started.
	//
	// Required: false
	StartedAt string `json:"startedAt,omitempty"`

	// FinishedAt is when the container finished.
	//
	// Required: false
	FinishedAt string `json:"finishedAt,omitempty"`

	// Health contains the healthcheck status, if the container defines one.
	//
	// Required: false
	Health *ContainerHealth `json:"health,omitempty"`
}

// ContainerHealthLogEntry represents a single probe result from the healthcheck log.
type ContainerHealthLogEntry struct {
	// Start is when the probe started.
	//
	// Required: false
	Start string `json:"start,omitempty"`

	// End is when the probe finished.
	//
	// Required: false
	End string `json:"end,omitempty"`

	// ExitCode is the exit code of the probe command.
	//
	// Required: false
	ExitCode int `json:"exitCode"`

	// Output is the stdout/stderr captured from the probe.
	//
	// Required: false
	Output string `json:"output,omitempty"`
}

// ContainerHealth represents the current healthcheck state of a container.
type ContainerHealth struct {
	// Status is the current healthcheck status (starting, healthy, unhealthy, none).
	//
	// Required: true
	Status string `json:"status"`

	// FailingStreak is the number of consecutive failures.
	//
	// Required: false
	FailingStreak int `json:"failingStreak"`

	// Log is the recent probe history, oldest first.
	//
	// Required: false
	Log []ContainerHealthLogEntry `json:"log,omitempty"`
}

// ContainerHealthcheck represents a container's healthcheck configuration.
// Duration values are expressed in nanoseconds (Docker SDK convention).
type ContainerHealthcheck struct {
	// Test is the probe command. Common forms:
	//  - empty / nil: inherit from the image
	//  - ["NONE"]: healthcheck disabled
	//  - ["CMD", "..."] / ["CMD-SHELL", "..."]: probe command
	//
	// Required: false
	Test []string `json:"test,omitempty"`

	// Interval between probes, in nanoseconds.
	//
	// Required: false
	Interval int64 `json:"interval,omitempty"`

	// Timeout for a single probe, in nanoseconds.
	//
	// Required: false
	Timeout int64 `json:"timeout,omitempty"`

	// StartPeriod is the initialization grace period, in nanoseconds.
	//
	// Required: false
	StartPeriod int64 `json:"startPeriod,omitempty"`

	// StartInterval is the probe interval during the start period, in nanoseconds.
	//
	// Required: false
	StartInterval int64 `json:"startInterval,omitempty"`

	// Retries is the number of consecutive failures before the container is marked unhealthy.
	//
	// Required: false
	Retries int `json:"retries,omitempty"`
}

// ContainerConfig represents configuration details for a container.
type ContainerConfig struct {
	// Env is a list of environment variables.
	//
	// Required: false
	Env []string `json:"env,omitempty"`

	// Cmd is the command to run.
	//
	// Required: false
	Cmd []string `json:"cmd,omitempty"`

	// Entrypoint is the entrypoint for the container.
	//
	// Required: false
	Entrypoint []string `json:"entrypoint,omitempty"`

	// WorkingDir is the working directory.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`

	// User to run as.
	//
	// Required: false
	User string `json:"user,omitempty"`

	// Hostname of the container.
	//
	// Required: false
	Hostname string `json:"hostname,omitempty"`

	// Healthcheck is the container's healthcheck configuration.
	//
	// Required: false
	Healthcheck *ContainerHealthcheck `json:"healthcheck,omitempty"`
}

// ContainerHostConfig represents host configuration for a container.
type ContainerHostConfig struct {
	// NetworkMode for the container.
	//
	// Required: false
	NetworkMode string `json:"networkMode,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy string `json:"restartPolicy,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged bool `json:"privileged,omitempty"`

	// AutoRemove indicates if the container is removed when stopped.
	//
	// Required: false
	AutoRemove bool `json:"autoRemove,omitempty"`

	// NanoCPUs is CPU allocation in nano CPUs.
	//
	// Required: false
	NanoCPUs int64 `json:"nanoCpus,omitempty"`

	// Memory limit in bytes.
	//
	// Required: false
	Memory int64 `json:"memory,omitempty"`
}

// Resource sort keys accepted by the container list endpoint. They are part of
// the shared API contract: the manager's environment proxy inspects the sort
// query parameter to enforce the additional containers:read permission before
// forwarding resource-sorted requests to an agent.
const (
	ContainerSortCPUUsage    = "cpuUsage"
	ContainerSortMemoryUsage = "memoryUsage"
)

// IsContainerResourceSort reports whether sort is a resource-based sort key.
func IsContainerResourceSort(sort string) bool {
	return sort == ContainerSortCPUUsage || sort == ContainerSortMemoryUsage
}

// ContainerResourceSample is a point-in-time resource usage sample for a container,
// returned with resource-sorted list responses so ordering and displayed
// values come from the same data.
type ContainerResourceSample struct {
	// CPUPercent is the normalized CPU usage percentage (0-100 scale).
	//
	// Required: true
	CPUPercent float64 `json:"cpuPercent"`

	// MemoryUsageBytes is the memory usage in bytes.
	//
	// Required: true
	MemoryUsageBytes uint64 `json:"memoryUsageBytes"`

	// MemoryLimitBytes is the memory limit in bytes.
	//
	// Required: true
	MemoryLimitBytes uint64 `json:"memoryLimitBytes"`

	// SampleTime is when the sample was taken.
	//
	// Required: true
	SampleTime time.Time `json:"sampleTime"`
}

// ContainerResourceSampleSubscription is the client frame on the batched container stats
// stream naming the containers to sample. Each frame replaces the previous set.
type ContainerResourceSampleSubscription struct {
	// ContainerIDs are the containers to sample.
	//
	// Required: true
	ContainerIDs []string `json:"containerIds"`
}

// ContainerResourceSampleBatch is one server frame on the batched container stats stream.
type ContainerResourceSampleBatch struct {
	// Samples maps container ID to its sample. Stopped, unknown and unsampled
	// containers are omitted.
	//
	// Required: true
	Samples map[string]*ContainerResourceSample `json:"samples"`

	// Error describes why this round could not be sampled.
	//
	// Required: false
	Error string `json:"error,omitempty"`

	// Timestamp is when the batch was sent.
	//
	// Required: true
	Timestamp time.Time `json:"timestamp"`
}

// ContainerSummary represents a container summary.
type ContainerSummary struct {
	EnvironmentTag

	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Names is a list of names for the container.
	//
	// Required: true
	Names []string `json:"names"`

	// Image used by the container.
	//
	// Required: true
	Image string `json:"image"`

	// ImageID is the ID of the image.
	//
	// Required: true
	ImageID string `json:"imageId"`

	// Command running in the container.
	//
	// Required: true
	Command string `json:"command"`

	// Created is the Unix timestamp when the container was created.
	//
	// Required: true
	Created int64 `json:"created"`

	// Ports exposed by the container.
	//
	// Required: true
	Ports []ContainerPort `json:"ports"`

	// Labels contains user-defined metadata.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// State is the current state of the container.
	//
	// Required: true
	State string `json:"state"`

	// Status provides a human-readable status.
	//
	// Required: true
	Status string `json:"status"`

	// HostConfig contains host configuration.
	//
	// Required: true
	HostConfig ContainerHostConfig `json:"hostConfig"`

	// NetworkSettings contains network configuration.
	//
	// Required: true
	NetworkSettings ContainerNetworkSettings `json:"networkSettings"`

	// Mounts lists volume mounts.
	//
	// Required: true
	Mounts []ContainerMount `json:"mounts"`

	// IconLightURL is the resolved light icon URL for dark themes.
	//
	// Required: false
	IconLightURL string `json:"iconLightUrl,omitempty"`

	// IconDarkURL is the resolved dark icon URL for light themes.
	//
	// Required: false
	IconDarkURL string `json:"iconDarkUrl,omitempty"`

	// UpdateStrategy is the strategy resolved by the updater for this image and policy.
	UpdateStrategy string `json:"updateStrategy,omitempty"`

	// UpdateInfo contains image update information for this container.
	//
	// Required: false
	UpdateInfo *ImageUpdateInfo `json:"updateInfo,omitempty"`

	// RedeployDisabled indicates whether redeploy actions are disabled for this container.
	//
	// Required: false
	RedeployDisabled bool `json:"redeployDisabled,omitempty"`

	// AutoUpdateEnabled reports whether the container is eligible for auto-update
	// under its updater label and the excluded-containers setting. It does not
	// reflect whether the auto-update scheduler itself is enabled.
	//
	// Required: true
	AutoUpdateEnabled bool `json:"autoUpdateEnabled"`

	// Hidden indicates whether the container is hidden from lists and dashboard counts.
	//
	// Required: false
	Hidden bool `json:"hidden,omitempty"`

	// ResourceSample holds the resource usage sample collected for a
	// resource-sorted list request. Nil when the request was not resource-sorted
	// or the sample was unavailable (stopped container or collection failure).
	//
	// Required: false
	ResourceSample *ContainerResourceSample `json:"resourceSample,omitempty"`
}

// ContainerComposeInfo contains Docker Compose project information extracted from container labels.
type ContainerComposeInfo struct {
	// ProjectName is the name of the Docker Compose project.
	//
	// Required: true
	ProjectName string `json:"projectName"`

	// ServiceName is the name of the service within the Compose project.
	//
	// Required: true
	ServiceName string `json:"serviceName"`

	// WorkingDir is the working directory of the Compose project.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`

	// ConfigFiles is the list of Compose config file paths for the project.
	//
	// Required: false
	ConfigFiles string `json:"configFiles,omitempty"`
}

// ContainerSummaryGroup represents a group of container summaries.
type ContainerSummaryGroup struct {
	// GroupName is the group label, such as a compose project name.
	//
	// Required: true
	GroupName string `json:"groupName"`

	// Items contains the container summaries in the group.
	//
	// Required: true
	Items []ContainerSummary `json:"items"`
}

// ContainerDetails represents detailed container information.
type ContainerDetails struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the container.
	//
	// Required: true
	Name string `json:"name"`

	// Image used by the container.
	//
	// Required: true
	Image string `json:"image"`

	// ImageID is the ID of the image.
	//
	// Required: true
	ImageID string `json:"imageId"`

	// Created is when the container was created.
	//
	// Required: true
	Created string `json:"created"`

	// State contains the container's current state.
	//
	// Required: true
	State ContainerState `json:"state"`

	// Config contains container configuration.
	//
	// Required: true
	Config ContainerConfig `json:"config"`

	// HostConfig contains host-level configuration.
	//
	// Required: true
	HostConfig ContainerHostConfig `json:"hostConfig"`

	// NetworkSettings contains network configuration.
	//
	// Required: true
	NetworkSettings ContainerNetworkSettings `json:"networkSettings"`

	// Ports exposed by the container.
	//
	// Required: true
	Ports []ContainerPort `json:"ports"`

	// Mounts lists volume mounts.
	//
	// Required: true
	Mounts []ContainerMount `json:"mounts"`

	// Labels contains user-defined metadata.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty"`

	// ComposeInfo contains Docker Compose project information.
	// Only present if container is part of a Compose project.
	//
	// Required: false
	ComposeInfo *ContainerComposeInfo `json:"composeInfo,omitempty"`

	// IconLightURL is the resolved light icon URL for dark themes.
	//
	// Required: false
	IconLightURL string `json:"iconLightUrl,omitempty"`

	// IconDarkURL is the resolved dark icon URL for light themes.
	//
	// Required: false
	IconDarkURL string `json:"iconDarkUrl,omitempty"`

	// RedeployDisabled indicates whether redeploy actions are disabled for this container.
	//
	// Required: false
	RedeployDisabled bool `json:"redeployDisabled,omitempty"`

	// AutoUpdateEnabled reports whether the container is eligible for auto-update
	// under its updater label and the excluded-containers setting. It does not
	// reflect whether the auto-update scheduler itself is enabled.
	//
	// Required: true
	AutoUpdateEnabled bool `json:"autoUpdateEnabled"`

	// UpdateInfo is the stored update check matching this container's current policy.
	//
	// Required: false
	UpdateInfo *ImageUpdateInfo `json:"updateInfo,omitempty"`

	// ActivityID is the background activity that tracked the action returning these details.
	//
	// Required: false
	ActivityID *string `json:"activityId,omitempty"`
}

// ContainerCreated represents a newly created container.
type ContainerCreated struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the container.
	//
	// Required: true
	Name string `json:"name"`

	// Image used by the container.
	//
	// Required: true
	Image string `json:"image"`

	// Status of the container.
	//
	// Required: true
	Status string `json:"status"`

	// Created is when the container was created.
	//
	// Required: true
	Created string `json:"created"`
}

// NewContainerSummary creates a ContainerSummary from a docker container.Summary.
func NewContainerSummary(c container.Summary) ContainerSummary {
	names := make([]string, 0, len(c.Names))
	for _, name := range c.Names {
		names = append(names, strings.TrimPrefix(name, "/"))
	}

	ports := make([]ContainerPort, 0, len(c.Ports))
	for _, p := range c.Ports {
		hostIP := ""
		if p.IP.IsValid() {
			hostIP = p.IP.String()
		}
		ports = append(ports, ContainerPort{
			IP:          hostIP,
			PrivatePort: int(p.PrivatePort),
			PublicPort:  int(p.PublicPort),
			Type:        p.Type,
		})
	}

	mounts := make([]ContainerMount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, ContainerMount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			Driver:      m.Driver,
			Mode:        m.Mode,
			RW:          m.RW,
			Propagation: string(m.Propagation),
		})
	}

	networks := map[string]ContainerNetworkEndpoint{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	return ContainerSummary{
		ID:      c.ID,
		Names:   names,
		Image:   c.Image,
		ImageID: c.ImageID,
		Command: c.Command,
		Created: c.Created,
		Ports:   ports,
		Labels:  c.Labels,
		State:   string(c.State),
		Status:  c.Status,
		HostConfig: ContainerHostConfig{
			NetworkMode: c.HostConfig.NetworkMode,
		},
		NetworkSettings: ContainerNetworkSettings{
			Networks: networks,
		},
		Mounts: mounts,
	}
}

// NewContainerDetails creates a ContainerDetails from a docker container.InspectResponse.
func NewContainerDetails(c *container.InspectResponse) ContainerDetails {
	cfg, labels, imageName := mapInspectConfig(c.Config)

	return ContainerDetails{
		ID:         c.ID,
		Name:       strings.TrimPrefix(c.Name, "/"),
		Image:      imageName,
		ImageID:    c.Image,
		Created:    c.Created,
		State:      mapInspectState(c.State),
		Config:     cfg,
		HostConfig: mapInspectHostConfig(c.HostConfig),
		NetworkSettings: ContainerNetworkSettings{
			Networks: mapInspectNetworks(c.NetworkSettings),
		},
		Ports:       mapInspectPorts(c.NetworkSettings),
		Mounts:      mapInspectMounts(c.Mounts),
		Labels:      labels,
		ComposeInfo: mapComposeInfo(labels),
	}
}

func mapInspectPorts(networkSettings *container.NetworkSettings) []ContainerPort {
	ports := make([]ContainerPort, 0)
	if networkSettings == nil || networkSettings.Ports == nil {
		return ports
	}

	for p, bindings := range networkSettings.Ports {
		privatePort := int(p.Num())
		typ := string(p.Proto())

		// When no host bindings exist, still include the private port.
		if len(bindings) == 0 {
			ports = append(ports, ContainerPort{
				PrivatePort: privatePort,
				Type:        typ,
			})
			continue
		}

		for _, b := range bindings {
			pub, _ := strconv.Atoi(b.HostPort)
			hostIP := ""
			if b.HostIP.IsValid() {
				hostIP = b.HostIP.String()
			}
			ports = append(ports, ContainerPort{
				IP:          hostIP,
				PrivatePort: privatePort,
				PublicPort:  pub,
				Type:        typ,
			})
		}
	}

	return ports
}

func mapInspectMounts(mountPoints []container.MountPoint) []ContainerMount {
	mounts := make([]ContainerMount, 0, len(mountPoints))
	for _, m := range mountPoints {
		mounts = append(mounts, ContainerMount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			Driver:      m.Driver,
			Mode:        m.Mode,
			RW:          m.RW,
			Propagation: string(m.Propagation),
		})
	}

	return mounts
}

func mapInspectNetworks(networkSettings *container.NetworkSettings) map[string]ContainerNetworkEndpoint {
	networks := map[string]ContainerNetworkEndpoint{}
	if networkSettings == nil || networkSettings.Networks == nil {
		return networks
	}

	for name, n := range networkSettings.Networks {
		networks[name] = mapEndpointSettings(n)
	}

	return networks
}

func mapInspectHostConfig(hostConfig *container.HostConfig) ContainerHostConfig {
	if hostConfig == nil {
		return ContainerHostConfig{}
	}

	return ContainerHostConfig{
		RestartPolicy: string(hostConfig.RestartPolicy.Name),
		Privileged:    hostConfig.Privileged,
		AutoRemove:    hostConfig.AutoRemove,
		NanoCPUs:      hostConfig.NanoCPUs,
		Memory:        hostConfig.Memory,
	}
}

func mapInspectConfig(config *container.Config) (ContainerConfig, map[string]string, string) {
	labels := map[string]string{}
	if config == nil {
		return ContainerConfig{}, labels, ""
	}

	cfg := ContainerConfig{
		Env:        append([]string{}, config.Env...),
		Cmd:        append([]string{}, config.Cmd...),
		Entrypoint: append([]string{}, config.Entrypoint...),
		WorkingDir: config.WorkingDir,
		User:       config.User,
		Hostname:   config.Hostname,
	}

	if hc := config.Healthcheck; hc != nil {
		cfg.Healthcheck = &ContainerHealthcheck{
			Test:          append([]string{}, hc.Test...),
			Interval:      int64(hc.Interval),
			Timeout:       int64(hc.Timeout),
			StartPeriod:   int64(hc.StartPeriod),
			StartInterval: int64(hc.StartInterval),
			Retries:       hc.Retries,
		}
	}

	if config.Labels != nil {
		maps.Copy(labels, config.Labels)
	}

	return cfg, labels, config.Image
}

func mapInspectState(state *container.State) ContainerState {
	if state == nil {
		return ContainerState{}
	}

	mappedState := ContainerState{
		Status:     string(state.Status),
		Running:    state.Running,
		ExitCode:   state.ExitCode,
		StartedAt:  state.StartedAt,
		FinishedAt: state.FinishedAt,
	}

	if state.Health != nil {
		mappedState.Health = mapInspectHealth(state.Health)
	}

	return mappedState
}

func mapInspectHealth(health *container.Health) *ContainerHealth {
	log := make([]ContainerHealthLogEntry, 0, len(health.Log))
	for _, entry := range health.Log {
		if entry == nil {
			continue
		}

		log = append(log, ContainerHealthLogEntry{
			Start:    formatTimeOrEmpty(entry.Start),
			End:      formatTimeOrEmpty(entry.End),
			ExitCode: entry.ExitCode,
			Output:   entry.Output,
		})
	}

	return &ContainerHealth{
		Status:        string(health.Status),
		FailingStreak: health.FailingStreak,
		Log:           log,
	}
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05.999999999Z07:00")
}

func mapComposeInfo(labels map[string]string) *ContainerComposeInfo {
	projectName, hasProject := labels["com.docker.compose.project"]
	if !hasProject {
		return nil
	}

	serviceName, hasService := labels["com.docker.compose.service"]
	if !hasService {
		return nil
	}

	composeInfo := &ContainerComposeInfo{
		ProjectName: projectName,
		ServiceName: serviceName,
	}
	if workingDir, ok := labels["com.docker.compose.project.working_dir"]; ok {
		composeInfo.WorkingDir = workingDir
	}
	if configFiles, ok := labels["com.docker.compose.project.config_files"]; ok {
		composeInfo.ConfigFiles = configFiles
	}

	return composeInfo
}

func mapEndpointSettings(n *network.EndpointSettings) ContainerNetworkEndpoint {
	if n == nil {
		return ContainerNetworkEndpoint{}
	}

	var driverOpts map[string]string
	if n.DriverOpts != nil {
		driverOpts = n.DriverOpts
	}

	gateway := ""
	if n.Gateway.IsValid() {
		gateway = n.Gateway.String()
	}

	ipAddress := ""
	if n.IPAddress.IsValid() {
		ipAddress = n.IPAddress.String()
	}

	ipv6Gateway := ""
	if n.IPv6Gateway.IsValid() {
		ipv6Gateway = n.IPv6Gateway.String()
	}

	globalIPv6Address := ""
	if n.GlobalIPv6Address.IsValid() {
		globalIPv6Address = n.GlobalIPv6Address.String()
	}

	return ContainerNetworkEndpoint{
		IPAMConfig:          n.IPAMConfig,
		Links:               n.Links,
		Aliases:             n.Aliases,
		MacAddress:          n.MacAddress.String(),
		DriverOpts:          driverOpts,
		GwPriority:          n.GwPriority,
		NetworkID:           n.NetworkID,
		EndpointID:          n.EndpointID,
		Gateway:             gateway,
		IPAddress:           ipAddress,
		IPPrefixLen:         n.IPPrefixLen,
		IPv6Gateway:         ipv6Gateway,
		GlobalIPv6Address:   globalIPv6Address,
		GlobalIPv6PrefixLen: n.GlobalIPv6PrefixLen,
		DNSNames:            n.DNSNames,
	}
}

// ContainerHostConfigEdit carries host-level configuration changes for a container
// edit. Each field is a pointer: nil preserves the existing value, non-nil
// (even empty) replaces it.
type ContainerHostConfigEdit struct {
	// Binds is the full desired list of volume bindings.
	//
	// Required: false
	Binds *[]string `json:"binds,omitempty"`

	// Mounts is the full desired list of typed mounts.
	//
	// Required: false
	Mounts *[]ContainerMountCreate `json:"mounts,omitempty"`

	// PortBindings is the full desired port binding map.
	//
	// Required: false
	PortBindings *map[string][]ContainerPortBindingCreate `json:"portBindings,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy *ContainerRestartPolicyCreate `json:"restartPolicy,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged *bool `json:"privileged,omitempty"`

	// CapAdd is the full desired list of added kernel capabilities.
	//
	// Required: false
	CapAdd *[]string `json:"capAdd,omitempty"`

	// CapDrop is the full desired list of dropped kernel capabilities.
	//
	// Required: false
	CapDrop *[]string `json:"capDrop,omitempty"`

	// AutoRemove indicates if the container is removed when stopped.
	//
	// Required: false
	AutoRemove *bool `json:"autoRemove,omitempty"`

	// ReadonlyRootfs makes the root filesystem read-only.
	//
	// Required: false
	ReadonlyRootfs *bool `json:"readonlyRootfs,omitempty"`

	// Memory limit in bytes.
	//
	// Required: false
	Memory *int64 `json:"memory,omitempty"`

	// MemorySwap limits total memory usage (memory + swap) in bytes.
	//
	// Required: false
	MemorySwap *int64 `json:"memorySwap,omitempty"`

	// NanoCPUs is CPU allocation in nano CPUs.
	//
	// Required: false
	NanoCPUs *int64 `json:"nanoCpus,omitempty"`

	// CPUShares is the relative CPU share weight.
	//
	// Required: false
	CPUShares *int64 `json:"cpuShares,omitempty"`
}

// ContainerEdit describes configuration changes to apply when recreating a container.
// Each form-owned section is a pointer: nil preserves the inspected value,
// non-nil (even empty) replaces that section. Settings not represented here
// (sysctls, ulimits, log drivers, devices, tmpfs, DNS, ...) always carry over
// from the existing container untouched.
type ContainerEdit struct {
	// Name is the new container name.
	//
	// Required: false
	Name *string `json:"name,omitempty"`

	// Image reference to run.
	//
	// Required: false
	Image *string `json:"image,omitempty"`

	// WorkingDir is the working directory inside the container.
	//
	// Required: false
	WorkingDir *string `json:"workingDir,omitempty"`

	// User to run the container as.
	//
	// Required: false
	User *string `json:"user,omitempty"`

	// Hostname for the container. An empty value clears it.
	//
	// Required: false
	Hostname *string `json:"hostname,omitempty"`

	// Command to run in the container.
	//
	// Required: false
	Command *[]string `json:"command,omitempty"`

	// Entrypoint for the container.
	//
	// Required: false
	Entrypoint *[]string `json:"entrypoint,omitempty"`

	// Environment variables for the container.
	//
	// Required: false
	Environment *[]string `json:"environment,omitempty"`

	// Labels to set on the container.
	//
	// Required: false
	Labels *map[string]string `json:"labels,omitempty"`

	// Healthcheck replaces the container healthcheck when set.
	//
	// Required: false
	Healthcheck *ContainerHealthcheckCreate `json:"healthcheck,omitempty"`

	// ClearHealthcheck reverts to the image-defined healthcheck.
	//
	// Required: false
	ClearHealthcheck bool `json:"clearHealthcheck,omitempty"`

	// HostConfig holds host-level configuration changes.
	//
	// Required: false
	HostConfig *ContainerHostConfigEdit `json:"hostConfig,omitempty"`

	// NetworkingConfig is the full desired network endpoint set. Nil preserves
	// the existing attachments.
	//
	// Required: false
	NetworkingConfig *ContainerNetworkingConfigCreate `json:"networkingConfig,omitempty"`

	// Credentials for pulling images from private registries.
	//
	// Required: false
	Credentials []ContainerRegistryCredential `json:"credentials,omitempty"`
}

// ContainerEditConfigNetwork describes one desired network attachment as shown in the
// edit form.
type ContainerEditConfigNetwork struct {
	// Aliases for the container on this network.
	//
	// Required: false
	Aliases []string `json:"aliases,omitempty"`

	// IPv4Address is the configured static IPv4 address, if any.
	//
	// Required: false
	IPv4Address string `json:"ipv4Address,omitempty"`

	// IPv6Address is the configured static IPv6 address, if any.
	//
	// Required: false
	IPv6Address string `json:"ipv6Address,omitempty"`
}

// ContainerEditConfigHostConfig is the host-config subset surfaced by the edit form.
type ContainerEditConfigHostConfig struct {
	// Binds is the list of volume bindings.
	//
	// Required: false
	Binds []string `json:"binds,omitempty"`

	// Mounts is the list of typed mounts.
	//
	// Required: false
	Mounts []ContainerMountCreate `json:"mounts,omitempty"`

	// PortBindings maps container ports to host bindings.
	//
	// Required: false
	PortBindings map[string][]ContainerPortBindingCreate `json:"portBindings,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy ContainerRestartPolicyCreate `json:"restartPolicy"`

	// NetworkMode for the container.
	//
	// Required: false
	NetworkMode string `json:"networkMode,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged bool `json:"privileged,omitempty"`

	// CapAdd is the list of added kernel capabilities.
	//
	// Required: false
	CapAdd []string `json:"capAdd,omitempty"`

	// CapDrop is the list of dropped kernel capabilities.
	//
	// Required: false
	CapDrop []string `json:"capDrop,omitempty"`

	// AutoRemove indicates if the container is removed when stopped.
	//
	// Required: false
	AutoRemove bool `json:"autoRemove,omitempty"`

	// ReadonlyRootfs indicates a read-only root filesystem.
	//
	// Required: false
	ReadonlyRootfs bool `json:"readonlyRootfs,omitempty"`

	// Memory limit in bytes.
	//
	// Required: false
	Memory int64 `json:"memory,omitempty"`

	// MemorySwap limits total memory usage (memory + swap) in bytes.
	//
	// Required: false
	MemorySwap int64 `json:"memorySwap,omitempty"`

	// NanoCPUs is CPU allocation in nano CPUs.
	//
	// Required: false
	NanoCPUs int64 `json:"nanoCpus,omitempty"`

	// CPUShares is the relative CPU share weight.
	//
	// Required: false
	CPUShares int64 `json:"cpuShares,omitempty"`
}

// ContainerEditConfig is the read DTO backing the container edit form.
type ContainerEditConfig struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the container.
	//
	// Required: true
	Name string `json:"name"`

	// Image reference the container was created from.
	//
	// Required: true
	Image string `json:"image"`

	// Command the container runs.
	//
	// Required: false
	Command []string `json:"command,omitempty"`

	// Entrypoint for the container.
	//
	// Required: false
	Entrypoint []string `json:"entrypoint,omitempty"`

	// WorkingDir is the working directory inside the container.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`

	// User the container runs as.
	//
	// Required: false
	User string `json:"user,omitempty"`

	// Hostname of the container, omitted when it is the Docker default.
	//
	// Required: false
	Hostname string `json:"hostname,omitempty"`

	// Environment variables of the container.
	//
	// Required: false
	Environment []string `json:"environment,omitempty"`

	// Labels set on the container.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty"`

	// Healthcheck configuration (durations in seconds), if the container
	// defines one.
	//
	// Required: false
	Healthcheck *ContainerHealthcheckCreate `json:"healthcheck,omitempty"`

	// HostConfig is the host-config subset surfaced by the edit form.
	//
	// Required: true
	HostConfig ContainerEditConfigHostConfig `json:"hostConfig"`

	// Networks maps network names to their desired endpoint settings.
	//
	// Required: false
	Networks map[string]ContainerEditConfigNetwork `json:"networks,omitempty"`

	// IsCompose indicates the container is managed by a compose project.
	//
	// Required: false
	IsCompose bool `json:"isCompose,omitempty"`

	// ComposeProject is the owning compose project name, if any.
	//
	// Required: false
	ComposeProject string `json:"composeProject,omitempty"`

	// EditDisabled indicates edit actions are disabled for this container.
	//
	// Required: false
	EditDisabled bool `json:"editDisabled,omitempty"`

	// Running indicates whether the container is currently running.
	//
	// Required: true
	Running bool `json:"running"`
}

// NewContainerEditConfigFromInspect builds an EditConfig from a docker inspect response.
func NewContainerEditConfigFromInspect(c *container.InspectResponse, isCompose, editDisabled bool) ContainerEditConfig {
	cfg := ContainerEditConfig{
		ID:           c.ID,
		Name:         strings.TrimPrefix(c.Name, "/"),
		IsCompose:    isCompose,
		EditDisabled: editDisabled,
		Running:      c.State != nil && c.State.Running,
	}

	if c.Config != nil {
		cfg.Image = c.Config.Image
		cfg.Command = append([]string{}, c.Config.Cmd...)
		cfg.Entrypoint = append([]string{}, c.Config.Entrypoint...)
		cfg.WorkingDir = c.Config.WorkingDir
		cfg.User = c.Config.User
		if len(c.ID) < 12 || c.Config.Hostname != c.ID[:12] {
			cfg.Hostname = c.Config.Hostname
		}
		cfg.Environment = append([]string{}, c.Config.Env...)
		cfg.Labels = c.Config.Labels
		cfg.ComposeProject = c.Config.Labels["com.docker.compose.project"]
		cfg.Healthcheck = mapEditHealthcheck(c.Config.Healthcheck)
	}

	cfg.HostConfig = mapEditHostConfig(c.HostConfig)
	cfg.Networks = mapEditNetworks(c)

	return cfg
}

func mapEditHealthcheck(hc *container.HealthConfig) *ContainerHealthcheckCreate {
	if hc == nil {
		return nil
	}

	return &ContainerHealthcheckCreate{
		Test:          append([]string{}, hc.Test...),
		Interval:      int64(hc.Interval / time.Second),
		Timeout:       int64(hc.Timeout / time.Second),
		StartPeriod:   int64(hc.StartPeriod / time.Second),
		StartInterval: int64(hc.StartInterval / time.Second),
		Retries:       hc.Retries,
	}
}

func mapEditHostConfig(hc *container.HostConfig) ContainerEditConfigHostConfig {
	if hc == nil {
		return ContainerEditConfigHostConfig{}
	}

	out := ContainerEditConfigHostConfig{
		Binds: append([]string{}, hc.Binds...),
		RestartPolicy: ContainerRestartPolicyCreate{
			Name:              string(hc.RestartPolicy.Name),
			MaximumRetryCount: hc.RestartPolicy.MaximumRetryCount,
		},
		NetworkMode:    string(hc.NetworkMode),
		Privileged:     hc.Privileged,
		CapAdd:         append([]string{}, hc.CapAdd...),
		CapDrop:        append([]string{}, hc.CapDrop...),
		AutoRemove:     hc.AutoRemove,
		ReadonlyRootfs: hc.ReadonlyRootfs,
		Memory:         hc.Memory,
		MemorySwap:     hc.MemorySwap,
		NanoCPUs:       hc.NanoCPUs,
		CPUShares:      hc.CPUShares,
	}

	for _, m := range hc.Mounts {
		out.Mounts = append(out.Mounts, ContainerMountCreate{
			Type:     string(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	if len(hc.PortBindings) > 0 {
		out.PortBindings = make(map[string][]ContainerPortBindingCreate, len(hc.PortBindings))
		for port, bindings := range hc.PortBindings {
			mapped := make([]ContainerPortBindingCreate, 0, len(bindings))
			for _, b := range bindings {
				hostIP := ""
				if b.HostIP.IsValid() {
					hostIP = b.HostIP.String()
				}
				mapped = append(mapped, ContainerPortBindingCreate{
					HostIP:   hostIP,
					HostPort: b.HostPort,
				})
			}
			out.PortBindings[port.String()] = mapped
		}
	}

	return out
}

func mapEditNetworks(c *container.InspectResponse) map[string]ContainerEditConfigNetwork {
	if c.NetworkSettings == nil || len(c.NetworkSettings.Networks) == 0 {
		return nil
	}

	shortID := ""
	if len(c.ID) >= 12 {
		shortID = c.ID[:12]
	}

	out := make(map[string]ContainerEditConfigNetwork, len(c.NetworkSettings.Networks))
	for name, endpoint := range c.NetworkSettings.Networks {
		mapped := ContainerEditConfigNetwork{}
		if endpoint != nil {
			// The daemon injects the container short ID as an implicit alias;
			// it is regenerated on recreate and must not round-trip as a
			// user-defined alias.
			for _, alias := range endpoint.Aliases {
				if alias == "" || alias == shortID {
					continue
				}
				mapped.Aliases = append(mapped.Aliases, alias)
			}
			if endpoint.IPAMConfig != nil {
				if endpoint.IPAMConfig.IPv4Address.IsValid() {
					mapped.IPv4Address = endpoint.IPAMConfig.IPv4Address.String()
				}
				if endpoint.IPAMConfig.IPv6Address.IsValid() {
					mapped.IPv6Address = endpoint.IPAMConfig.IPv6Address.String()
				}
			}
		}
		out[name] = mapped
	}

	return out
}

// ContainerProcesses is the process snapshot Docker reports for a running container.
type ContainerProcesses struct {
	// Titles are the column headers Docker returned, in order.
	//
	// Required: true
	Titles []string `json:"titles"`

	// Processes are the rows Docker returned; each row is positional to Titles.
	//
	// Required: true
	Processes [][]string `json:"processes"`
}

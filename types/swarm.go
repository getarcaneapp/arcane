package types

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/moby/moby/api/types/swarm"
)

type SwarmRuntimeStatus struct {
	Enabled   bool   `json:"enabled"`
	ClusterID string `json:"clusterId,omitempty" doc:"Swarm cluster ID when the environment is a swarm manager"`
}

type SwarmInfo struct {
	// ID is the swarm ID.
	//
	// Required: true
	ID string `json:"id"`

	// CreatedAt is when the swarm was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the swarm was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// Spec is the swarm specification.
	//
	// Required: true
	Spec swarm.Spec `json:"spec"`

	// RootRotationInProgress indicates if a root rotation is in progress.
	//
	// Required: true
	RootRotationInProgress bool `json:"rootRotationInProgress"`
}

// NewSwarmInfo converts a Docker swarm inspection result into the API-facing SwarmInfo shape.
//
// It copies the cluster identifiers, timestamps, spec, and root-rotation state
// from the Docker SDK type without mutating the source value.
//
// s is the Docker swarm value returned by the Docker client.
//
// Returns the serialized swarm metadata used by the Arcane API.
func NewSwarmInfo(s swarm.Swarm) SwarmInfo {
	return SwarmInfo{
		ID:                     s.ID,
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
		Spec:                   s.Spec,
		RootRotationInProgress: s.RootRotationInProgress,
	}
}

type SwarmConfig struct {
	EnvironmentTag

	ID        string           `json:"id" readOnly:"true"`
	Version   swarm.Version    `json:"version" readOnly:"true"`
	CreatedAt time.Time        `json:"createdAt" readOnly:"true"`
	UpdatedAt time.Time        `json:"updatedAt" readOnly:"true"`
	Spec      swarm.ConfigSpec `json:"spec" required:"false"`
}

type SwarmSecret struct {
	EnvironmentTag

	ID        string           `json:"id" readOnly:"true"`
	Version   swarm.Version    `json:"version" readOnly:"true"`
	CreatedAt time.Time        `json:"createdAt" readOnly:"true"`
	UpdatedAt time.Time        `json:"updatedAt" readOnly:"true"`
	Spec      swarm.SecretSpec `json:"spec" required:"false"`
}

// NewSwarmConfig converts a Docker swarm config into the API-facing ConfigSummary shape.
//
// It copies the config identity, version, timestamps, and spec from the Docker
// SDK type without modifying the source value.
//
// cfg is the Docker swarm config to summarize.
//
// Returns the serialized config summary used by the API.
func NewSwarmConfig(cfg swarm.Config) SwarmConfig {
	return SwarmConfig{
		ID:        cfg.ID,
		Version:   cfg.Version,
		CreatedAt: cfg.CreatedAt,
		UpdatedAt: cfg.UpdatedAt,
		Spec:      cfg.Spec,
	}
}

// NewSwarmSecret converts a Docker swarm secret into the API-facing SecretSummary shape.
//
// It copies the secret identity, version, timestamps, and spec from the Docker
// SDK type without modifying the source value.
//
// secret is the Docker swarm secret to summarize.
//
// Returns the serialized secret summary used by the API.
func NewSwarmSecret(secret swarm.Secret) SwarmSecret {
	return SwarmSecret{
		ID:        secret.ID,
		Version:   secret.Version,
		CreatedAt: secret.CreatedAt,
		UpdatedAt: secret.UpdatedAt,
		Spec:      secret.Spec,
	}
}

type SwarmConfigUpdateRequest struct {
	Version uint64            `json:"version" doc:"Current config version, used to reject stale updates"`
	Labels  map[string]string `json:"labels" doc:"Replacement labels; config data is immutable"`
}

type SwarmSecretUpdateRequest struct {
	Version uint64            `json:"version" doc:"Current secret version, used to reject stale updates"`
	Labels  map[string]string `json:"labels" doc:"Replacement labels; secret data is immutable"`
}

type SwarmInitRequest struct {
	ListenAddr       string                 `json:"listenAddr,omitempty"`
	AdvertiseAddr    string                 `json:"advertiseAddr,omitempty"`
	DataPathAddr     string                 `json:"dataPathAddr,omitempty"`
	Spec             json.RawMessage        `json:"spec" doc:"Swarm specification"`
	Availability     swarm.NodeAvailability `json:"availability,omitempty"`
	DefaultAddrPool  []string               `json:"defaultAddrPool,omitempty"`
	DataPathPort     uint32                 `json:"dataPathPort,omitempty"`
	SubnetSize       uint32                 `json:"subnetSize,omitempty"`
	ForceNewCluster  bool                   `json:"forceNewCluster,omitempty"`
	AutoLockManagers bool                   `json:"autoLockManagers,omitempty"`
}

type SwarmInitResponse struct {
	NodeID string `json:"nodeId"`
}

type SwarmJoinRequest struct {
	ListenAddr    string                 `json:"listenAddr,omitempty"`
	AdvertiseAddr string                 `json:"advertiseAddr,omitempty"`
	DataPathAddr  string                 `json:"dataPathAddr,omitempty"`
	RemoteAddrs   []string               `json:"remoteAddrs"`
	JoinToken     string                 `json:"joinToken"`
	Availability  swarm.NodeAvailability `json:"availability,omitempty"`
}

type SwarmLeaveRequest struct {
	Force bool `json:"force,omitempty"`
}

type SwarmUnlockRequest struct {
	Key string `json:"key"`
}

type SwarmUnlockKeyResponse struct {
	UnlockKey string `json:"unlockKey"`
}

type SwarmJoinTokensResponse struct {
	Worker  string `json:"worker"`
	Manager string `json:"manager"`
}

type SwarmRotateJoinTokensRequest struct {
	RotateWorkerToken  bool `json:"rotateWorkerToken,omitempty"`
	RotateManagerToken bool `json:"rotateManagerToken,omitempty"`
}

type SwarmUpdateRequest struct {
	Version                uint64          `json:"version,omitempty"`
	Spec                   json.RawMessage `json:"spec" doc:"Updated swarm specification"`
	RotateWorkerToken      bool            `json:"rotateWorkerToken,omitempty"`
	RotateManagerToken     bool            `json:"rotateManagerToken,omitempty"`
	RotateManagerUnlockKey bool            `json:"rotateManagerUnlockKey,omitempty"`
}

type NodeAgentReconcileRequest struct{}

type NodeAgentReconcileResult struct {
	NodeID        string               `json:"nodeId"`
	State         NodeAgentState       `json:"state"`
	EnvironmentID *string              `json:"environmentId,omitempty"`
	Candidates    []NodeAgentCandidate `json:"candidates,omitempty"`
}

type NodeAgentReconcileResponse struct {
	Results []NodeAgentReconcileResult `json:"results"`
}

type NodeAgentBindingRequest struct {
	EnvironmentID     string `json:"environmentId"`
	Rebind            bool   `json:"rebind,omitempty"`
	ReplaceDeployment bool   `json:"replaceDeployment,omitempty"`
}

type SwarmJoinEnvironmentRole string

const (
	SwarmJoinEnvironmentRoleWorker  SwarmJoinEnvironmentRole = "worker"
	SwarmJoinEnvironmentRoleManager SwarmJoinEnvironmentRole = "manager"

	SwarmJoinEnvironmentResultJoined           SwarmJoinEnvironmentResultState = "joined"
	SwarmJoinEnvironmentResultAlreadyMember    SwarmJoinEnvironmentResultState = "already_member"
	SwarmJoinEnvironmentResultJoinedUnverified SwarmJoinEnvironmentResultState = "joined_unverified"
	SwarmJoinEnvironmentResultFailed           SwarmJoinEnvironmentResultState = "failed"
)

type SwarmJoinEnvironmentResultState string

type SwarmJoinCandidate struct {
	EnvironmentID   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	EnvironmentType string `json:"environmentType"`
	Status          string `json:"status"`
}

type SwarmJoinEnvironmentTarget struct {
	EnvironmentID string                   `json:"environmentId"`
	Role          SwarmJoinEnvironmentRole `json:"role"`
	Availability  swarm.NodeAvailability   `json:"availability,omitempty"`
	ListenAddr    string                   `json:"listenAddr,omitempty"`
	AdvertiseAddr string                   `json:"advertiseAddr,omitempty"`
	DataPathAddr  string                   `json:"dataPathAddr,omitempty"`
}

type SwarmJoinEnvironmentsRequest struct {
	RemoteAddrs []string                     `json:"remoteAddrs"`
	Targets     []SwarmJoinEnvironmentTarget `json:"targets"`
}

type SwarmJoinEnvironmentResult struct {
	EnvironmentID string                          `json:"environmentId"`
	State         SwarmJoinEnvironmentResultState `json:"state"`
	NodeID        *string                         `json:"nodeId,omitempty"`
	Error         *string                         `json:"error,omitempty"`
}

type SwarmJoinEnvironmentsResponse struct {
	Results []SwarmJoinEnvironmentResult `json:"results"`
}

type NodeAgentState string

type NodeAgentBindingKind string

const (
	NodeAgentStateNone       NodeAgentState = "none"
	NodeAgentStatePending    NodeAgentState = "pending"
	NodeAgentStateOffline    NodeAgentState = "offline"
	NodeAgentStateConnected  NodeAgentState = "connected"
	NodeAgentStateMismatched NodeAgentState = "mismatched"
	NodeAgentStateAmbiguous  NodeAgentState = "ambiguous"

	NodeAgentBindingKindLocal       NodeAgentBindingKind = "local"
	NodeAgentBindingKindEnvironment NodeAgentBindingKind = "environment"
	NodeAgentBindingKindDedicated   NodeAgentBindingKind = "dedicated"
)

type NodeAgentCandidate struct {
	EnvironmentID   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	EnvironmentType string `json:"environmentType"`
}

type NodeAgentStatus struct {
	State            NodeAgentState        `json:"state"`
	BindingKind      *NodeAgentBindingKind `json:"bindingKind,omitempty"`
	EnvironmentID    *string               `json:"environmentId,omitempty"`
	EnvironmentName  *string               `json:"environmentName,omitempty"`
	EnvironmentType  *string               `json:"environmentType,omitempty"`
	Connected        *bool                 `json:"connected,omitempty"`
	LastHeartbeat    *time.Time            `json:"lastHeartbeat,omitempty"`
	LastPollAt       *time.Time            `json:"lastPollAt,omitempty"`
	ReportedNodeID   *string               `json:"reportedNodeId,omitempty"`
	ReportedHostname *string               `json:"reportedHostname,omitempty"`
	Candidates       []NodeAgentCandidate  `json:"candidates,omitempty"`
}

type SwarmNode struct {
	EnvironmentTag

	// ID is the unique identifier of the node.
	//
	// Required: true
	ID string `json:"id" readOnly:"true"`

	Name string `json:"name,omitempty"`

	Version uint64 `json:"version,omitempty"`

	// Hostname is the node hostname.
	//
	// Required: true
	Hostname string `json:"hostname" readOnly:"true"`

	// Role indicates whether the node is a manager or worker.
	//
	// Required: false
	Role string `json:"role" required:"false"`

	// Availability indicates if the node is active, paused, or drained.
	//
	// Required: false
	Availability string `json:"availability" required:"false"`

	// Status is the node readiness state.
	//
	// Required: true
	Status string `json:"status" readOnly:"true"`

	// Address is the node address.
	//
	// Required: false
	Address string `json:"address,omitempty" readOnly:"true"`

	// ManagerStatus is the manager status string if applicable.
	//
	// Required: false
	ManagerStatus string `json:"managerStatus,omitempty" readOnly:"true"`

	// ManagerAddress is the manager's reachable swarm control-plane address.
	//
	// Required: false
	ManagerAddress string `json:"managerAddress,omitempty" readOnly:"true"`

	// Reachability is the manager reachability if applicable.
	//
	// Required: false
	Reachability string `json:"reachability,omitempty" readOnly:"true"`

	// Labels contains user-defined node labels from the node spec.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty"`

	// SystemLabels contains read-only engine labels.
	//
	// Required: false
	SystemLabels map[string]string `json:"systemLabels,omitempty" readOnly:"true"`

	// EngineVersion is the Docker engine version.
	//
	// Required: false
	EngineVersion string `json:"engineVersion,omitempty" readOnly:"true"`

	// Platform is the node platform string.
	//
	// Required: false
	Platform string `json:"platform,omitempty" readOnly:"true"`

	// CreatedAt is when the node was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt" readOnly:"true"`

	// UpdatedAt is when the node was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt" readOnly:"true"`

	// Agent describes Arcane node-agent coverage for this swarm node.
	//
	// Required: true
	Agent NodeAgentStatus `json:"agent" readOnly:"true"`
}

// NewSwarmNode converts a Docker swarm node into the API-facing NodeSummary shape.
//
// It derives manager role labels, reachability, platform strings, and default
// node-agent state from the Docker SDK value so callers can return a stable,
// JSON-friendly representation.
//
// node is the Docker swarm node to summarize.
//
// Returns a NodeSummary populated from node.
func NewSwarmNode(node swarm.Node) SwarmNode {
	managerStatus := ""
	managerAddress := ""
	reachability := ""
	if node.ManagerStatus != nil {
		if node.ManagerStatus.Leader {
			managerStatus = "leader"
		} else {
			managerStatus = "manager"
		}
		reachability = string(node.ManagerStatus.Reachability)
		managerAddress = node.ManagerStatus.Addr
	}

	platform := ""
	if node.Description.Platform.OS != "" || node.Description.Platform.Architecture != "" {
		platform = fmt.Sprintf("%s/%s", node.Description.Platform.OS, node.Description.Platform.Architecture)
	}

	return SwarmNode{
		ID:             node.ID,
		Name:           node.Spec.Name,
		Version:        node.Version.Index,
		Hostname:       node.Description.Hostname,
		Role:           string(node.Spec.Role),
		Availability:   string(node.Spec.Availability),
		Status:         string(node.Status.State),
		Address:        node.Status.Addr,
		ManagerStatus:  managerStatus,
		ManagerAddress: managerAddress,
		Reachability:   reachability,
		Labels:         node.Spec.Labels,
		SystemLabels:   node.Description.Engine.Labels,
		EngineVersion:  node.Description.Engine.EngineVersion,
		Platform:       platform,
		CreatedAt:      node.CreatedAt,
		UpdatedAt:      node.UpdatedAt,
		Agent: NodeAgentStatus{
			State: NodeAgentStateNone,
		},
	}
}

const SwarmStackNamespaceLabel = "com.docker.stack.namespace"

type SwarmServicePort struct {
	// Protocol is the transport protocol used by the port.
	//
	// Required: true
	Protocol string `json:"protocol"`

	// TargetPort is the port inside the container.
	//
	// Required: true
	TargetPort uint32 `json:"targetPort"`

	// PublishedPort is the port exposed on the host.
	//
	// Required: false
	PublishedPort uint32 `json:"publishedPort,omitempty"`

	// PublishMode is the publish mode used for the port.
	//
	// Required: false
	PublishMode string `json:"publishMode,omitempty"`
}

type SwarmServiceMount struct {
	// Type of the mount (bind, volume, tmpfs, npipe, cluster).
	Type string `json:"type"`

	// Source is the host path or volume name.
	Source string `json:"source,omitempty"`

	// Target is the container-internal path.
	Target string `json:"target"`

	// ReadOnly indicates if the mount is read-only.
	ReadOnly bool `json:"readOnly,omitempty"`

	// VolumeDriver is the volume driver name (only for volume mounts).
	VolumeDriver string `json:"volumeDriver,omitempty"`

	// VolumeOptions contains driver-specific options (only for volume mounts).
	VolumeOptions map[string]string `json:"volumeOptions,omitempty"`

	// DevicePath is the host device path for bind-backed volumes (driver_opts type=none, o=bind).
	DevicePath string `json:"devicePath,omitempty"`
}

type SwarmServiceSummary struct {
	EnvironmentTag

	// ID is the unique identifier of the service.
	//
	// Required: true
	ID string `json:"id"`

	// Name is the service name.
	//
	// Required: true
	Name string `json:"name"`

	// Image is the container image used by the service.
	//
	// Required: true
	Image string `json:"image"`

	// Mode is the service mode (replicated, global, replicated-job, or global-job).
	//
	// Required: true
	Mode string `json:"mode"`

	// Replicas is the desired replica count.
	// For replicated services this comes from the spec.
	// For global services this is the number of eligible nodes (from ServiceStatus.DesiredTasks).
	//
	// Required: true
	Replicas uint64 `json:"replicas"`

	// RunningReplicas is the number of tasks currently in the Running state.
	//
	// Required: true
	RunningReplicas uint64 `json:"runningReplicas"`

	// Ports is the list of published ports for the service.
	//
	// Required: true
	Ports []SwarmServicePort `json:"ports"`

	// CreatedAt is the time when the service was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the time when the service was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// Labels contains user-defined metadata for the service.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// StackName is the stack namespace if the service belongs to a stack.
	//
	// Required: false
	StackName string `json:"stackName,omitempty"`

	// Nodes is the list of node hostnames running tasks for this service.
	//
	// Required: true
	Nodes []string `json:"nodes"`

	// Networks is the list of network names attached to this service.
	//
	// Required: true
	Networks []string `json:"networks"`

	// Mounts is the list of volume/bind mounts configured on this service.
	//
	// Required: true
	Mounts []SwarmServiceMount `json:"mounts"`
}

type SwarmServiceInspect struct {
	// ID is the unique identifier of the service.
	//
	// Required: true
	ID string `json:"id"`

	// Version is the service version metadata.
	//
	// Required: true
	Version swarm.Version `json:"version"`

	// CreatedAt is the time when the service was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the time when the service was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	// Spec is the full service specification.
	//
	// Required: true
	Spec swarm.ServiceSpec `json:"spec"`

	// Endpoint is the service endpoint configuration.
	//
	// Required: true
	Endpoint swarm.Endpoint `json:"endpoint"`

	// UpdateStatus is the current update status, if any.
	UpdateStatus *swarm.UpdateStatus `json:"updateStatus,omitempty"`

	// Nodes is a list of node hostnames running this service.
	Nodes []string `json:"nodes,omitempty"`

	// NetworkDetails contains enriched network information keyed by network ID.
	NetworkDetails map[string]SwarmServiceNetworkDetail `json:"networkDetails,omitempty"`

	// Mounts contains enriched mount information with volume driver details.
	Mounts []SwarmServiceMount `json:"mounts,omitempty"`
}

// SwarmServiceNetworkIPAMConfig represents a single IPAM configuration entry with camelCase JSON tags.
type SwarmServiceNetworkIPAMConfig struct {
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	IPRange string `json:"ipRange,omitempty"`
}

// SwarmServiceNetworkConfigDetail holds details about a config-only network referenced by configFrom.
type SwarmServiceNetworkConfigDetail struct {
	Name        string                          `json:"name"`
	Driver      string                          `json:"driver"`
	Scope       string                          `json:"scope"`
	EnableIPv4  bool                            `json:"enableIPv4"`
	EnableIPv6  bool                            `json:"enableIPv6"`
	Options     map[string]string               `json:"options,omitempty"`
	IPv4Configs []SwarmServiceNetworkIPAMConfig `json:"ipv4Configs,omitempty"`
	IPv6Configs []SwarmServiceNetworkIPAMConfig `json:"ipv6Configs,omitempty"`
}

// SwarmServiceNetworkDetail holds enriched network information for a service's attached network.
type SwarmServiceNetworkDetail struct {
	Options       map[string]string                `json:"options,omitempty"`
	ConfigNetwork *SwarmServiceNetworkConfigDetail `json:"configNetwork,omitempty"`
	ID            string                           `json:"id"`
	Name          string                           `json:"name"`
	Driver        string                           `json:"driver"`
	Scope         string                           `json:"scope"`
	ConfigFrom    string                           `json:"configFrom,omitempty"`
	IPAMConfigs   []SwarmServiceNetworkIPAMConfig  `json:"ipamConfigs,omitempty"`
	Internal      bool                             `json:"internal"`
	Attachable    bool                             `json:"attachable"`
	Ingress       bool                             `json:"ingress"`
	EnableIPv4    bool                             `json:"enableIPv4"`
	EnableIPv6    bool                             `json:"enableIPv6"`
	ConfigOnly    bool                             `json:"configOnly"`
}

type SwarmServiceCreateRequest struct {
	// Spec is the service specification as a JSON object.
	//
	// Required: true
	Spec json.RawMessage `json:"spec" doc:"Service specification"`

	// Options are additional create options for the service.
	//
	// Required: false
	Options *SwarmServiceCreateOptions `json:"options,omitempty" doc:"Additional create options"`
}

type SwarmServiceUpdateRequest struct {
	// Version is the service version index to update.
	//
	// Required: true
	Version uint64 `json:"version"`

	// Spec is the updated service specification.
	//
	// Required: true
	Spec swarm.ServiceSpec `json:"spec"`

	// Options are additional update options for the service.
	//
	// Required: false
	Options *SwarmServiceUpdateOptions `json:"options,omitzero"`
}

type SwarmServiceCreateResponse struct {
	// ID is the created service ID.
	//
	// Required: true
	ID string `json:"id"`

	// Warnings are any warnings returned by the Docker API.
	//
	// Required: false
	Warnings []string `json:"warnings,omitempty"`
}

type SwarmServiceUpdateResponse struct {
	// Warnings are any warnings returned by the Docker API.
	//
	// Required: false
	Warnings []string `json:"warnings,omitempty"`
}

type SwarmServiceCreateOptions struct {
	// EncodedRegistryAuth is the encoded registry authorization credentials.
	//
	// Required: false
	EncodedRegistryAuth string `json:"encodedRegistryAuth,omitempty"`

	// QueryRegistry indicates if registry metadata should be queried.
	//
	// Required: false
	QueryRegistry bool `json:"queryRegistry,omitempty"`
}

type SwarmServiceUpdateOptions struct {
	// EncodedRegistryAuth is the encoded registry authorization credentials.
	//
	// Required: false
	EncodedRegistryAuth string `json:"encodedRegistryAuth,omitempty"`

	// RegistryAuthFrom specifies where to find registry auth credentials.
	//
	// Required: false
	RegistryAuthFrom swarm.RegistryAuthSource `json:"registryAuthFrom,omitempty"`

	// Rollback requests a server-side rollback ("previous" or "none").
	//
	// Required: false
	Rollback string `json:"rollback,omitempty"`

	// QueryRegistry indicates if registry metadata should be queried.
	//
	// Required: false
	QueryRegistry bool `json:"queryRegistry,omitempty"`
}

type SwarmServiceScaleRequest struct {
	// Replicas is the desired replica count.
	//
	// Required: true
	Replicas uint64 `json:"replicas"`
}

// NewSwarmServiceSummary converts a Docker swarm service into the API-facing ServiceSummary shape.
//
// It derives the service mode, replica counts, running task counts, published
// ports, stack namespace label, attached network names, and container mounts.
// Network IDs are resolved through networkNameByID when possible, then fall
// back to the attachment alias or raw target value. A nil nodeNames slice is
// normalized to an empty slice for stable JSON output.
//
// service is the Docker swarm service to summarize.
// nodeNames lists the node hostnames currently running tasks for the service.
// networkNameByID maps attached network IDs to human-readable names.
//
// Returns a ServiceSummary populated from service and the supplied enrichment data.
func NewSwarmServiceSummary(service swarm.Service, nodeNames []string, networkNameByID map[string]string) SwarmServiceSummary {
	spec := service.Spec

	mode := "unknown"
	replicas := uint64(0)
	runningReplicas := uint64(0)
	switch {
	case spec.Mode.Replicated != nil:
		mode = "replicated"
		if spec.Mode.Replicated.Replicas != nil {
			replicas = *spec.Mode.Replicated.Replicas
		}
	case spec.Mode.Global != nil:
		mode = "global"
		if service.ServiceStatus != nil {
			replicas = service.ServiceStatus.DesiredTasks
		}
	case spec.Mode.ReplicatedJob != nil:
		mode = "replicated-job"
		switch {
		case spec.Mode.ReplicatedJob.TotalCompletions != nil:
			replicas = *spec.Mode.ReplicatedJob.TotalCompletions
		case spec.Mode.ReplicatedJob.MaxConcurrent != nil:
			replicas = *spec.Mode.ReplicatedJob.MaxConcurrent
		default:
			replicas = 1
		}
	case spec.Mode.GlobalJob != nil:
		mode = "global-job"
		if service.ServiceStatus != nil {
			replicas = service.ServiceStatus.DesiredTasks
		}
	}

	if service.ServiceStatus != nil {
		runningReplicas = service.ServiceStatus.RunningTasks
	}

	image := ""
	if spec.TaskTemplate.ContainerSpec != nil {
		image = spec.TaskTemplate.ContainerSpec.Image
	}

	portSpecs := service.Endpoint.Spec.Ports
	if len(portSpecs) == 0 {
		portSpecs = service.Endpoint.Ports
	}
	ports := make([]SwarmServicePort, 0, len(portSpecs))
	for _, port := range portSpecs {
		ports = append(ports, SwarmServicePort{
			Protocol:      string(port.Protocol),
			TargetPort:    port.TargetPort,
			PublishedPort: port.PublishedPort,
			PublishMode:   string(port.PublishMode),
		})
	}

	stackName := ""
	if spec.Labels != nil {
		stackName = spec.Labels[SwarmStackNamespaceLabel]
	}

	// Extract networks from task template, resolving IDs to names
	networkConfigs := spec.TaskTemplate.Networks
	networks := make([]string, 0, len(networkConfigs))
	for _, n := range networkConfigs {
		if name, ok := networkNameByID[n.Target]; ok {
			networks = append(networks, name)
		} else if len(n.Aliases) > 0 {
			networks = append(networks, n.Aliases[0])
		} else {
			networks = append(networks, n.Target)
		}
	}

	// Extract mounts from container spec
	mounts := make([]SwarmServiceMount, 0)
	if spec.TaskTemplate.ContainerSpec != nil {
		for _, m := range spec.TaskTemplate.ContainerSpec.Mounts {
			mounts = append(mounts, SwarmServiceMount{
				Type:     string(m.Type),
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			})
		}
	}

	if nodeNames == nil {
		nodeNames = []string{}
	}

	return SwarmServiceSummary{
		ID:              service.ID,
		Name:            spec.Name,
		Image:           image,
		Mode:            mode,
		Replicas:        replicas,
		RunningReplicas: runningReplicas,
		Ports:           ports,
		CreatedAt:       service.CreatedAt,
		UpdatedAt:       service.UpdatedAt,
		Labels:          spec.Labels,
		StackName:       stackName,
		Nodes:           nodeNames,
		Networks:        networks,
		Mounts:          mounts,
	}
}

// NewSwarmServiceInspect converts a Docker swarm service into the API-facing ServiceInspect shape.
//
// It copies the core inspection fields directly from the Docker SDK type.
// Callers can enrich the returned value with node, network, or mount details
// after construction when needed.
//
// service is the Docker swarm service to convert.
//
// Returns the base inspection payload for service.
func NewSwarmServiceInspect(service swarm.Service) SwarmServiceInspect {
	return SwarmServiceInspect{
		ID:           service.ID,
		Version:      service.Version,
		CreatedAt:    service.CreatedAt,
		UpdatedAt:    service.UpdatedAt,
		Spec:         service.Spec,
		Endpoint:     service.Endpoint,
		UpdateStatus: service.UpdateStatus,
	}
}

type SwarmStackSummary struct {
	EnvironmentTag

	// ID is the unique identifier for the stack (uses namespace).
	//
	// Required: true
	ID string `json:"id"`

	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// Namespace is the stack namespace label value.
	//
	// Required: true
	Namespace string `json:"namespace"`

	// Services is the number of services in the stack.
	//
	// Required: true
	Services int `json:"services"`

	// CreatedAt is the earliest service creation time in the stack.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the latest service update time in the stack.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`

	ClusterID string `json:"clusterId,omitempty" doc:"Swarm cluster ID of the manager that listed the stack"`
	HasSource bool   `json:"hasSource" doc:"Whether this environment stores the stack's saved source files"`
}

type SwarmStackInspect struct {
	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// Namespace is the stack namespace label value.
	//
	// Required: true
	Namespace string `json:"namespace"`

	// Services is the number of services in the stack.
	//
	// Required: true
	Services int `json:"services"`

	// CreatedAt is the earliest service creation time in the stack.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is the latest service update time in the stack.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

type SwarmStackRenderConfigRequest struct {
	// Name is the stack name (namespace).
	//
	// Required: true
	Name string `json:"name"`

	// ComposeContent is the Docker Compose YAML content.
	//
	// Required: true
	ComposeContent string `json:"composeContent"`

	// OverrideContent is the optional Docker Compose override YAML content merged
	// on top of ComposeContent, mirroring `docker compose` override files.
	//
	// Required: false
	OverrideContent string `json:"overrideContent,omitempty"`

	// EnvContent is the optional environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`
}

type SwarmStackRenderConfigResponse struct {
	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name"`

	// RenderedCompose is the normalized compose config output.
	//
	// Required: true
	RenderedCompose string `json:"renderedCompose"`

	// Services contains the final stack-scoped service names.
	//
	// Required: true
	Services []string `json:"services"`

	// Networks contains the final resolved network names, including stack
	// namespacing for non-external resources.
	//
	// Required: true
	Networks []string `json:"networks"`

	// Volumes contains the final resolved volume names, including stack
	// namespacing for non-external resources.
	//
	// Required: true
	Volumes []string `json:"volumes"`

	// Configs contains the final content-addressed or external config names.
	//
	// Required: true
	Configs []string `json:"configs"`

	// Secrets contains the final content-addressed or external secret names.
	//
	// Required: true
	Secrets []string `json:"secrets"`

	// Warnings contains non-fatal warnings.
	//
	// Required: false
	Warnings []string `json:"warnings,omitempty"`
}

type SwarmStackSource struct {
	// Name is the stack name.
	//
	// Required: true
	Name string `json:"name" readOnly:"true"`

	// ComposeContent is the original Docker Compose YAML content used for deployment.
	//
	// Required: false
	ComposeContent string `json:"composeContent" required:"false"`

	// OverrideContent is the optional Docker Compose override content merged on top
	// of ComposeContent at deploy, mirroring `docker compose` override files.
	//
	// Required: false
	OverrideContent string `json:"overrideContent,omitempty"`

	// EnvContent is the optional original environment file content used for deployment.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// Files is an optional list of additional files synced with the stack.
	//
	// Required: false
	Files []SwarmSyncFile `json:"files,omitempty"`
}

// SwarmStackDeployRequest is used to deploy a Swarm stack from a Compose file.
type SwarmStackDeployRequest struct {
	// Name is the stack name (namespace).
	//
	// Required: true
	Name string `json:"name"`

	// ComposeContent is the Docker Compose YAML content.
	//
	// Required: true
	ComposeContent string `json:"composeContent"`

	// OverrideContent is the optional Docker Compose override YAML content merged
	// on top of ComposeContent, mirroring `docker compose` override files.
	//
	// Required: false
	OverrideContent string `json:"overrideContent,omitempty"`

	// EnvContent is the optional environment file content.
	//
	// Required: false
	EnvContent string `json:"envContent,omitempty"`

	// Files is an optional list of additional files to sync (e.g. env_file, configs, secrets).
	//
	// Required: false
	Files []SwarmSyncFile `json:"files,omitempty"`

	// WithRegistryAuth sends registry auth details to Swarm agents.
	//
	// Required: false
	WithRegistryAuth bool `json:"withRegistryAuth,omitempty"`

	// Prune removes services that are no longer referenced in the stack.
	//
	// Required: false
	Prune bool `json:"prune,omitempty"`

	// ResolveImage controls how image digests are resolved (always, changed, never).
	//
	// Required: false
	ResolveImage string `json:"resolveImage,omitempty"`

	// WorkingDir is the trusted internal working directory used by GitOps.
	// It is never decoded from public API requests.
	//
	// Required: false
	WorkingDir string `json:"-"`
}

// SwarmSyncFile represents a file to be synced to the target environment.
type SwarmSyncFile struct {
	// RelativePath is the path of the file relative to the stack's working directory.
	//
	// Required: true
	RelativePath string `json:"relativePath"`

	// Content is the file content.
	//
	// Required: true
	Content []byte `json:"content"`
}

// SwarmStackDeployResponse represents the result of a stack deployment.
type SwarmStackDeployResponse struct {
	// Name is the deployed stack name.
	//
	// Required: true
	Name string `json:"name"`
}

type SwarmTaskSummary struct {
	EnvironmentTag

	// ID is the unique identifier of the task.
	//
	// Required: true
	ID string `json:"id"`

	// Name is the task name.
	//
	// Required: true
	Name string `json:"name"`

	// ServiceID is the service ID the task belongs to.
	//
	// Required: true
	ServiceID string `json:"serviceId"`

	// ServiceName is the service name the task belongs to.
	//
	// Required: true
	ServiceName string `json:"serviceName"`

	// NodeID is the node ID running the task.
	//
	// Required: true
	NodeID string `json:"nodeId"`

	// NodeName is the node name running the task.
	//
	// Required: true
	NodeName string `json:"nodeName"`

	// DesiredState is the desired state for the task.
	//
	// Required: true
	DesiredState string `json:"desiredState"`

	// CurrentState is the current runtime state for the task.
	//
	// Required: true
	CurrentState string `json:"currentState"`

	// Error is any error message reported by the task.
	//
	// Required: false
	Error string `json:"error,omitempty"`

	// ContainerID is the container ID backing the task.
	//
	// Required: false
	ContainerID string `json:"containerId,omitempty"`

	// Image is the container image used by the task.
	//
	// Required: false
	Image string `json:"image,omitempty"`

	// Slot is the task slot for replicated services.
	//
	// Required: false
	Slot int `json:"slot,omitempty"`

	// CreatedAt is when the task was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the task was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

// NewSwarmTaskSummary converts a Docker swarm task into the API-facing TaskSummary shape.
//
// It extracts task identity, service and node names supplied by the caller,
// container image and container ID details when available, and falls back from
// the Docker task error field to the task status message when necessary.
//
// task is the Docker swarm task to summarize.
// serviceName is the human-readable service name associated with task.
// nodeName is the human-readable node name associated with task.
//
// Returns a TaskSummary populated from task and the supplied display names.
func NewSwarmTaskSummary(task swarm.Task, serviceName, nodeName string) SwarmTaskSummary {
	name := ""
	if task.Name != "" {
		name = task.Name
	}

	image := ""
	if task.Spec.ContainerSpec != nil {
		image = task.Spec.ContainerSpec.Image
	}

	containerID := ""
	if task.Status.ContainerStatus != nil {
		containerID = task.Status.ContainerStatus.ContainerID
	}

	errorMessage := task.Status.Err
	if errorMessage == "" {
		errorMessage = task.Status.Message
	}

	slot := task.Slot

	return SwarmTaskSummary{
		ID:           task.ID,
		Name:         name,
		ServiceID:    task.ServiceID,
		ServiceName:  serviceName,
		NodeID:       task.NodeID,
		NodeName:     nodeName,
		DesiredState: string(task.DesiredState),
		CurrentState: string(task.Status.State),
		Error:        errorMessage,
		ContainerID:  containerID,
		Image:        image,
		Slot:         slot,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}
}

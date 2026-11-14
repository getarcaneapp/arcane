package types

import (
	"encoding/json/jsontext"
	"fmt"
	"time"

	"github.com/moby/moby/api/types/swarm"
)

type (
	SwarmRuntimeStatus struct {
		ClusterID string `json:"clusterId,omitempty" doc:"Swarm cluster ID when the environment is a swarm manager"`
		Enabled   bool   `json:"enabled"`
	}

	SwarmInfo struct {
		ID                     string     `json:"id"`
		CreatedAt              time.Time  `json:"createdAt"`
		UpdatedAt              time.Time  `json:"updatedAt"`
		Spec                   swarm.Spec `json:"spec"`
		RootRotationInProgress bool       `json:"rootRotationInProgress"`
	}

	SwarmConfig struct {
		Spec      swarm.ConfigSpec `json:"spec" required:"false"`
		CreatedAt time.Time        `json:"createdAt" readOnly:"true"`
		UpdatedAt time.Time        `json:"updatedAt" readOnly:"true"`
		EnvironmentTag

		ID      string        `json:"id" readOnly:"true"`
		Version swarm.Version `json:"version" readOnly:"true"`
	}

	SwarmSecret struct {
		Spec      swarm.SecretSpec `json:"spec" required:"false"`
		CreatedAt time.Time        `json:"createdAt" readOnly:"true"`
		UpdatedAt time.Time        `json:"updatedAt" readOnly:"true"`
		EnvironmentTag

		ID      string        `json:"id" readOnly:"true"`
		Version swarm.Version `json:"version" readOnly:"true"`
	}

	SwarmConfigUpdateRequest struct {
		Labels  map[string]string `json:"labels" doc:"Replacement labels; config data is immutable"`
		Version uint64            `json:"version" doc:"Current config version, used to reject stale updates"`
	}

	SwarmSecretUpdateRequest struct {
		Labels  map[string]string `json:"labels" doc:"Replacement labels; secret data is immutable"`
		Version uint64            `json:"version" doc:"Current secret version, used to reject stale updates"`
	}

	SwarmInitRequest struct {
		ListenAddr       string                 `json:"listenAddr,omitempty" doc:"Manager listen address, host:port or interface; defaults to 0.0.0.0:2377"`
		AdvertiseAddr    string                 `json:"advertiseAddr,omitempty" doc:"Address advertised to other nodes, host[:port] or interface; empty lets Docker detect it"`
		DataPathAddr     string                 `json:"dataPathAddr,omitempty" doc:"Address or interface for overlay data traffic; defaults to the advertised address"`
		Spec             jsontext.Value         `json:"spec" doc:"Swarm specification"`
		Availability     swarm.NodeAvailability `json:"availability,omitempty" doc:"active, pause or drain"`
		DefaultAddrPool  []string               `json:"defaultAddrPool,omitempty" doc:"CIDR pools for overlay network subnets; empty uses Docker's default"`
		DataPathPort     uint32                 `json:"dataPathPort,omitempty" doc:"UDP port for VXLAN data traffic; 0 uses Docker's default 4789"`
		SubnetSize       uint32                 `json:"subnetSize,omitempty" doc:"Prefix length of subnets from defaultAddrPool; 0 uses Docker's default 24"`
		ForceNewCluster  bool                   `json:"forceNewCluster,omitempty" doc:"Force a new single-manager cluster from this node's state, even if already in a swarm"`
		AutoLockManagers bool                   `json:"autoLockManagers,omitempty" doc:"Restarted managers stay locked until unlocked with the unlock key"`
	}

	SwarmInitResponse struct {
		NodeID string `json:"nodeId"`
	}

	SwarmJoinRequest struct {
		ListenAddr    string                 `json:"listenAddr,omitempty" doc:"Manager listen address, host:port or interface; defaults to 0.0.0.0:2377"`
		AdvertiseAddr string                 `json:"advertiseAddr,omitempty" doc:"Address advertised to other nodes, host[:port] or interface; empty lets Docker detect it"`
		DataPathAddr  string                 `json:"dataPathAddr,omitempty" doc:"Address or interface for overlay data traffic; defaults to the advertised address"`
		JoinToken     string                 `json:"joinToken" doc:"Worker or manager join token of the swarm"`
		Availability  swarm.NodeAvailability `json:"availability,omitempty" doc:"active, pause or drain"`
		RemoteAddrs   []string               `json:"remoteAddrs" doc:"Manager addresses to join through, as host:port"`
	}

	SwarmLeaveRequest struct {
		Force bool `json:"force,omitempty" doc:"Leave even as a manager, which can cost the swarm its quorum"`
	}

	SwarmUnlockRequest struct {
		Key string `json:"key" doc:"Unlock key of the locked swarm"`
	}

	SwarmUnlockKeyResponse struct {
		UnlockKey string `json:"unlockKey"`
	}

	SwarmJoinTokensResponse struct {
		Worker  string `json:"worker"`
		Manager string `json:"manager"`
	}

	SwarmRotateJoinTokensRequest struct {
		RotateWorkerToken  bool `json:"rotateWorkerToken,omitempty" doc:"Rotate the worker token; both rotate when neither flag is set"`
		RotateManagerToken bool `json:"rotateManagerToken,omitempty" doc:"Rotate the manager token; both rotate when neither flag is set"`
	}

	SwarmUpdateRequest struct {
		Spec                   jsontext.Value `json:"spec" doc:"Updated swarm specification"`
		Version                uint64         `json:"version,omitempty" doc:"Swarm version index to update; 0 uses the current one"`
		RotateWorkerToken      bool           `json:"rotateWorkerToken,omitempty"`
		RotateManagerToken     bool           `json:"rotateManagerToken,omitempty"`
		RotateManagerUnlockKey bool           `json:"rotateManagerUnlockKey,omitempty"`
	}

	NodeAgentReconcileRequest struct{}

	NodeAgentReconcileResult struct {
		NodeID        string               `json:"nodeId"`
		State         NodeAgentState       `json:"state"`
		EnvironmentID *string              `json:"environmentId,omitempty"`
		Candidates    []NodeAgentCandidate `json:"candidates,omitempty"`
	}

	NodeAgentReconcileResponse struct {
		Results []NodeAgentReconcileResult `json:"results"`
	}

	NodeAgentBindingRequest struct {
		EnvironmentID     string `json:"environmentId" doc:"Visible environment to attach; it must report this swarm node"`
		Rebind            bool   `json:"rebind,omitempty" doc:"Move an existing binding of the node or environment instead of failing"`
		ReplaceDeployment bool   `json:"replaceDeployment,omitempty" doc:"Also delete the dedicated agent environment Arcane deployed for this node"`
	}

	SwarmJoinEnvironmentRole string

	SwarmJoinEnvironmentResultState string

	SwarmJoinCandidate struct {
		EnvironmentID   string `json:"environmentId"`
		EnvironmentName string `json:"environmentName"`
		EnvironmentType string `json:"environmentType"`
		Status          string `json:"status"`
	}

	SwarmJoinEnvironmentTarget struct {
		EnvironmentID string                   `json:"environmentId" doc:"Environment whose Docker host joins the swarm"`
		Role          SwarmJoinEnvironmentRole `json:"role" doc:"manager or worker"`
		Availability  swarm.NodeAvailability   `json:"availability,omitempty" doc:"active, pause or drain"`
		ListenAddr    string                   `json:"listenAddr,omitempty" doc:"Manager listen address on the target; defaults to 0.0.0.0:2377"`
		AdvertiseAddr string                   `json:"advertiseAddr,omitempty" doc:"Address the target advertises; empty lets Docker detect it"`
		DataPathAddr  string                   `json:"dataPathAddr,omitempty" doc:"Address or interface for overlay data traffic on the target"`
	}

	SwarmJoinEnvironmentsRequest struct {
		RemoteAddrs []string                     `json:"remoteAddrs" doc:"Manager addresses to join through, as host:port; empty uses the swarm's known managers"`
		Targets     []SwarmJoinEnvironmentTarget `json:"targets" doc:"Environments to join; managers first, then workers in parallel"`
	}

	SwarmJoinEnvironmentResult struct {
		NodeID        *string                         `json:"nodeId,omitempty"`
		Error         *string                         `json:"error,omitempty"`
		EnvironmentID string                          `json:"environmentId"`
		State         SwarmJoinEnvironmentResultState `json:"state"`
	}

	SwarmJoinEnvironmentsResponse struct {
		Results []SwarmJoinEnvironmentResult `json:"results"`
	}

	NodeAgentState string

	NodeAgentBindingKind string

	NodeAgentCandidate struct {
		EnvironmentID   string `json:"environmentId"`
		EnvironmentName string `json:"environmentName"`
		EnvironmentType string `json:"environmentType"`
	}

	NodeAgentStatus struct {
		State            NodeAgentState        `json:"state"`
		BindingKind      *NodeAgentBindingKind `json:"bindingKind,omitempty"`
		EnvironmentID    *string               `json:"environmentId,omitempty"`
		EnvironmentName  *string               `json:"environmentName,omitempty"`
		EnvironmentType  *string               `json:"environmentType,omitempty"`
		Connected        *bool                 `json:"connected,omitempty"`
		LastHeartbeat    *time.Time            `json:"lastHeartbeat,omitempty"`
		ReportedNodeID   *string               `json:"reportedNodeId,omitempty"`
		ReportedHostname *string               `json:"reportedHostname,omitempty"`
		Candidates       []NodeAgentCandidate  `json:"candidates,omitempty"`
	}

	SwarmNode struct {
		CreatedAt    time.Time         `json:"createdAt" readOnly:"true"`
		UpdatedAt    time.Time         `json:"updatedAt" readOnly:"true"`
		Labels       map[string]string `json:"labels,omitempty" doc:"User-defined node labels; an update replaces the whole set"`
		SystemLabels map[string]string `json:"systemLabels,omitempty" readOnly:"true"`

		EnvironmentTag
		ID string `json:"id" readOnly:"true"`

		Name           string          `json:"name,omitempty" doc:"Node name from the node spec"`
		Hostname       string          `json:"hostname" readOnly:"true"`
		Role           string          `json:"role" required:"false" doc:"manager or worker"`
		Availability   string          `json:"availability" required:"false" doc:"Scheduling availability: active, pause or drain"`
		Status         string          `json:"status" readOnly:"true"`
		Address        string          `json:"address,omitempty" readOnly:"true"`
		ManagerStatus  string          `json:"managerStatus,omitempty" readOnly:"true"`
		ManagerAddress string          `json:"managerAddress,omitempty" readOnly:"true"`
		Reachability   string          `json:"reachability,omitempty" readOnly:"true"`
		EngineVersion  string          `json:"engineVersion,omitempty" readOnly:"true"`
		Platform       string          `json:"platform,omitempty" readOnly:"true"`
		Agent          NodeAgentStatus `json:"agent" readOnly:"true"`

		Version uint64 `json:"version,omitempty" doc:"Node version index; an update with 0 uses the current one"`
	}

	SwarmServicePort struct {
		Protocol      string `json:"protocol"`
		PublishMode   string `json:"publishMode,omitempty"`
		TargetPort    uint32 `json:"targetPort"`
		PublishedPort uint32 `json:"publishedPort,omitempty"`
	}

	SwarmServiceMount struct {
		VolumeOptions map[string]string `json:"volumeOptions,omitempty"`
		Type          string            `json:"type"`
		Source        string            `json:"source,omitempty"`
		Target        string            `json:"target"`
		VolumeDriver  string            `json:"volumeDriver,omitempty"`
		DevicePath    string            `json:"devicePath,omitempty"`
		ReadOnly      bool              `json:"readOnly,omitempty"`
	}

	SwarmServiceSummary struct {
		CreatedAt time.Time         `json:"createdAt"`
		UpdatedAt time.Time         `json:"updatedAt"`
		Labels    map[string]string `json:"labels"`

		EnvironmentTag
		ID        string              `json:"id"`
		Name      string              `json:"name"`
		Image     string              `json:"image"`
		Mode      string              `json:"mode"`
		StackName string              `json:"stackName,omitempty"`
		Ports     []SwarmServicePort  `json:"ports"`
		Nodes     []string            `json:"nodes"`
		Networks  []string            `json:"networks"`
		Mounts    []SwarmServiceMount `json:"mounts"`

		Replicas        uint64 `json:"replicas"`
		RunningReplicas uint64 `json:"runningReplicas"`
	}

	SwarmServiceInspect struct {
		Spec           swarm.ServiceSpec                    `json:"spec"`
		CreatedAt      time.Time                            `json:"createdAt"`
		UpdatedAt      time.Time                            `json:"updatedAt"`
		UpdateStatus   *swarm.UpdateStatus                  `json:"updateStatus,omitempty"`
		NetworkDetails map[string]SwarmServiceNetworkDetail `json:"networkDetails,omitempty"`
		ID             string                               `json:"id"`
		Endpoint       swarm.Endpoint                       `json:"endpoint"`
		Nodes          []string                             `json:"nodes,omitempty"`
		Mounts         []SwarmServiceMount                  `json:"mounts,omitempty"`
		Version        swarm.Version                        `json:"version"`
	}

	SwarmServiceNetworkIPAMConfig struct {
		Subnet  string `json:"subnet,omitempty"`
		Gateway string `json:"gateway,omitempty"`
		IPRange string `json:"ipRange,omitempty"`
	}

	SwarmServiceNetworkConfigDetail struct {
		Options     map[string]string               `json:"options,omitempty"`
		Name        string                          `json:"name"`
		Driver      string                          `json:"driver"`
		Scope       string                          `json:"scope"`
		IPv4Configs []SwarmServiceNetworkIPAMConfig `json:"ipv4Configs,omitempty"`
		IPv6Configs []SwarmServiceNetworkIPAMConfig `json:"ipv6Configs,omitempty"`
		EnableIPv4  bool                            `json:"enableIPv4"`
		EnableIPv6  bool                            `json:"enableIPv6"`
	}

	SwarmServiceNetworkDetail struct {
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

	SwarmServiceCreateRequest struct {
		Options *SwarmServiceCreateOptions `json:"options,omitempty" doc:"Additional create options"`
		Spec    jsontext.Value             `json:"spec" doc:"Service specification"`
	}

	SwarmServiceUpdateRequest struct {
		Spec    swarm.ServiceSpec          `json:"spec" doc:"Full service spec replacing the current one"`
		Options *SwarmServiceUpdateOptions `json:"options,omitzero" doc:"Registry auth, registry query and rollback options"`
		Version uint64                     `json:"version" doc:"Service version index to update; 0 uses the current one"`
	}

	SwarmServiceCreateResponse struct {
		ID       string   `json:"id"`
		Warnings []string `json:"warnings,omitempty"`
	}

	SwarmServiceUpdateResponse struct {
		Warnings []string `json:"warnings,omitempty"`
	}

	SwarmServiceCreateOptions struct {
		EncodedRegistryAuth string `json:"encodedRegistryAuth,omitempty"`
		QueryRegistry       bool   `json:"queryRegistry,omitempty"`
	}

	SwarmServiceUpdateOptions struct {
		EncodedRegistryAuth string                   `json:"encodedRegistryAuth,omitempty"`
		RegistryAuthFrom    swarm.RegistryAuthSource `json:"registryAuthFrom,omitempty"`
		Rollback            string                   `json:"rollback,omitempty"`
		QueryRegistry       bool                     `json:"queryRegistry,omitempty"`
	}

	SwarmServiceScaleRequest struct {
		Replicas uint64 `json:"replicas" doc:"Replicas, or completions for a replicated job"`
	}

	SwarmStackSummary struct {
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`

		EnvironmentTag
		ID        string `json:"id"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`

		ClusterID string `json:"clusterId,omitempty" doc:"Swarm cluster ID of the manager that listed the stack"`
		Services  int    `json:"services"`

		HasSource bool `json:"hasSource" doc:"Whether this environment stores the stack's saved source files"`
	}

	SwarmStackInspect struct {
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
		Name      string    `json:"name"`
		Namespace string    `json:"namespace"`
		Services  int       `json:"services"`
	}

	SwarmStackRenderConfigRequest struct {
		Name            string `json:"name" doc:"Stack name (namespace)"`
		ComposeContent  string `json:"composeContent"`
		OverrideContent string `json:"overrideContent,omitempty" doc:"Compose override merged over composeContent"`
		EnvContent      string `json:"envContent,omitempty" doc:"Content of the stack's .env file"`
	}

	SwarmStackRenderConfigResponse struct {
		Name            string   `json:"name"`
		RenderedCompose string   `json:"renderedCompose"`
		Services        []string `json:"services"`
		Networks        []string `json:"networks"`
		Volumes         []string `json:"volumes"`
		Configs         []string `json:"configs"`
		Secrets         []string `json:"secrets"`
		Warnings        []string `json:"warnings,omitempty"`
	}

	SwarmStackSource struct {
		Name            string          `json:"name" readOnly:"true"`
		ComposeContent  string          `json:"composeContent" required:"false" doc:"Saved compose file; saving a source redeploys the stack"`
		OverrideContent string          `json:"overrideContent,omitempty" doc:"Compose override merged over composeContent"`
		EnvContent      string          `json:"envContent,omitempty" doc:"Content of the stack's .env file"`
		Files           []SwarmSyncFile `json:"files,omitempty" doc:"Extra stack files, such as env files, configs and secrets"`
	}

	SwarmStackDeployRequest struct {
		Name            string `json:"name" doc:"Stack name (namespace); required"`
		ComposeContent  string `json:"composeContent" doc:"Compose file content; saved as the stack source"`
		OverrideContent string `json:"overrideContent,omitempty" doc:"Compose override merged over composeContent"`
		EnvContent      string `json:"envContent,omitempty" doc:"Content of the stack's .env file"`
		ResolveImage    string `json:"resolveImage,omitempty" doc:"Registry digest lookup: always (default), changed (changed images only) or never"`

		WorkingDir       string          `json:"-"`
		Files            []SwarmSyncFile `json:"files,omitempty" doc:"Extra files the compose file uses, such as env files, configs and secrets"`
		WithRegistryAuth bool            `json:"withRegistryAuth,omitempty" doc:"Send the credentials of Arcane's registries with the service specs"`
		Prune            bool            `json:"prune,omitempty" doc:"Remove stack services the compose file no longer defines"`
	}

	SwarmSyncFile struct {
		RelativePath string `json:"relativePath" doc:"Path relative to the stack directory"`
		Content      []byte `json:"content" doc:"File content, base64-encoded"`
	}

	SwarmStackDeployResponse struct {
		Name string `json:"name"`
	}

	SwarmTaskSummary struct {
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
		EnvironmentTag
		ID           string `json:"id"`
		Name         string `json:"name"`
		ServiceID    string `json:"serviceId"`
		ServiceName  string `json:"serviceName"`
		NodeID       string `json:"nodeId"`
		NodeName     string `json:"nodeName"`
		DesiredState string `json:"desiredState"`
		CurrentState string `json:"currentState"`
		Error        string `json:"error,omitempty"`
		ContainerID  string `json:"containerId,omitempty"`
		Image        string `json:"image,omitempty"`
		Slot         int    `json:"slot,omitempty"`
	}
)

const (
	SwarmJoinEnvironmentRoleWorker             SwarmJoinEnvironmentRole        = "worker"
	SwarmJoinEnvironmentRoleManager            SwarmJoinEnvironmentRole        = "manager"
	SwarmJoinEnvironmentResultJoined           SwarmJoinEnvironmentResultState = "joined"
	SwarmJoinEnvironmentResultAlreadyMember    SwarmJoinEnvironmentResultState = "already_member"
	SwarmJoinEnvironmentResultJoinedUnverified SwarmJoinEnvironmentResultState = "joined_unverified"
	SwarmJoinEnvironmentResultFailed           SwarmJoinEnvironmentResultState = "failed"

	NodeAgentStateNone              NodeAgentState       = "none"
	NodeAgentStatePending           NodeAgentState       = "pending"
	NodeAgentStateOffline           NodeAgentState       = "offline"
	NodeAgentStateConnected         NodeAgentState       = "connected"
	NodeAgentStateMismatched        NodeAgentState       = "mismatched"
	NodeAgentStateAmbiguous         NodeAgentState       = "ambiguous"
	NodeAgentBindingKindLocal       NodeAgentBindingKind = "local"
	NodeAgentBindingKindEnvironment NodeAgentBindingKind = "environment"
	NodeAgentBindingKindDedicated   NodeAgentBindingKind = "dedicated"

	SwarmStackNamespaceLabel = "com.docker.stack.namespace"
)

func NewSwarmInfo(s swarm.Swarm) SwarmInfo {
	return SwarmInfo{
		ID:                     s.ID,
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
		Spec:                   s.Spec,
		RootRotationInProgress: s.RootRotationInProgress,
	}
}

func NewSwarmConfig(cfg swarm.Config) SwarmConfig {
	return SwarmConfig{
		ID:        cfg.ID,
		Version:   cfg.Version,
		CreatedAt: cfg.CreatedAt,
		UpdatedAt: cfg.UpdatedAt,
		Spec:      cfg.Spec,
	}
}

func NewSwarmSecret(secret swarm.Secret) SwarmSecret {
	return SwarmSecret{
		ID:        secret.ID,
		Version:   secret.Version,
		CreatedAt: secret.CreatedAt,
		UpdatedAt: secret.UpdatedAt,
		Spec:      secret.Spec,
	}
}

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

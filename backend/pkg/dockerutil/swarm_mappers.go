package docker

import (
	"fmt"

	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/moby/moby/api/types/swarm"
)

// NewSwarmInfo converts Docker swarm metadata to the API shape.
func NewSwarmInfo(s swarm.Swarm) swarmtypes.SwarmInfo {
	return swarmtypes.SwarmInfo{
		ID:                     s.ID,
		CreatedAt:              s.CreatedAt,
		UpdatedAt:              s.UpdatedAt,
		Spec:                   s.Spec,
		RootRotationInProgress: s.RootRotationInProgress,
	}
}

// NewSwarmConfigSummary converts a Docker swarm config to the API shape.
func NewSwarmConfigSummary(cfg swarm.Config) swarmtypes.ConfigSummary {
	return swarmtypes.ConfigSummary{
		ID:        cfg.ID,
		Version:   cfg.Version,
		CreatedAt: cfg.CreatedAt,
		UpdatedAt: cfg.UpdatedAt,
		Spec:      cfg.Spec,
	}
}

// NewSwarmSecretSummary converts a Docker swarm secret to the API shape.
func NewSwarmSecretSummary(secret swarm.Secret) swarmtypes.SecretSummary {
	return swarmtypes.SecretSummary{
		ID:        secret.ID,
		Version:   secret.Version,
		CreatedAt: secret.CreatedAt,
		UpdatedAt: secret.UpdatedAt,
		Spec:      secret.Spec,
	}
}

// NewSwarmNodeSummary converts a Docker swarm node to the API shape.
func NewSwarmNodeSummary(node swarm.Node) swarmtypes.NodeSummary {
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

	return swarmtypes.NodeSummary{
		ID:             node.ID,
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
		Agent: swarmtypes.NodeAgentStatus{
			State: swarmtypes.NodeAgentStateNone,
		},
	}
}

// NewSwarmServiceSummary converts a Docker swarm service and enrichment data to the API shape.
func NewSwarmServiceSummary(service swarm.Service, nodeNames []string, networkNameByID map[string]string) swarmtypes.ServiceSummary {
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
	ports := make([]swarmtypes.ServicePort, 0, len(portSpecs))
	for _, port := range portSpecs {
		ports = append(ports, swarmtypes.ServicePort{
			Protocol:      string(port.Protocol),
			TargetPort:    port.TargetPort,
			PublishedPort: port.PublishedPort,
			PublishMode:   string(port.PublishMode),
		})
	}

	stackName := ""
	if spec.Labels != nil {
		stackName = spec.Labels[swarmtypes.StackNamespaceLabel]
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

	mounts := make([]swarmtypes.ServiceMount, 0)
	if spec.TaskTemplate.ContainerSpec != nil {
		for _, m := range spec.TaskTemplate.ContainerSpec.Mounts {
			mounts = append(mounts, swarmtypes.ServiceMount{
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

	return swarmtypes.ServiceSummary{
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

// NewSwarmServiceInspect converts a Docker swarm service to the API inspect shape.
func NewSwarmServiceInspect(service swarm.Service) swarmtypes.ServiceInspect {
	return swarmtypes.ServiceInspect{
		ID:           service.ID,
		Version:      service.Version,
		CreatedAt:    service.CreatedAt,
		UpdatedAt:    service.UpdatedAt,
		Spec:         service.Spec,
		Endpoint:     service.Endpoint,
		UpdateStatus: service.UpdateStatus,
	}
}

// NewSwarmTaskSummary converts a Docker swarm task and display names to the API shape.
func NewSwarmTaskSummary(task swarm.Task, serviceName, nodeName string) swarmtypes.TaskSummary {
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

	return swarmtypes.TaskSummary{
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
		Slot:         task.Slot,
		CreatedAt:    task.CreatedAt,
		UpdatedAt:    task.UpdatedAt,
	}
}

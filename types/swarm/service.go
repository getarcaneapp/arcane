package swarm

import (
	"encoding/json"
	"time"

	"github.com/moby/moby/api/types/swarm"
)

const StackNamespaceLabel = "com.docker.stack.namespace"

type ServicePort struct {
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

type ServiceMount struct {
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

type ServiceSummary struct {
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

	// Mode is the service mode (replicated or global).
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
	Ports []ServicePort `json:"ports"`

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
	Mounts []ServiceMount `json:"mounts"`
}

type ServiceInspect struct {
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
	NetworkDetails map[string]ServiceNetworkDetail `json:"networkDetails,omitempty"`

	// Mounts contains enriched mount information with volume driver details.
	Mounts []ServiceMount `json:"mounts,omitempty"`
}

// ServiceNetworkIPAMConfig represents a single IPAM configuration entry with camelCase JSON tags.
type ServiceNetworkIPAMConfig struct {
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	IPRange string `json:"ipRange,omitempty"`
}

// ServiceNetworkConfigDetail holds details about a config-only network referenced by configFrom.
type ServiceNetworkConfigDetail struct {
	Name        string                     `json:"name"`
	Driver      string                     `json:"driver"`
	Scope       string                     `json:"scope"`
	EnableIPv4  bool                       `json:"enableIPv4"`
	EnableIPv6  bool                       `json:"enableIPv6"`
	Options     map[string]string          `json:"options,omitempty"`
	IPv4Configs []ServiceNetworkIPAMConfig `json:"ipv4Configs,omitempty"`
	IPv6Configs []ServiceNetworkIPAMConfig `json:"ipv6Configs,omitempty"`
}

// ServiceNetworkDetail holds enriched network information for a service's attached network.
type ServiceNetworkDetail struct {
	ID            string                      `json:"id"`
	Name          string                      `json:"name"`
	Driver        string                      `json:"driver"`
	Scope         string                      `json:"scope"`
	Internal      bool                        `json:"internal"`
	Attachable    bool                        `json:"attachable"`
	Ingress       bool                        `json:"ingress"`
	EnableIPv4    bool                        `json:"enableIPv4"`
	EnableIPv6    bool                        `json:"enableIPv6"`
	ConfigFrom    string                      `json:"configFrom,omitempty"`
	ConfigOnly    bool                        `json:"configOnly"`
	Options       map[string]string           `json:"options,omitempty"`
	IPAMConfigs   []ServiceNetworkIPAMConfig  `json:"ipamConfigs,omitempty"`
	ConfigNetwork *ServiceNetworkConfigDetail `json:"configNetwork,omitempty"`
}

type ServiceCreateRequest struct {
	// Spec is the service specification as a JSON object.
	//
	// Required: true
	Spec json.RawMessage `json:"spec" doc:"Service specification"`

	// Options are additional create options for the service.
	//
	// Required: false
	Options *ServiceCreateOptions `json:"options,omitempty" doc:"Additional create options"`
}

type ServiceUpdateRequest struct {
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
	Options *ServiceUpdateOptions `json:"options,omitempty"`
}

type ServiceCreateResponse struct {
	// ID is the created service ID.
	//
	// Required: true
	ID string `json:"id"`

	// Warnings are any warnings returned by the Docker API.
	//
	// Required: false
	Warnings []string `json:"warnings,omitempty"`
}

type ServiceUpdateResponse struct {
	// Warnings are any warnings returned by the Docker API.
	//
	// Required: false
	Warnings []string `json:"warnings,omitempty"`
}

type ServiceCreateOptions struct {
	// EncodedRegistryAuth is the encoded registry authorization credentials.
	//
	// Required: false
	EncodedRegistryAuth string `json:"encodedRegistryAuth,omitempty"`

	// QueryRegistry indicates if registry metadata should be queried.
	//
	// Required: false
	QueryRegistry bool `json:"queryRegistry,omitempty"`
}

type ServiceUpdateOptions struct {
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

type ServiceScaleRequest struct {
	// Replicas is the desired replica count.
	//
	// Required: true
	Replicas uint64 `json:"replicas"`
}

func NewServiceSummary(service swarm.Service, nodeNames []string, networkNameByID map[string]string) ServiceSummary {
	spec := service.Spec

	mode := "unknown"
	replicas := uint64(0)
	runningReplicas := uint64(0)
	if spec.Mode.Replicated != nil {
		mode = "replicated"
		if spec.Mode.Replicated.Replicas != nil {
			replicas = *spec.Mode.Replicated.Replicas
		}
	} else if spec.Mode.Global != nil {
		mode = "global"
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

	ports := make([]ServicePort, 0)
	portSpecs := service.Endpoint.Spec.Ports
	if len(portSpecs) == 0 {
		portSpecs = service.Endpoint.Ports
	}
	for _, port := range portSpecs {
		ports = append(ports, ServicePort{
			Protocol:      string(port.Protocol),
			TargetPort:    port.TargetPort,
			PublishedPort: port.PublishedPort,
			PublishMode:   string(port.PublishMode),
		})
	}

	stackName := ""
	if spec.Labels != nil {
		stackName = spec.Labels[StackNamespaceLabel]
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
	mounts := make([]ServiceMount, 0)
	if spec.TaskTemplate.ContainerSpec != nil {
		for _, m := range spec.TaskTemplate.ContainerSpec.Mounts {
			mounts = append(mounts, ServiceMount{
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

	return ServiceSummary{
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

func NewServiceInspect(service swarm.Service) ServiceInspect {
	return ServiceInspect{
		ID:           service.ID,
		Version:      service.Version,
		CreatedAt:    service.CreatedAt,
		UpdatedAt:    service.UpdatedAt,
		Spec:         service.Spec,
		Endpoint:     service.Endpoint,
		UpdateStatus: service.UpdateStatus,
	}
}

package swarm

import (
	"encoding/json"
	"time"

	"github.com/docker/docker/api/types/swarm"
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
}

type ServiceCreateRequest struct {
	// Spec is the service specification as a JSON object.
	//
	// Required: true
	Spec json.RawMessage `json:"spec" doc:"Service specification"`

	// Options are additional create options for the service.
	//
	// Required: false
	Options json.RawMessage `json:"options,omitempty" doc:"Additional create options"`
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
	Options swarm.ServiceUpdateOptions `json:"options,omitempty"`
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

func NewServiceSummary(service swarm.Service) ServiceSummary {
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

package podman

import (
	"encoding/json"
	"time"
)

type PodContainer struct {
	ID           string `json:"id"`
	Names        string `json:"names"`
	Status       string `json:"status"`
	RestartCount uint   `json:"restartCount"`
}

type Pod struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	Status     string            `json:"status"`
	Created    time.Time         `json:"created"`
	InfraID    string            `json:"infraId,omitempty"`
	Cgroup     string            `json:"cgroup,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Networks   []string          `json:"networks,omitempty"`
	Containers []PodContainer    `json:"containers"`
}

type PodPortBinding struct {
	HostIP   string `json:"hostIp,omitempty"`
	HostPort string `json:"hostPort"`
}

type PodInfraConfig struct {
	PortBindings map[string][]PodPortBinding `json:"portBindings,omitempty"`
	HostNetwork  bool                        `json:"hostNetwork"`
	Networks     []string                    `json:"networks,omitempty"`
}

type PodInspectContainer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type PodInspect struct {
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Namespace        string                `json:"namespace,omitempty"`
	Created          time.Time             `json:"created"`
	State            string                `json:"state"`
	Hostname         string                `json:"hostname,omitempty"`
	Labels           map[string]string     `json:"labels,omitempty"`
	CreateInfra      bool                  `json:"createInfra"`
	InfraContainerID string                `json:"infraContainerId,omitempty"`
	InfraConfig      *PodInfraConfig       `json:"infraConfig,omitempty"`
	SharedNamespaces []string              `json:"sharedNamespaces,omitempty"`
	NumContainers    uint                  `json:"numContainers"`
	Containers       []PodInspectContainer `json:"containers"`
}

type PodPortMapping struct {
	ContainerPort uint16 `json:"containerPort" minimum:"1" doc:"Port inside the pod"`
	HostPort      uint16 `json:"hostPort,omitempty" doc:"Port on the host"`
	HostIP        string `json:"hostIp,omitempty" doc:"Host IP to bind"`
	Protocol      string `json:"protocol,omitempty" doc:"tcp, udp, or sctp"`
	Range         uint16 `json:"range,omitempty" doc:"Number of consecutive ports to map"`
}

type PodCreateRequest struct {
	Name             string            `json:"name" minLength:"1" doc:"Pod name"`
	Labels           map[string]string `json:"labels,omitempty" doc:"Pod labels"`
	Hostname         string            `json:"hostname,omitempty" doc:"Pod hostname"`
	PortMappings     []PodPortMapping  `json:"portMappings,omitempty" doc:"Published ports"`
	Network          string            `json:"network,omitempty" doc:"Network to join"`
	SharedNamespaces []string          `json:"sharedNamespaces,omitempty" doc:"Namespaces shared between pod members"`
	NoInfra          bool              `json:"noInfra,omitempty" doc:"Create the pod without an infra container"`
	Advanced         json.RawMessage   `json:"advanced,omitempty" doc:"Raw libpod PodSpecGenerator fields merged over the typed fields"`
}

type PodActionRequest struct {
	Timeout *int   `json:"timeout,omitempty" doc:"Seconds to wait before killing the pod on stop"`
	Signal  string `json:"signal,omitempty" doc:"Signal sent on kill"`
}

type PodCreateResponse struct {
	ID string `json:"id"`
}

type PodStats struct {
	Pod           string `json:"pod"`
	CID           string `json:"cid"`
	Name          string `json:"name"`
	CPU           string `json:"cpu"`
	MemUsage      string `json:"memUsage"`
	MemUsageBytes string `json:"memUsageBytes"`
	Mem           string `json:"mem"`
	NetIO         string `json:"netIo"`
	BlockIO       string `json:"blockIo"`
	PIDS          string `json:"pids"`
}

type PodTop struct {
	Titles    []string   `json:"titles"`
	Processes [][]string `json:"processes"`
}

type PodPruneReport struct {
	ID string `json:"id"`
}

type Container struct {
	ID        string            `json:"id"`
	Names     []string          `json:"names"`
	Image     string            `json:"image"`
	ImageID   string            `json:"imageId,omitempty"`
	State     string            `json:"state"`
	Status    string            `json:"status,omitempty"`
	Created   time.Time         `json:"created"`
	StartedAt int64             `json:"startedAt,omitempty"`
	ExitCode  int32             `json:"exitCode"`
	Exited    bool              `json:"exited"`
	Labels    map[string]string `json:"labels,omitempty"`
	Pod       string            `json:"pod,omitempty"`
	PodName   string            `json:"podName,omitempty"`
	IsInfra   bool              `json:"isInfra"`
}

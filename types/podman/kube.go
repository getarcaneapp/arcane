package podman

type KubeGenerateType string

const (
	KubeGenerateTypePod        KubeGenerateType = "pod"
	KubeGenerateTypeService    KubeGenerateType = "service"
	KubeGenerateTypeDeployment KubeGenerateType = "deployment"
	KubeGenerateTypeDaemonSet  KubeGenerateType = "daemonset"
	KubeGenerateTypeJob        KubeGenerateType = "job"
)

type KubePlayRequest struct {
	YAML     string   `json:"yaml" minLength:"1" doc:"Kubernetes YAML to play"`
	Start    *bool    `json:"start,omitempty" doc:"Start the pods after creating them"`
	Replace  bool     `json:"replace,omitempty" doc:"Replace existing pods and containers"`
	Networks []string `json:"networks,omitempty" doc:"Networks to attach the pods to"`
}

type KubeDownRequest struct {
	YAML  string `json:"yaml" minLength:"1" doc:"Kubernetes YAML to tear down"`
	Force bool   `json:"force,omitempty" doc:"Also remove volumes created by the YAML"`
}

type KubeGenerateRequest struct {
	Names   []string         `json:"names" minItems:"1" doc:"Pods, containers, or volumes to include"`
	Type    KubeGenerateType `json:"type,omitempty" enum:"pod,service,deployment,daemonset,job" doc:"Kubernetes kind to generate"`
	Service bool             `json:"service,omitempty" doc:"Also generate a Kubernetes service"`
}

type KubeGenerateResponse struct {
	YAML string `json:"yaml"`
}

type KubePlayPod struct {
	ID              string   `json:"id"`
	Containers      []string `json:"containers"`
	InitContainers  []string `json:"initContainers"`
	Logs            []string `json:"logs"`
	ContainerErrors []string `json:"containerErrors"`
}

type KubePlayVolume struct {
	Name string `json:"name"`
}

type KubePlayReport struct {
	Pods               []KubePlayPod    `json:"pods"`
	Volumes            []KubePlayVolume `json:"volumes"`
	ServiceContainerID string           `json:"serviceContainerId,omitempty"`
	ExitCode           *int32           `json:"exitCode,omitempty"`
}

type KubeDownPod struct {
	ID string `json:"id"`
}

type KubeDownReport struct {
	StopReport []KubeDownPod `json:"stopReport"`
	RmReport   []KubeDownPod `json:"rmReport"`
}

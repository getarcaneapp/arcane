package types

type (
	PortMapping struct {
		EnvironmentTag
		ID            string `json:"id"`
		ContainerID   string `json:"containerId"`
		ContainerName string `json:"containerName"`
		HostIP        string `json:"hostIp,omitempty"`
		Protocol      string `json:"protocol"`
		HostPort      int    `json:"hostPort,omitempty"`
		ContainerPort int    `json:"containerPort"`
		IsPublished   bool   `json:"isPublished"`
	}
)

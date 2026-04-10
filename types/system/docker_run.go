package system

type DockerRunCommand struct {
	Image       string   `json:"image"`
	Name        string   `json:"name,omitempty"`
	Ports       []string `json:"ports,omitempty"`
	Volumes     []string `json:"volumes,omitempty"`
	Environment []string `json:"environment,omitempty"`
	Networks    []string `json:"networks,omitempty"`
	Restart     string   `json:"restart,omitempty"`
	Workdir     string   `json:"workdir,omitempty"`
	User        string   `json:"user,omitempty"`
	Entrypoint  string   `json:"entrypoint,omitempty"`
	Command     string   `json:"command,omitempty"`
	Detached    bool     `json:"detached,omitempty"`
	Interactive bool     `json:"interactive,omitempty"`
	TTY         bool     `json:"tty,omitempty"`
	Remove      bool     `json:"remove,omitempty"`
	Privileged  bool     `json:"privileged,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	HealthCheck string   `json:"healthCheck,omitempty"`
	MemoryLimit string   `json:"memoryLimit,omitempty"`
	CPULimit    string   `json:"cpuLimit,omitempty"`
}

type DockerComposeHealthcheck struct {
	Test string `yaml:"test" json:"test"`
}

type DockerComposeResources struct {
	Limits *DockerComposeResourceLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
}

type DockerComposeResourceLimits struct {
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`
	CPUs   string `yaml:"cpus,omitempty" json:"cpus,omitempty"`
}

type DockerComposeDeploy struct {
	Resources *DockerComposeResources `yaml:"resources,omitempty" json:"resources,omitempty"`
}

type DockerComposeService struct {
	Image         string                    `yaml:"image" json:"image"`
	ContainerName string                    `yaml:"container_name,omitempty" json:"container_name,omitempty"`
	Ports         []string                  `yaml:"ports,omitempty" json:"ports,omitempty"`
	Volumes       []string                  `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Environment   []string                  `yaml:"environment,omitempty" json:"environment,omitempty"`
	Networks      []string                  `yaml:"networks,omitempty" json:"networks,omitempty"`
	Restart       string                    `yaml:"restart,omitempty" json:"restart,omitempty"`
	WorkingDir    string                    `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	User          string                    `yaml:"user,omitempty" json:"user,omitempty"`
	Entrypoint    string                    `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Command       string                    `yaml:"command,omitempty" json:"command,omitempty"`
	StdinOpen     bool                      `yaml:"stdin_open,omitempty" json:"stdin_open,omitempty"`
	TTY           bool                      `yaml:"tty,omitempty" json:"tty,omitempty"`
	Privileged    bool                      `yaml:"privileged,omitempty" json:"privileged,omitempty"`
	Labels        []string                  `yaml:"labels,omitempty" json:"labels,omitempty"`
	Healthcheck   *DockerComposeHealthcheck `yaml:"healthcheck,omitempty" json:"healthcheck,omitempty"`
	Deploy        *DockerComposeDeploy      `yaml:"deploy,omitempty" json:"deploy,omitempty"`
}

type DockerComposeConfig struct {
	Services map[string]DockerComposeService `yaml:"services" json:"services"`
}

type ConvertDockerRunRequest struct {
	DockerRunCommand string `json:"dockerRunCommand" binding:"required"`
}

type ConvertDockerRunResponse struct {
	Success       bool   `json:"success"`
	DockerCompose string `json:"dockerCompose"`
	EnvVars       string `json:"envVars"`
	ServiceName   string `json:"serviceName"`
}

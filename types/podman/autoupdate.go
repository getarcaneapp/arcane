package podman

type AutoUpdateMode string

const (
	AutoUpdateModeREST        AutoUpdateMode = "rest"
	AutoUpdateModeSystemd     AutoUpdateMode = "systemd"
	AutoUpdateModeUnavailable AutoUpdateMode = "unavailable"
)

type AutoUpdateOptions struct {
	DryRun    bool                `json:"dryRun,omitempty"`
	Rollback  *bool               `json:"rollback,omitempty"`
	TLSVerify *bool               `json:"tlsVerify,omitempty"`
	Authfile  string              `json:"authfile,omitempty"`
	Filters   map[string][]string `json:"filters,omitempty"`
}

type AutoUpdateReport struct {
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	ImageName     string `json:"imageName"`
	Policy        string `json:"policy"`
	SystemdUnit   string `json:"systemdUnit"`
	Updated       string `json:"updated"`
}

type AutoUpdateResult struct {
	Mode    AutoUpdateMode     `json:"mode"`
	Reports []AutoUpdateReport `json:"reports"`
	Errors  []string           `json:"errors"`
	Unit    *UnitStatus        `json:"unit,omitempty"`
}

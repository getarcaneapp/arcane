package podman

type SystemdScope string

const (
	SystemdScopeSystem SystemdScope = "system"
	SystemdScopeUser   SystemdScope = "user"
)

type Status struct {
	Detected             bool           `json:"detected"`
	Version              string         `json:"version,omitempty"`
	APIVersion           string         `json:"apiVersion,omitempty"`
	Rootless             bool           `json:"rootless"`
	QuadletsSupported    bool           `json:"quadletsSupported"`
	QuadletAppsSupported bool           `json:"quadletAppsSupported"`
	AutoUpdateMode       AutoUpdateMode `json:"autoUpdateMode"`
	SystemdAvailable     bool           `json:"systemdAvailable"`
	SystemdScope         SystemdScope   `json:"systemdScope,omitempty"`
	SystemdReason        string         `json:"systemdReason,omitempty"`
}

type SystemdAvailability struct {
	Available bool         `json:"available"`
	Scope     SystemdScope `json:"scope,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}

type UnitStatus struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	LoadState   string `json:"loadState"`
	ActiveState string `json:"activeState"`
	SubState    string `json:"subState"`
}

type VersionComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Version struct {
	Version       string             `json:"version"`
	APIVersion    string             `json:"apiVersion"`
	MinAPIVersion string             `json:"minApiVersion,omitempty"`
	GitCommit     string             `json:"gitCommit,omitempty"`
	GoVersion     string             `json:"goVersion,omitempty"`
	Os            string             `json:"os,omitempty"`
	Arch          string             `json:"arch,omitempty"`
	KernelVersion string             `json:"kernelVersion,omitempty"`
	Components    []VersionComponent `json:"components,omitempty"`
}

type InfoSecurity struct {
	Rootless        bool   `json:"rootless"`
	SELinuxEnabled  bool   `json:"selinuxEnabled"`
	AppArmorEnabled bool   `json:"apparmorEnabled"`
	SeccompEnabled  bool   `json:"seccompEnabled"`
	Capabilities    string `json:"capabilities,omitempty"`
}

type InfoDistribution struct {
	Distribution string `json:"distribution,omitempty"`
	Variant      string `json:"variant,omitempty"`
	Version      string `json:"version,omitempty"`
	Codename     string `json:"codename,omitempty"`
}

type InfoOCIRuntime struct {
	Name    string `json:"name,omitempty"`
	Package string `json:"package,omitempty"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

type InfoRemoteSocket struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
}

type InfoHost struct {
	Arch           string           `json:"arch,omitempty"`
	BuildahVersion string           `json:"buildahVersion,omitempty"`
	CgroupManager  string           `json:"cgroupManager,omitempty"`
	CgroupVersion  string           `json:"cgroupVersion,omitempty"`
	CPUs           int              `json:"cpus"`
	Distribution   InfoDistribution `json:"distribution"`
	EventLogger    string           `json:"eventLogger,omitempty"`
	Hostname       string           `json:"hostname,omitempty"`
	Kernel         string           `json:"kernel,omitempty"`
	MemFree        int64            `json:"memFree"`
	MemTotal       int64            `json:"memTotal"`
	NetworkBackend string           `json:"networkBackend,omitempty"`
	OCIRuntime     InfoOCIRuntime   `json:"ociRuntime"`
	Os             string           `json:"os,omitempty"`
	RemoteSocket   InfoRemoteSocket `json:"remoteSocket"`
	Security       InfoSecurity     `json:"security"`
	Uptime         string           `json:"uptime,omitempty"`
}

type InfoContainerStore struct {
	Number  int `json:"number"`
	Paused  int `json:"paused"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
}

type InfoImageStore struct {
	Number int `json:"number"`
}

type InfoStore struct {
	ConfigFile      string             `json:"configFile,omitempty"`
	ContainerStore  InfoContainerStore `json:"containerStore"`
	GraphDriverName string             `json:"graphDriverName,omitempty"`
	GraphRoot       string             `json:"graphRoot,omitempty"`
	ImageStore      InfoImageStore     `json:"imageStore"`
	RunRoot         string             `json:"runRoot,omitempty"`
}

type InfoVersion struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Version    string `json:"version,omitempty"`
	GoVersion  string `json:"goVersion,omitempty"`
	GitCommit  string `json:"gitCommit,omitempty"`
	BuiltTime  string `json:"builtTime,omitempty"`
	OsArch     string `json:"osArch,omitempty"`
}

type Info struct {
	Host    InfoHost    `json:"host"`
	Store   InfoStore   `json:"store"`
	Version InfoVersion `json:"version"`
}

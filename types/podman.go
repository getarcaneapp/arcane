package types

import (
	"strings"
	"time"
)

type PodmanAutoUpdateMode string

const (
	PodmanAutoUpdateModeREST        PodmanAutoUpdateMode = "rest"
	PodmanAutoUpdateModeSystemd     PodmanAutoUpdateMode = "systemd"
	PodmanAutoUpdateModeUnavailable PodmanAutoUpdateMode = "unavailable"
)

type PodmanAutoUpdateOptions struct {
	DryRun    bool                `json:"dryRun,omitempty"`
	Rollback  *bool               `json:"rollback,omitempty"`
	TLSVerify *bool               `json:"tlsVerify,omitempty"`
	Authfile  string              `json:"authfile,omitempty"`
	Filters   map[string][]string `json:"filters,omitempty"`
}

type PodmanAutoUpdateReport struct {
	ContainerID   string `json:"containerId"`
	ContainerName string `json:"containerName"`
	ImageName     string `json:"imageName"`
	Policy        string `json:"policy"`
	SystemdUnit   string `json:"systemdUnit"`
	Updated       string `json:"updated"`
}

type PodmanAutoUpdateResult struct {
	Mode    PodmanAutoUpdateMode     `json:"mode"`
	Reports []PodmanAutoUpdateReport `json:"reports"`
	Errors  []string                 `json:"errors"`
	Unit    *PodmanUnitStatus        `json:"unit,omitempty"`
}

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

const PodmanSystemdUnitLabel = "PODMAN_SYSTEMD_UNIT"

func QuadletUnit(labels map[string]string) string {
	unit := labels[PodmanSystemdUnitLabel]
	if strings.HasPrefix(unit, "podman-compose@") {
		return ""
	}
	return unit
}

type PodContainer struct {
	ID           string `json:"id"`
	Names        string `json:"names"`
	Status       string `json:"status"`
	RestartCount uint   `json:"restartCount"`
}

type Pod struct {
	EnvironmentTag

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
	PortBindings       map[string][]PodPortBinding `json:"portBindings,omitempty"`
	HostNetwork        bool                        `json:"hostNetwork"`
	StaticIP           string                      `json:"staticIp,omitempty"`
	StaticMAC          string                      `json:"staticMac,omitempty"`
	NoManageResolvConf bool                        `json:"noManageResolvConf"`
	DNSServer          []string                    `json:"dnsServer,omitempty"`
	DNSSearch          []string                    `json:"dnsSearch,omitempty"`
	DNSOption          []string                    `json:"dnsOption,omitempty"`
	NoManageHostname   bool                        `json:"noManageHostname"`
	NoManageHosts      bool                        `json:"noManageHosts"`
	HostAdd            []string                    `json:"hostAdd,omitempty"`
	HostsFile          string                      `json:"hostsFile,omitempty"`
	Networks           []string                    `json:"networks,omitempty"`
	NetworkOptions     map[string][]string         `json:"networkOptions,omitempty"`
	PidNS              string                      `json:"pidNs,omitempty"`
	UserNS             string                      `json:"userNs,omitempty"`
	UtsNS              string                      `json:"utsNs,omitempty"`
}

type PodInspectContainer struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	State    string           `json:"state"`
	Image    string           `json:"image,omitempty"`
	Status   string           `json:"status,omitempty"`
	Created  time.Time        `json:"created"`
	ExitCode int32            `json:"exitCode"`
	IsInfra  bool             `json:"isInfra"`
	Ports    []PodPortMapping `json:"ports,omitempty"`
}

type PodMount struct {
	Type        string   `json:"type"`
	Name        string   `json:"name,omitempty"`
	Source      string   `json:"source,omitempty"`
	Destination string   `json:"destination"`
	Driver      string   `json:"driver,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Options     []string `json:"options,omitempty"`
	RW          bool     `json:"rw"`
	Propagation string   `json:"propagation,omitempty"`
	SubPath     string   `json:"subPath,omitempty"`
}

type PodDevice struct {
	PathOnHost        string `json:"pathOnHost"`
	PathInContainer   string `json:"pathInContainer"`
	CgroupPermissions string `json:"cgroupPermissions,omitempty"`
}

type PodWeightDevice struct {
	Path   string `json:"path"`
	Weight uint16 `json:"weight"`
}

type PodThrottleDevice struct {
	Path string `json:"path"`
	Rate uint64 `json:"rate"`
}

type PodInspect struct {
	ID                 string                `json:"id"`
	Name               string                `json:"name"`
	Namespace          string                `json:"namespace,omitempty"`
	Created            time.Time             `json:"created"`
	State              string                `json:"state"`
	CreateCommand      []string              `json:"createCommand,omitempty"`
	ExitPolicy         string                `json:"exitPolicy,omitempty"`
	RestartPolicy      string                `json:"restartPolicy,omitempty"`
	Hostname           string                `json:"hostname,omitempty"`
	Labels             map[string]string     `json:"labels,omitempty"`
	CreateCgroup       bool                  `json:"createCgroup"`
	CgroupParent       string                `json:"cgroupParent,omitempty"`
	CgroupPath         string                `json:"cgroupPath,omitempty"`
	CreateInfra        bool                  `json:"createInfra"`
	InfraContainerID   string                `json:"infraContainerId,omitempty"`
	InfraConfig        *PodInfraConfig       `json:"infraConfig,omitempty"`
	SharedNamespaces   []string              `json:"sharedNamespaces,omitempty"`
	NumContainers      uint                  `json:"numContainers"`
	Containers         []PodInspectContainer `json:"containers"`
	CPUPeriod          uint64                `json:"cpuPeriod,omitempty"`
	CPUQuota           int64                 `json:"cpuQuota,omitempty"`
	CPUShares          uint64                `json:"cpuShares,omitempty"`
	CPUSetCPUs         string                `json:"cpusetCpus,omitempty"`
	CPUSetMems         string                `json:"cpusetMems,omitempty"`
	MemoryLimit        uint64                `json:"memoryLimit,omitempty"`
	MemorySwap         uint64                `json:"memorySwap,omitempty"`
	BlkioWeight        uint64                `json:"blkioWeight,omitempty"`
	BlkioWeightDevices []PodWeightDevice     `json:"blkioWeightDevices,omitempty"`
	DeviceReadBps      []PodThrottleDevice   `json:"deviceReadBps,omitempty"`
	DeviceWriteBps     []PodThrottleDevice   `json:"deviceWriteBps,omitempty"`
	Mounts             []PodMount            `json:"mounts,omitempty"`
	Devices            []PodDevice           `json:"devices,omitempty"`
	VolumesFrom        []string              `json:"volumesFrom,omitempty"`
	SecurityOpts       []string              `json:"securityOpts,omitempty"`
}

type PodPortMapping struct {
	ContainerPort uint16 `json:"containerPort" minimum:"1" doc:"Port inside the pod"`
	HostPort      uint16 `json:"hostPort,omitempty" doc:"Port on the host"`
	HostIP        string `json:"hostIp,omitempty" doc:"Host IP to bind"`
	Protocol      string `json:"protocol,omitempty" doc:"tcp, udp, or sctp"`
	Range         uint16 `json:"range,omitempty" doc:"Number of consecutive ports to map"`
}

type PodNetwork struct {
	Name          string   `json:"name" minLength:"1" doc:"Network to join"`
	Aliases       []string `json:"aliases,omitempty" doc:"Network-scoped aliases"`
	StaticIPs     []string `json:"staticIps,omitempty" doc:"Static IPv4 or IPv6 addresses"`
	StaticMAC     string   `json:"staticMac,omitempty" doc:"Static MAC address"`
	InterfaceName string   `json:"interfaceName,omitempty" doc:"Interface name inside the pod"`
}

type PodVolume struct {
	Source      string   `json:"source" minLength:"1" doc:"Absolute host path or named volume"`
	Destination string   `json:"destination" minLength:"1" doc:"Path inside the pod"`
	Options     []string `json:"options,omitempty" doc:"Mount options such as ro, z, Z, or U"`
}

type PodDeviceLimit struct {
	Device string `json:"device" minLength:"1" doc:"Block device as MAJOR:MINOR"`
	Value  string `json:"value" minLength:"1" doc:"Weight (10-1000) for weight limits, or a rate such as 1mb for throughput limits"`
}

type PodCreateRequest struct {
	Name               string            `json:"name" minLength:"1" doc:"Pod name"`
	Hostname           string            `json:"hostname,omitempty" doc:"Pod hostname"`
	Labels             map[string]string `json:"labels,omitempty" doc:"Pod labels"`
	ExitPolicy         string            `json:"exitPolicy,omitempty" enum:"continue,stop" doc:"Behaviour when the last container exits"`
	RestartPolicy      string            `json:"restartPolicy,omitempty" enum:"no,always,on-failure,unless-stopped" doc:"Restart policy for containers in the pod"`
	RestartRetries     *uint             `json:"restartRetries,omitempty" doc:"Restart attempts for the on-failure policy"`
	NoInfra            bool              `json:"noInfra,omitempty" doc:"Create the pod without an infra container"`
	InfraImage         string            `json:"infraImage,omitempty" doc:"Image for the infra container"`
	InfraCommand       []string          `json:"infraCommand,omitempty" doc:"Entrypoint of the infra container"`
	InfraName          string            `json:"infraName,omitempty" doc:"Name of the infra container"`
	InfraConmonPidFile string            `json:"infraConmonPidFile,omitempty" doc:"Path that receives the infra container's conmon PID"`
	SharedNamespaces   []string          `json:"sharedNamespaces,omitempty" doc:"Namespaces shared between pod members"`
	ShareParent        *bool             `json:"shareParent,omitempty" doc:"Use the pod's cgroup as the cgroup parent of its containers"`
	Pid                string            `json:"pid,omitempty" doc:"PID namespace: host, private, ns:<path>, or container:<id>"`
	Ipc                string            `json:"ipc,omitempty" doc:"IPC namespace: host, private, shareable, none, ns:<path>, or container:<id>"`
	Uts                string            `json:"uts,omitempty" doc:"UTS namespace: host, private, ns:<path>, or container:<id>"`
	Userns             string            `json:"userns,omitempty" doc:"User namespace, for example auto, keep-id, keep-id:uid=1000, host, or ns:<path>"`
	UIDMap             []string          `json:"uidMap,omitempty" doc:"UID mappings as CONTAINER:HOST:SIZE"`
	GIDMap             []string          `json:"gidMap,omitempty" doc:"GID mappings as CONTAINER:HOST:SIZE"`
	NetworkMode        string            `json:"networkMode,omitempty" doc:"bridge, host, none, private, slirp4netns, pasta, or ns:<path>"`
	NetworkOptions     []string          `json:"networkOptions,omitempty" doc:"Options for the slirp4netns or pasta network mode"`
	Networks           []PodNetwork      `json:"networks,omitempty" doc:"Networks to join"`
	PortMappings       []PodPortMapping  `json:"portMappings,omitempty" doc:"Published ports"`
	DNSServers         []string          `json:"dnsServers,omitempty" doc:"DNS servers"`
	DNSSearch          []string          `json:"dnsSearch,omitempty" doc:"DNS search domains"`
	DNSOptions         []string          `json:"dnsOptions,omitempty" doc:"DNS resolver options"`
	NoManageResolvConf bool              `json:"noManageResolvConf,omitempty" doc:"Use the image's /etc/resolv.conf"`
	AddHosts           []string          `json:"addHosts,omitempty" doc:"Extra /etc/hosts entries as HOST:IP"`
	NoHosts            bool              `json:"noHosts,omitempty" doc:"Use the image's /etc/hosts"`
	NoHostname         bool              `json:"noHostname,omitempty" doc:"Use the image's /etc/hostname"`
	HostsFile          string            `json:"hostsFile,omitempty" doc:"Base file for /etc/hosts, or image or none"`
	CgroupParent       string            `json:"cgroupParent,omitempty" doc:"Parent cgroup of the pod"`
	CPUs               float64           `json:"cpus,omitempty" minimum:"0" doc:"Number of CPUs"`
	CPUShares          uint64            `json:"cpuShares,omitempty" doc:"Relative CPU weight"`
	CPUSetCPUs         string            `json:"cpusetCpus,omitempty" doc:"CPUs allowed to run the pod, for example 0-3"`
	CPUSetMems         string            `json:"cpusetMems,omitempty" doc:"Memory nodes allowed to run the pod, for example 0,1"`
	Memory             string            `json:"memory,omitempty" doc:"Memory limit such as 512m or 2g"`
	MemorySwap         string            `json:"memorySwap,omitempty" doc:"Memory plus swap limit such as 1g, or -1 for unlimited swap"`
	BlkioWeight        uint16            `json:"blkioWeight,omitempty" minimum:"10" maximum:"1000" doc:"Relative block IO weight"`
	BlkioWeightDevices []PodDeviceLimit  `json:"blkioWeightDevices,omitempty" doc:"Per-device block IO weights"`
	DeviceReadBps      []PodDeviceLimit  `json:"deviceReadBps,omitempty" doc:"Per-device read rate limits"`
	DeviceWriteBps     []PodDeviceLimit  `json:"deviceWriteBps,omitempty" doc:"Per-device write rate limits"`
	Devices            []string          `json:"devices,omitempty" doc:"Host devices as HOST[:CONTAINER[:PERMISSIONS]]"`
	GPUs               []string          `json:"gpus,omitempty" doc:"NVIDIA GPUs to add through CDI, or all"`
	Volumes            []PodVolume       `json:"volumes,omitempty" doc:"Bind mounts and named volumes"`
	VolumesFrom        []string          `json:"volumesFrom,omitempty" doc:"Containers to mount volumes from, as CONTAINER[:OPTIONS]"`
	ShmSize            string            `json:"shmSize,omitempty" doc:"Size of /dev/shm such as 64m"`
	ShmSizeSystemd     string            `json:"shmSizeSystemd,omitempty" doc:"Size of systemd tmpfs mounts such as 64m"`
	SecurityOpt        []string          `json:"securityOpt,omitempty" doc:"Security options such as label=disable"`
	Sysctl             map[string]string `json:"sysctl,omitempty" doc:"Namespaced kernel parameters"`
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

type PodmanContainer struct {
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
	Ports     []PodPortMapping  `json:"ports,omitempty"`
}

type QuadletType string

const (
	QuadletTypeContainer QuadletType = "container"
	QuadletTypePod       QuadletType = "pod"
	QuadletTypeKube      QuadletType = "kube"
	QuadletTypeNetwork   QuadletType = "network"
	QuadletTypeVolume    QuadletType = "volume"
	QuadletTypeImage     QuadletType = "image"
	QuadletTypeBuild     QuadletType = "build"
	QuadletTypeArtifact  QuadletType = "artifact"
)

type Quadlet struct {
	Name     string `json:"name"`
	UnitName string `json:"unitName,omitempty"`
	Path     string `json:"path"`
	Status   string `json:"status"`
	App      string `json:"app,omitempty"`
}

type QuadletFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type QuadletInstallRequest struct {
	Name       string        `json:"name" minLength:"1" doc:"Quadlet file name including its extension"`
	Content    string        `json:"content" minLength:"1" doc:"Quadlet file contents"`
	ExtraFiles []QuadletFile `json:"extraFiles,omitempty" doc:"Additional files installed beside the quadlet"`
	App        string        `json:"app,omitempty" doc:"Application the quadlet belongs to"`
}

type QuadletUpdateRequest struct {
	Content    string        `json:"content" minLength:"1" doc:"Quadlet file contents"`
	ExtraFiles []QuadletFile `json:"extraFiles,omitempty" doc:"Additional files installed beside the quadlet"`
}

type QuadletDetail struct {
	EnvironmentTag

	Name       string            `json:"name"`
	UnitName   string            `json:"unitName"`
	Path       string            `json:"path"`
	Status     string            `json:"status"`
	App        string            `json:"app,omitempty"`
	Type       QuadletType       `json:"type"`
	Template   bool              `json:"template"`
	UnitStatus *PodmanUnitStatus `json:"unitStatus,omitempty"`
	Containers []PodmanContainer `json:"containers,omitempty"`
}

type QuadletInstallReport struct {
	InstalledQuadlets map[string]string `json:"installedQuadlets"`
	QuadletErrors     map[string]string `json:"quadletErrors"`
}

type QuadletRemoveReport struct {
	Removed []string          `json:"removed"`
	Errors  map[string]string `json:"errors"`
}

type PodmanSecretDriver struct {
	Name    string            `json:"name"`
	Options map[string]string `json:"options,omitempty"`
}

type PodmanSecretSpec struct {
	Name   string             `json:"name"`
	Driver PodmanSecretDriver `json:"driver"`
	Labels map[string]string  `json:"labels,omitempty"`
}

type PodmanSecret struct {
	EnvironmentTag

	ID        string           `json:"id"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Spec      PodmanSecretSpec `json:"spec"`
}

type PodmanSecretCreateRequest struct {
	Name          string            `json:"name" minLength:"1" doc:"Secret name"`
	Data          string            `json:"data" minLength:"1" doc:"Secret value"`
	Driver        string            `json:"driver,omitempty" doc:"Secret driver"`
	DriverOptions map[string]string `json:"driverOptions,omitempty" doc:"Secret driver options"`
	Labels        map[string]string `json:"labels,omitempty" doc:"Secret labels"`
}

type PodmanSecretCreateResponse struct {
	ID string `json:"id"`
}

type PodmanSecretUpdateRequest struct {
	Data          string            `json:"data" minLength:"1" doc:"New secret value; Podman replaces the whole secret"`
	Driver        string            `json:"driver,omitempty" doc:"Secret driver"`
	DriverOptions map[string]string `json:"driverOptions,omitempty" doc:"Secret driver options"`
	Labels        map[string]string `json:"labels,omitempty" doc:"Secret labels"`
}

type PodmanSystemdScope string

const (
	PodmanSystemdScopeSystem PodmanSystemdScope = "system"
	PodmanSystemdScopeUser   PodmanSystemdScope = "user"
)

type PodmanStatus struct {
	Detected             bool                 `json:"detected"`
	Version              string               `json:"version,omitempty"`
	APIVersion           string               `json:"apiVersion,omitempty"`
	Rootless             bool                 `json:"rootless"`
	QuadletsSupported    bool                 `json:"quadletsSupported"`
	QuadletAppsSupported bool                 `json:"quadletAppsSupported"`
	AutoUpdateMode       PodmanAutoUpdateMode `json:"autoUpdateMode"`
	SystemdAvailable     bool                 `json:"systemdAvailable"`
	SystemdScope         PodmanSystemdScope   `json:"systemdScope,omitempty"`
	SystemdReason        string               `json:"systemdReason,omitempty"`
}

type PodmanSystemdAvailability struct {
	Available bool               `json:"available"`
	Scope     PodmanSystemdScope `json:"scope,omitempty"`
	Reason    string             `json:"reason,omitempty"`
}

type PodmanUnitStatus struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	LoadState   string `json:"loadState"`
	ActiveState string `json:"activeState"`
	SubState    string `json:"subState"`
}

type PodmanVersionComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type PodmanVersion struct {
	Version       string                   `json:"version"`
	APIVersion    string                   `json:"apiVersion"`
	MinAPIVersion string                   `json:"minApiVersion,omitempty"`
	GitCommit     string                   `json:"gitCommit,omitempty"`
	GoVersion     string                   `json:"goVersion,omitempty"`
	Os            string                   `json:"os,omitempty"`
	Arch          string                   `json:"arch,omitempty"`
	KernelVersion string                   `json:"kernelVersion,omitempty"`
	Components    []PodmanVersionComponent `json:"components,omitempty"`
}

type PodmanInfoSecurity struct {
	Rootless           bool   `json:"rootless"`
	SELinuxEnabled     bool   `json:"selinuxEnabled"`
	AppArmorEnabled    bool   `json:"apparmorEnabled"`
	SeccompEnabled     bool   `json:"seccompEnabled"`
	SeccompProfilePath string `json:"seccompProfilePath,omitempty"`
	Capabilities       string `json:"capabilities,omitempty"`
}

type PodmanInfoDistribution struct {
	Distribution string `json:"distribution,omitempty"`
	Variant      string `json:"variant,omitempty"`
	Version      string `json:"version,omitempty"`
	Codename     string `json:"codename,omitempty"`
}

type PodmanInfoOCIRuntime struct {
	Name    string `json:"name,omitempty"`
	Package string `json:"package,omitempty"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

type PodmanInfoConmon struct {
	Package string `json:"package,omitempty"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

type PodmanInfoHelperBinary struct {
	Executable string `json:"executable,omitempty"`
	Package    string `json:"package,omitempty"`
	Version    string `json:"version,omitempty"`
}

type PodmanInfoNetworkDNS struct {
	Version string `json:"version,omitempty"`
	Package string `json:"package,omitempty"`
	Path    string `json:"path,omitempty"`
}

type PodmanInfoNetworkBackend struct {
	Backend        string               `json:"backend,omitempty"`
	Version        string               `json:"version,omitempty"`
	Package        string               `json:"package,omitempty"`
	Path           string               `json:"path,omitempty"`
	DNS            PodmanInfoNetworkDNS `json:"dns"`
	DefaultNetwork string               `json:"defaultNetwork,omitempty"`
}

type PodmanInfoIDMap struct {
	ContainerID int64 `json:"container_id"`
	HostID      int64 `json:"host_id"`
	Size        int64 `json:"size"`
}

type PodmanInfoIDMappings struct {
	UIDMap []PodmanInfoIDMap `json:"uidmap,omitempty"`
	GIDMap []PodmanInfoIDMap `json:"gidmap,omitempty"`
}

type PodmanInfoRemoteSocket struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
}

type PodmanInfoHost struct {
	Arch               string                   `json:"arch,omitempty"`
	BuildahVersion     string                   `json:"buildahVersion,omitempty"`
	CgroupManager      string                   `json:"cgroupManager,omitempty"`
	CgroupVersion      string                   `json:"cgroupVersion,omitempty"`
	CgroupControllers  []string                 `json:"cgroupControllers,omitempty"`
	Conmon             PodmanInfoConmon         `json:"conmon"`
	CPUs               int                      `json:"cpus"`
	DatabaseBackend    string                   `json:"databaseBackend,omitempty"`
	Distribution       PodmanInfoDistribution   `json:"distribution"`
	EventLogger        string                   `json:"eventLogger,omitempty"`
	Hostname           string                   `json:"hostname,omitempty"`
	IDMappings         PodmanInfoIDMappings     `json:"idMappings"`
	Kernel             string                   `json:"kernel,omitempty"`
	LogDriver          string                   `json:"logDriver,omitempty"`
	MemFree            int64                    `json:"memFree"`
	MemTotal           int64                    `json:"memTotal"`
	NetworkBackend     string                   `json:"networkBackend,omitempty"`
	NetworkBackendInfo PodmanInfoNetworkBackend `json:"networkBackendInfo"`
	OCIRuntime         PodmanInfoOCIRuntime     `json:"ociRuntime"`
	Os                 string                   `json:"os,omitempty"`
	RemoteSocket       PodmanInfoRemoteSocket   `json:"remoteSocket"`
	RootlessNetworkCmd string                   `json:"rootlessNetworkCmd,omitempty"`
	ServiceIsRemote    bool                     `json:"serviceIsRemote"`
	Security           PodmanInfoSecurity       `json:"security"`
	Slirp4netns        PodmanInfoHelperBinary   `json:"slirp4netns"`
	Pasta              PodmanInfoHelperBinary   `json:"pasta"`
	SwapFree           int64                    `json:"swapFree"`
	SwapTotal          int64                    `json:"swapTotal"`
	Uptime             string                   `json:"uptime,omitempty"`
}

type PodmanInfoContainerStore struct {
	Number  int `json:"number"`
	Paused  int `json:"paused"`
	Running int `json:"running"`
	Stopped int `json:"stopped"`
}

type PodmanInfoImageStore struct {
	Number int `json:"number"`
}

type PodmanInfoStore struct {
	ConfigFile      string                   `json:"configFile,omitempty"`
	ContainerStore  PodmanInfoContainerStore `json:"containerStore"`
	GraphDriverName string                   `json:"graphDriverName,omitempty"`
	GraphRoot       string                   `json:"graphRoot,omitempty"`
	GraphStatus     map[string]string        `json:"graphStatus,omitempty"`
	ImageStore      PodmanInfoImageStore     `json:"imageStore"`
	RunRoot         string                   `json:"runRoot,omitempty"`
	VolumePath      string                   `json:"volumePath,omitempty"`
}

type PodmanInfoRegistries struct {
	Search []string `json:"search,omitempty"`
}

type PodmanInfoPlugins struct {
	Network []string `json:"network,omitempty"`
	Volume  []string `json:"volume,omitempty"`
	Log     []string `json:"log,omitempty"`
}

type PodmanInfoVersion struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Version    string `json:"version,omitempty"`
	GoVersion  string `json:"goVersion,omitempty"`
	GitCommit  string `json:"gitCommit,omitempty"`
	BuiltTime  string `json:"builtTime,omitempty"`
	OsArch     string `json:"osArch,omitempty"`
}

type PodmanInfo struct {
	Host       PodmanInfoHost       `json:"host"`
	Store      PodmanInfoStore      `json:"store"`
	Registries PodmanInfoRegistries `json:"registries"`
	Plugins    PodmanInfoPlugins    `json:"plugins"`
	Version    PodmanInfoVersion    `json:"version"`
}

package types

import (
	"strings"
	"time"
)

type (
	PodmanAutoUpdateMode string

	PodmanAutoUpdateOptions struct {
		Rollback  *bool               `json:"rollback,omitempty"`
		TLSVerify *bool               `json:"tlsVerify,omitempty"`
		Filters   map[string][]string `json:"filters,omitempty"`
		Authfile  string              `json:"authfile,omitempty"`
		DryRun    bool                `json:"dryRun,omitempty"`
	}

	PodmanAutoUpdateReport struct {
		ContainerID   string `json:"containerId"`
		ContainerName string `json:"containerName"`
		ImageName     string `json:"imageName"`
		Policy        string `json:"policy"`
		SystemdUnit   string `json:"systemdUnit"`
		Updated       string `json:"updated"`
	}

	PodmanAutoUpdateResult struct {
		Unit    *PodmanUnitStatus        `json:"unit,omitempty"`
		Mode    PodmanAutoUpdateMode     `json:"mode"`
		Reports []PodmanAutoUpdateReport `json:"reports"`
		Errors  []string                 `json:"errors"`
	}

	KubeGenerateType string

	KubePlayRequest struct {
		Start    *bool    `json:"start,omitempty" doc:"Start the pods after creating them"`
		YAML     string   `json:"yaml" minLength:"1" doc:"Kubernetes YAML to play"`
		Networks []string `json:"networks,omitempty" doc:"Networks to attach the pods to"`
		Replace  bool     `json:"replace,omitempty" doc:"Replace existing pods and containers"`
	}

	KubeDownRequest struct {
		YAML  string `json:"yaml" minLength:"1" doc:"Kubernetes YAML to tear down"`
		Force bool   `json:"force,omitempty" doc:"Also remove volumes created by the YAML"`
	}

	KubeGenerateRequest struct {
		Type    KubeGenerateType `json:"type,omitempty" enum:"pod,service,deployment,daemonset,job" doc:"Kubernetes kind to generate"`
		Names   []string         `json:"names" minItems:"1" doc:"Pods, containers, or volumes to include"`
		Service bool             `json:"service,omitempty" doc:"Also generate a Kubernetes service"`
	}

	KubeGenerateResponse struct {
		YAML string `json:"yaml"`
	}

	KubePlayPod struct {
		ID              string   `json:"id"`
		Containers      []string `json:"containers"`
		InitContainers  []string `json:"initContainers"`
		Logs            []string `json:"logs"`
		ContainerErrors []string `json:"containerErrors"`
	}

	KubePlayVolume struct {
		Name string `json:"name"`
	}

	KubePlayReport struct {
		ExitCode           *int32           `json:"exitCode,omitempty"`
		ServiceContainerID string           `json:"serviceContainerId,omitempty"`
		Pods               []KubePlayPod    `json:"pods"`
		Volumes            []KubePlayVolume `json:"volumes"`
	}

	KubeDownPod struct {
		ID string `json:"id"`
	}

	KubeDownReport struct {
		StopReport []KubeDownPod `json:"stopReport"`
		RmReport   []KubeDownPod `json:"rmReport"`
	}

	PodContainer struct {
		ID           string `json:"id"`
		Names        string `json:"names"`
		Status       string `json:"status"`
		RestartCount uint   `json:"restartCount"`
	}

	Pod struct {
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

	PodPortBinding struct {
		HostIP   string `json:"hostIp,omitempty"`
		HostPort string `json:"hostPort"`
	}

	PodInfraConfig struct {
		PortBindings       map[string][]PodPortBinding `json:"portBindings,omitempty"`
		NetworkOptions     map[string][]string         `json:"networkOptions,omitempty"`
		StaticIP           string                      `json:"staticIp,omitempty"`
		StaticMAC          string                      `json:"staticMac,omitempty"`
		HostsFile          string                      `json:"hostsFile,omitempty"`
		PidNS              string                      `json:"pidNs,omitempty"`
		UserNS             string                      `json:"userNs,omitempty"`
		UtsNS              string                      `json:"utsNs,omitempty"`
		DNSServer          []string                    `json:"dnsServer,omitempty"`
		DNSSearch          []string                    `json:"dnsSearch,omitempty"`
		DNSOption          []string                    `json:"dnsOption,omitempty"`
		HostAdd            []string                    `json:"hostAdd,omitempty"`
		Networks           []string                    `json:"networks,omitempty"`
		HostNetwork        bool                        `json:"hostNetwork"`
		NoManageResolvConf bool                        `json:"noManageResolvConf"`
		NoManageHostname   bool                        `json:"noManageHostname"`
		NoManageHosts      bool                        `json:"noManageHosts"`
	}

	PodInspectContainer struct {
		Created  time.Time        `json:"created"`
		ID       string           `json:"id"`
		Name     string           `json:"name"`
		State    string           `json:"state"`
		Image    string           `json:"image,omitempty"`
		Status   string           `json:"status,omitempty"`
		Ports    []PodPortMapping `json:"ports,omitempty"`
		ExitCode int32            `json:"exitCode"`
		IsInfra  bool             `json:"isInfra"`
	}

	PodMount struct {
		Type        string   `json:"type"`
		Name        string   `json:"name,omitempty"`
		Source      string   `json:"source,omitempty"`
		Destination string   `json:"destination"`
		Driver      string   `json:"driver,omitempty"`
		Mode        string   `json:"mode,omitempty"`
		Propagation string   `json:"propagation,omitempty"`
		SubPath     string   `json:"subPath,omitempty"`
		Options     []string `json:"options,omitempty"`
		RW          bool     `json:"rw"`
	}

	PodDevice struct {
		PathOnHost        string `json:"pathOnHost"`
		PathInContainer   string `json:"pathInContainer"`
		CgroupPermissions string `json:"cgroupPermissions,omitempty"`
	}

	PodWeightDevice struct {
		Path   string `json:"path"`
		Weight uint16 `json:"weight"`
	}

	PodThrottleDevice struct {
		Path string `json:"path"`
		Rate uint64 `json:"rate"`
	}

	PodInspect struct {
		Created            time.Time             `json:"created"`
		Labels             map[string]string     `json:"labels,omitempty"`
		InfraConfig        *PodInfraConfig       `json:"infraConfig,omitempty"`
		ID                 string                `json:"id"`
		Name               string                `json:"name"`
		Namespace          string                `json:"namespace,omitempty"`
		State              string                `json:"state"`
		ExitPolicy         string                `json:"exitPolicy,omitempty"`
		RestartPolicy      string                `json:"restartPolicy,omitempty"`
		Hostname           string                `json:"hostname,omitempty"`
		CgroupParent       string                `json:"cgroupParent,omitempty"`
		CgroupPath         string                `json:"cgroupPath,omitempty"`
		InfraContainerID   string                `json:"infraContainerId,omitempty"`
		CPUSetCPUs         string                `json:"cpusetCpus,omitempty"`
		CPUSetMems         string                `json:"cpusetMems,omitempty"`
		CreateCommand      []string              `json:"createCommand,omitempty"`
		SharedNamespaces   []string              `json:"sharedNamespaces,omitempty"`
		Containers         []PodInspectContainer `json:"containers"`
		BlkioWeightDevices []PodWeightDevice     `json:"blkioWeightDevices,omitempty"`
		DeviceReadBps      []PodThrottleDevice   `json:"deviceReadBps,omitempty"`
		DeviceWriteBps     []PodThrottleDevice   `json:"deviceWriteBps,omitempty"`
		Mounts             []PodMount            `json:"mounts,omitempty"`
		Devices            []PodDevice           `json:"devices,omitempty"`
		VolumesFrom        []string              `json:"volumesFrom,omitempty"`
		SecurityOpts       []string              `json:"securityOpts,omitempty"`
		NumContainers      uint                  `json:"numContainers"`
		CPUPeriod          uint64                `json:"cpuPeriod,omitempty"`
		CPUQuota           int64                 `json:"cpuQuota,omitempty"`
		CPUShares          uint64                `json:"cpuShares,omitempty"`
		MemoryLimit        uint64                `json:"memoryLimit,omitempty"`
		MemorySwap         uint64                `json:"memorySwap,omitempty"`
		BlkioWeight        uint64                `json:"blkioWeight,omitempty"`
		CreateCgroup       bool                  `json:"createCgroup"`
		CreateInfra        bool                  `json:"createInfra"`
	}

	PodPortMapping struct {
		HostIP        string `json:"hostIp,omitempty" doc:"Host IP to bind"`
		Protocol      string `json:"protocol,omitempty" doc:"tcp, udp, or sctp"`
		ContainerPort uint16 `json:"containerPort" minimum:"1" doc:"Port inside the pod"`
		HostPort      uint16 `json:"hostPort,omitempty" doc:"Port on the host"`
		Range         uint16 `json:"range,omitempty" doc:"Number of consecutive ports to map"`
	}

	PodNetwork struct {
		Name          string   `json:"name" minLength:"1" doc:"Network to join"`
		StaticMAC     string   `json:"staticMac,omitempty" doc:"Static MAC address"`
		InterfaceName string   `json:"interfaceName,omitempty" doc:"Interface name inside the pod"`
		Aliases       []string `json:"aliases,omitempty" doc:"Network-scoped aliases"`
		StaticIPs     []string `json:"staticIps,omitempty" doc:"Static IPv4 or IPv6 addresses"`
	}

	PodVolume struct {
		Source      string   `json:"source" minLength:"1" doc:"Absolute host path or named volume"`
		Destination string   `json:"destination" minLength:"1" doc:"Path inside the pod"`
		Options     []string `json:"options,omitempty" doc:"Mount options such as ro, z, Z, or U"`
	}

	PodDeviceLimit struct {
		Device string `json:"device" minLength:"1" doc:"Block device as MAJOR:MINOR"`
		Value  string `json:"value" minLength:"1" doc:"Weight (10-1000) for weight limits, or a rate such as 1mb for throughput limits"`
	}

	PodCreateRequest struct {
		Labels             map[string]string `json:"labels,omitempty" doc:"Pod labels"`
		RestartRetries     *uint             `json:"restartRetries,omitempty" doc:"Restart attempts for the on-failure policy"`
		ShareParent        *bool             `json:"shareParent,omitempty" doc:"Use the pod's cgroup as the cgroup parent of its containers"`
		Sysctl             map[string]string `json:"sysctl,omitempty" doc:"Namespaced kernel parameters"`
		Name               string            `json:"name" minLength:"1" doc:"Pod name"`
		Hostname           string            `json:"hostname,omitempty" doc:"Pod hostname"`
		ExitPolicy         string            `json:"exitPolicy,omitempty" enum:"continue,stop" doc:"Behaviour when the last container exits"`
		RestartPolicy      string            `json:"restartPolicy,omitempty" enum:"no,always,on-failure,unless-stopped" doc:"Restart policy for containers in the pod"`
		InfraImage         string            `json:"infraImage,omitempty" doc:"Image for the infra container"`
		InfraName          string            `json:"infraName,omitempty" doc:"Name of the infra container"`
		InfraConmonPidFile string            `json:"infraConmonPidFile,omitempty" doc:"Path that receives the infra container's conmon PID"`
		Pid                string            `json:"pid,omitempty" doc:"PID namespace: host, private, ns:<path>, or container:<id>"`
		Ipc                string            `json:"ipc,omitempty" doc:"IPC namespace: host, private, shareable, none, ns:<path>, or container:<id>"`
		Uts                string            `json:"uts,omitempty" doc:"UTS namespace: host, private, ns:<path>, or container:<id>"`
		Userns             string            `json:"userns,omitempty" doc:"User namespace, for example auto, keep-id, keep-id:uid=1000, host, or ns:<path>"`
		NetworkMode        string            `json:"networkMode,omitempty" doc:"bridge, host, none, private, slirp4netns, pasta, or ns:<path>"`
		HostsFile          string            `json:"hostsFile,omitempty" doc:"Base file for /etc/hosts, or image or none"`
		CgroupParent       string            `json:"cgroupParent,omitempty" doc:"Parent cgroup of the pod"`
		CPUSetCPUs         string            `json:"cpusetCpus,omitempty" doc:"CPUs allowed to run the pod, for example 0-3"`
		CPUSetMems         string            `json:"cpusetMems,omitempty" doc:"Memory nodes allowed to run the pod, for example 0,1"`
		Memory             string            `json:"memory,omitempty" doc:"Memory limit such as 512m or 2g"`
		MemorySwap         string            `json:"memorySwap,omitempty" doc:"Memory plus swap limit such as 1g, or -1 for unlimited swap"`
		ShmSize            string            `json:"shmSize,omitempty" doc:"Size of /dev/shm such as 64m"`
		ShmSizeSystemd     string            `json:"shmSizeSystemd,omitempty" doc:"Size of systemd tmpfs mounts such as 64m"`
		InfraCommand       []string          `json:"infraCommand,omitempty" doc:"Entrypoint of the infra container"`
		SharedNamespaces   []string          `json:"sharedNamespaces,omitempty" doc:"Namespaces shared between pod members"`
		UIDMap             []string          `json:"uidMap,omitempty" doc:"UID mappings as CONTAINER:HOST:SIZE"`
		GIDMap             []string          `json:"gidMap,omitempty" doc:"GID mappings as CONTAINER:HOST:SIZE"`
		NetworkOptions     []string          `json:"networkOptions,omitempty" doc:"Options for the slirp4netns or pasta network mode"`
		Networks           []PodNetwork      `json:"networks,omitempty" doc:"Networks to join"`
		PortMappings       []PodPortMapping  `json:"portMappings,omitempty" doc:"Published ports"`
		DNSServers         []string          `json:"dnsServers,omitempty" doc:"DNS servers"`
		DNSSearch          []string          `json:"dnsSearch,omitempty" doc:"DNS search domains"`
		DNSOptions         []string          `json:"dnsOptions,omitempty" doc:"DNS resolver options"`
		AddHosts           []string          `json:"addHosts,omitempty" doc:"Extra /etc/hosts entries as HOST:IP"`
		BlkioWeightDevices []PodDeviceLimit  `json:"blkioWeightDevices,omitempty" doc:"Per-device block IO weights"`
		DeviceReadBps      []PodDeviceLimit  `json:"deviceReadBps,omitempty" doc:"Per-device read rate limits"`
		DeviceWriteBps     []PodDeviceLimit  `json:"deviceWriteBps,omitempty" doc:"Per-device write rate limits"`
		Devices            []string          `json:"devices,omitempty" doc:"Host devices as HOST[:CONTAINER[:PERMISSIONS]]"`
		GPUs               []string          `json:"gpus,omitempty" doc:"NVIDIA GPUs to add through CDI, or all"`
		Volumes            []PodVolume       `json:"volumes,omitempty" doc:"Bind mounts and named volumes"`
		VolumesFrom        []string          `json:"volumesFrom,omitempty" doc:"Containers to mount volumes from, as CONTAINER[:OPTIONS]"`
		SecurityOpt        []string          `json:"securityOpt,omitempty" doc:"Security options such as label=disable"`
		CPUs               float64           `json:"cpus,omitempty" minimum:"0" doc:"Number of CPUs"`
		CPUShares          uint64            `json:"cpuShares,omitempty" doc:"Relative CPU weight"`
		BlkioWeight        uint16            `json:"blkioWeight,omitempty" minimum:"10" maximum:"1000" doc:"Relative block IO weight"`
		NoInfra            bool              `json:"noInfra,omitempty" doc:"Create the pod without an infra container"`
		NoManageResolvConf bool              `json:"noManageResolvConf,omitempty" doc:"Use the image's /etc/resolv.conf"`
		NoHosts            bool              `json:"noHosts,omitempty" doc:"Use the image's /etc/hosts"`
		NoHostname         bool              `json:"noHostname,omitempty" doc:"Use the image's /etc/hostname"`
	}

	PodActionRequest struct {
		Timeout *int   `json:"timeout,omitempty" doc:"Seconds to wait before killing the pod on stop"`
		Signal  string `json:"signal,omitempty" doc:"Signal sent on kill"`
	}

	PodCreateResponse struct {
		ID string `json:"id"`
	}

	PodStats struct {
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

	PodTop struct {
		Titles    []string   `json:"titles"`
		Processes [][]string `json:"processes"`
	}

	PodPruneReport struct {
		ID string `json:"id"`
	}

	PodmanContainer struct {
		Created   time.Time         `json:"created"`
		Labels    map[string]string `json:"labels,omitempty"`
		ID        string            `json:"id"`
		Image     string            `json:"image"`
		ImageID   string            `json:"imageId,omitempty"`
		State     string            `json:"state"`
		Status    string            `json:"status,omitempty"`
		Pod       string            `json:"pod,omitempty"`
		PodName   string            `json:"podName,omitempty"`
		Names     []string          `json:"names"`
		Ports     []PodPortMapping  `json:"ports,omitempty"`
		StartedAt int64             `json:"startedAt,omitempty"`
		ExitCode  int32             `json:"exitCode"`
		Exited    bool              `json:"exited"`
		IsInfra   bool              `json:"isInfra"`
	}

	QuadletType string

	Quadlet struct {
		Name     string `json:"name"`
		UnitName string `json:"unitName,omitempty"`
		Path     string `json:"path"`
		Status   string `json:"status"`
		App      string `json:"app,omitempty"`
	}

	QuadletFile struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}

	QuadletInstallRequest struct {
		Name       string        `json:"name" minLength:"1" doc:"Quadlet file name including its extension"`
		Content    string        `json:"content" minLength:"1" doc:"Quadlet file contents"`
		App        string        `json:"app,omitempty" doc:"Application the quadlet belongs to"`
		ExtraFiles []QuadletFile `json:"extraFiles,omitempty" doc:"Additional files installed beside the quadlet"`
	}

	QuadletUpdateRequest struct {
		Content    string        `json:"content" minLength:"1" doc:"Quadlet file contents"`
		ExtraFiles []QuadletFile `json:"extraFiles,omitempty" doc:"Additional files installed beside the quadlet"`
	}

	QuadletDetail struct {
		UnitStatus *PodmanUnitStatus `json:"unitStatus,omitempty"`
		EnvironmentTag

		Name       string            `json:"name"`
		UnitName   string            `json:"unitName"`
		Path       string            `json:"path"`
		Status     string            `json:"status"`
		App        string            `json:"app,omitempty"`
		Type       QuadletType       `json:"type"`
		Containers []PodmanContainer `json:"containers,omitempty"`
		Template   bool              `json:"template"`
	}

	QuadletInstallReport struct {
		InstalledQuadlets map[string]string `json:"installedQuadlets"`
		QuadletErrors     map[string]string `json:"quadletErrors"`
	}

	QuadletRemoveReport struct {
		Errors  map[string]string `json:"errors"`
		Removed []string          `json:"removed"`
	}

	PodmanSecretDriver struct {
		Options map[string]string `json:"options,omitempty"`
		Name    string            `json:"name"`
	}

	PodmanSecretSpec struct {
		Driver PodmanSecretDriver `json:"driver"`
		Labels map[string]string  `json:"labels,omitempty"`
		Name   string             `json:"name"`
	}

	PodmanSecret struct {
		Spec      PodmanSecretSpec `json:"spec"`
		CreatedAt time.Time        `json:"createdAt"`
		UpdatedAt time.Time        `json:"updatedAt"`
		EnvironmentTag

		ID string `json:"id"`
	}

	PodmanSecretCreateRequest struct {
		DriverOptions map[string]string `json:"driverOptions,omitempty" doc:"Secret driver options"`
		Labels        map[string]string `json:"labels,omitempty" doc:"Secret labels"`
		Name          string            `json:"name" minLength:"1" doc:"Secret name"`
		Data          string            `json:"data" minLength:"1" doc:"Secret value"`
		Driver        string            `json:"driver,omitempty" doc:"Secret driver"`
	}

	PodmanSecretCreateResponse struct {
		ID string `json:"id"`
	}

	PodmanSecretUpdateRequest struct {
		DriverOptions map[string]string `json:"driverOptions,omitempty" doc:"Secret driver options"`
		Labels        map[string]string `json:"labels,omitempty" doc:"Secret labels"`
		Data          string            `json:"data" minLength:"1" doc:"New secret value; Podman replaces the whole secret"`
		Driver        string            `json:"driver,omitempty" doc:"Secret driver"`
	}

	PodmanSystemdScope string

	PodmanStatus struct {
		Version              string               `json:"version,omitempty"`
		APIVersion           string               `json:"apiVersion,omitempty"`
		AutoUpdateMode       PodmanAutoUpdateMode `json:"autoUpdateMode"`
		SystemdScope         PodmanSystemdScope   `json:"systemdScope,omitempty"`
		SystemdReason        string               `json:"systemdReason,omitempty"`
		PodCount             int                  `json:"podCount" doc:"Number of Podman pods"`
		Detected             bool                 `json:"detected"`
		Rootless             bool                 `json:"rootless"`
		QuadletsSupported    bool                 `json:"quadletsSupported"`
		QuadletAppsSupported bool                 `json:"quadletAppsSupported"`
		SystemdAvailable     bool                 `json:"systemdAvailable"`
	}

	PodmanSystemdAvailability struct {
		Scope     PodmanSystemdScope `json:"scope,omitempty"`
		Reason    string             `json:"reason,omitempty"`
		Available bool               `json:"available"`
	}

	PodmanUnitStatus struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		LoadState   string `json:"loadState"`
		ActiveState string `json:"activeState"`
		SubState    string `json:"subState"`
	}

	PodmanVersionComponent struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	PodmanVersion struct {
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

	PodmanInfoSecurity struct {
		SeccompProfilePath string `json:"seccompProfilePath,omitempty"`
		Capabilities       string `json:"capabilities,omitempty"`
		Rootless           bool   `json:"rootless"`
		SELinuxEnabled     bool   `json:"selinuxEnabled"`
		AppArmorEnabled    bool   `json:"apparmorEnabled"`
		SeccompEnabled     bool   `json:"seccompEnabled"`
	}

	PodmanInfoDistribution struct {
		Distribution string `json:"distribution,omitempty"`
		Variant      string `json:"variant,omitempty"`
		Version      string `json:"version,omitempty"`
		Codename     string `json:"codename,omitempty"`
	}

	PodmanInfoOCIRuntime struct {
		Name    string `json:"name,omitempty"`
		Package string `json:"package,omitempty"`
		Path    string `json:"path,omitempty"`
		Version string `json:"version,omitempty"`
	}

	PodmanInfoConmon struct {
		Package string `json:"package,omitempty"`
		Path    string `json:"path,omitempty"`
		Version string `json:"version,omitempty"`
	}

	PodmanInfoHelperBinary struct {
		Executable string `json:"executable,omitempty"`
		Package    string `json:"package,omitempty"`
		Version    string `json:"version,omitempty"`
	}

	PodmanInfoNetworkDNS struct {
		Version string `json:"version,omitempty"`
		Package string `json:"package,omitempty"`
		Path    string `json:"path,omitempty"`
	}

	PodmanInfoNetworkBackend struct {
		Backend        string               `json:"backend,omitempty"`
		Version        string               `json:"version,omitempty"`
		Package        string               `json:"package,omitempty"`
		Path           string               `json:"path,omitempty"`
		DNS            PodmanInfoNetworkDNS `json:"dns"`
		DefaultNetwork string               `json:"defaultNetwork,omitempty"`
	}

	PodmanInfoIDMap struct {
		ContainerID int64 `json:"container_id"`
		HostID      int64 `json:"host_id"`
		Size        int64 `json:"size"`
	}

	PodmanInfoIDMappings struct {
		UIDMap []PodmanInfoIDMap `json:"uidmap,omitempty"`
		GIDMap []PodmanInfoIDMap `json:"gidmap,omitempty"`
	}

	PodmanInfoRemoteSocket struct {
		Path   string `json:"path,omitempty"`
		Exists bool   `json:"exists"`
	}

	PodmanInfoHost struct {
		NetworkBackendInfo PodmanInfoNetworkBackend `json:"networkBackendInfo"`
		Distribution       PodmanInfoDistribution   `json:"distribution"`
		OCIRuntime         PodmanInfoOCIRuntime     `json:"ociRuntime"`
		Conmon             PodmanInfoConmon         `json:"conmon"`
		Slirp4netns        PodmanInfoHelperBinary   `json:"slirp4netns"`
		Pasta              PodmanInfoHelperBinary   `json:"pasta"`
		Security           PodmanInfoSecurity       `json:"security"`
		Arch               string                   `json:"arch,omitempty"`
		BuildahVersion     string                   `json:"buildahVersion,omitempty"`
		CgroupManager      string                   `json:"cgroupManager,omitempty"`
		CgroupVersion      string                   `json:"cgroupVersion,omitempty"`
		DatabaseBackend    string                   `json:"databaseBackend,omitempty"`
		EventLogger        string                   `json:"eventLogger,omitempty"`
		Hostname           string                   `json:"hostname,omitempty"`
		Kernel             string                   `json:"kernel,omitempty"`
		LogDriver          string                   `json:"logDriver,omitempty"`
		NetworkBackend     string                   `json:"networkBackend,omitempty"`
		Os                 string                   `json:"os,omitempty"`
		RootlessNetworkCmd string                   `json:"rootlessNetworkCmd,omitempty"`
		Uptime             string                   `json:"uptime,omitempty"`
		IDMappings         PodmanInfoIDMappings     `json:"idMappings"`
		CgroupControllers  []string                 `json:"cgroupControllers,omitempty"`
		RemoteSocket       PodmanInfoRemoteSocket   `json:"remoteSocket"`
		CPUs               int                      `json:"cpus"`
		MemFree            int64                    `json:"memFree"`
		MemTotal           int64                    `json:"memTotal"`
		SwapFree           int64                    `json:"swapFree"`
		SwapTotal          int64                    `json:"swapTotal"`
		ServiceIsRemote    bool                     `json:"serviceIsRemote"`
	}

	PodmanInfoContainerStore struct {
		Number  int `json:"number"`
		Paused  int `json:"paused"`
		Running int `json:"running"`
		Stopped int `json:"stopped"`
	}

	PodmanInfoImageStore struct {
		Number int `json:"number"`
	}

	PodmanInfoStore struct {
		GraphStatus     map[string]string        `json:"graphStatus,omitempty"`
		ConfigFile      string                   `json:"configFile,omitempty"`
		GraphDriverName string                   `json:"graphDriverName,omitempty"`
		GraphRoot       string                   `json:"graphRoot,omitempty"`
		RunRoot         string                   `json:"runRoot,omitempty"`
		VolumePath      string                   `json:"volumePath,omitempty"`
		ContainerStore  PodmanInfoContainerStore `json:"containerStore"`
		ImageStore      PodmanInfoImageStore     `json:"imageStore"`
	}

	PodmanInfoRegistries struct {
		Search []string `json:"search,omitempty"`
	}

	PodmanInfoPlugins struct {
		Network []string `json:"network,omitempty"`
		Volume  []string `json:"volume,omitempty"`
		Log     []string `json:"log,omitempty"`
	}

	PodmanInfoVersion struct {
		APIVersion string `json:"apiVersion,omitempty"`
		Version    string `json:"version,omitempty"`
		GoVersion  string `json:"goVersion,omitempty"`
		GitCommit  string `json:"gitCommit,omitempty"`
		BuiltTime  string `json:"builtTime,omitempty"`
		OsArch     string `json:"osArch,omitempty"`
	}

	PodmanInfo struct {
		Version    PodmanInfoVersion    `json:"version"`
		Plugins    PodmanInfoPlugins    `json:"plugins"`
		Registries PodmanInfoRegistries `json:"registries"`
		Store      PodmanInfoStore      `json:"store"`
		Host       PodmanInfoHost       `json:"host"`
	}
)

const (
	PodmanAutoUpdateModeREST        PodmanAutoUpdateMode = "rest"
	PodmanAutoUpdateModeSystemd     PodmanAutoUpdateMode = "systemd"
	PodmanAutoUpdateModeUnavailable PodmanAutoUpdateMode = "unavailable"

	KubeGenerateTypePod        KubeGenerateType = "pod"
	KubeGenerateTypeService    KubeGenerateType = "service"
	KubeGenerateTypeDeployment KubeGenerateType = "deployment"
	KubeGenerateTypeDaemonSet  KubeGenerateType = "daemonset"
	KubeGenerateTypeJob        KubeGenerateType = "job"

	PodmanSystemdUnitLabel = "PODMAN_SYSTEMD_UNIT"

	QuadletTypeContainer QuadletType = "container"
	QuadletTypePod       QuadletType = "pod"
	QuadletTypeKube      QuadletType = "kube"
	QuadletTypeNetwork   QuadletType = "network"
	QuadletTypeVolume    QuadletType = "volume"
	QuadletTypeImage     QuadletType = "image"
	QuadletTypeBuild     QuadletType = "build"
	QuadletTypeArtifact  QuadletType = "artifact"

	PodmanSystemdScopeSystem PodmanSystemdScope = "system"
	PodmanSystemdScopeUser   PodmanSystemdScope = "user"
)

func QuadletUnit(labels map[string]string) string {
	unit := labels[PodmanSystemdUnitLabel]
	if strings.HasPrefix(unit, "podman-compose@") {
		return ""
	}
	return unit
}

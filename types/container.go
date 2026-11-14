package types

import (
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

type (
	ContainerSetAutoUpdateRequest struct {
		Enabled bool `json:"enabled" doc:"Whether auto-update is enabled for this container"`
	}

	ContainerRestartPolicyCreate struct {
		Name              string `json:"name,omitempty"`
		MaximumRetryCount int    `json:"maximumRetryCount,omitempty"`
	}

	ContainerPortBindingCreate struct {
		HostIP   string `json:"hostIp,omitempty"`
		HostPort string `json:"hostPort,omitempty"`
	}

	ContainerHostConfigCreate struct {
		PortBindings    map[string][]ContainerPortBindingCreate `json:"portBindings,omitempty"`
		RestartPolicy   *ContainerRestartPolicyCreate           `json:"restartPolicy,omitempty"`
		Privileged      *bool                                   `json:"privileged,omitempty"`
		AutoRemove      *bool                                   `json:"autoRemove,omitempty"`
		ReadonlyRootfs  *bool                                   `json:"readonlyRootfs,omitempty"`
		PublishAllPorts *bool                                   `json:"publishAllPorts,omitempty"`
		NetworkMode     string                                  `json:"networkMode,omitempty"`
		Binds           []string                                `json:"binds,omitempty"`
		CapAdd          []string                                `json:"capAdd,omitempty"`
		CapDrop         []string                                `json:"capDrop,omitempty"`
		Mounts          []ContainerMountCreate                  `json:"mounts,omitempty"`

		Memory int64 `json:"memory,omitempty"`

		MemorySwap int64 `json:"memorySwap,omitempty"`
		NanoCPUs   int64 `json:"nanoCpus,omitempty"`
		CPUShares  int64 `json:"cpuShares,omitempty"`
	}

	ContainerEndpointSettingsCreate struct {
		IPv4Address string   `json:"ipv4Address,omitempty"`
		IPv6Address string   `json:"ipv6Address,omitempty"`
		Aliases     []string `json:"aliases,omitempty"`
	}

	ContainerMountCreate struct {
		Type     string `json:"type"`
		Source   string `json:"source"`
		Target   string `json:"target"`
		ReadOnly bool   `json:"readOnly,omitempty"`
	}

	ContainerHealthcheckCreate struct {
		Test []string `json:"test,omitempty"`

		Interval int64 `json:"interval,omitempty"`

		Timeout int64 `json:"timeout,omitempty"`

		StartPeriod int64 `json:"startPeriod,omitempty"`

		StartInterval int64 `json:"startInterval,omitempty"`
		Retries       int   `json:"retries,omitempty"`
	}

	ContainerNetworkingConfigCreate struct {
		EndpointsConfig map[string]ContainerEndpointSettingsCreate `json:"endpointsConfig,omitempty"`
	}

	Container struct {
		Labels       map[string]string   `json:"labels,omitempty" doc:"Container labels; Arcane also sets com.arcane.created"`
		ExposedPorts map[string]struct{} `json:"exposedPorts,omitempty" doc:"Ports to expose unpublished, keyed port or port/protocol (tcp by default), each an empty object"`

		Healthcheck      *ContainerHealthcheckCreate      `json:"healthcheck,omitempty" doc:"Overrides the image healthcheck; durations in seconds, test NONE disables it"`
		HostConfig       *ContainerHostConfigCreate       `json:"hostConfig,omitempty"`
		NetworkingConfig *ContainerNetworkingConfigCreate `json:"networkingConfig,omitempty" doc:"Networks to join keyed by name, with optional static addresses and aliases"`
		Ports            map[string]string                `json:"ports,omitempty" doc:"Published TCP ports, container port to host port; added to hostConfig.portBindings"`
		Name             string                           `json:"name" required:"false" doc:"Container name; must not be empty"`
		Image            string                           `json:"image" required:"false" doc:"Image to run, pulled when missing locally; must not be empty"`
		WorkingDir       string                           `json:"workingDir,omitempty"`
		User             string                           `json:"user,omitempty" doc:"User, and optionally group, that runs the process, by name or ID"`
		Hostname         string                           `json:"hostname,omitempty" doc:"Must be a valid RFC 1123 hostname"`
		Domainname       string                           `json:"domainname,omitempty"`
		RestartPolicy    string                           `json:"restartPolicy,omitempty" doc:"no, always, unless-stopped or on-failure; ignored when hostConfig.restartPolicy is set"`
		Command          []string                         `json:"command,omitempty" doc:"Overrides the image CMD"`
		Cmd              []string                         `json:"cmd,omitempty" doc:"Alias of command, used when command is empty"`
		Entrypoint       []string                         `json:"entrypoint,omitempty" doc:"Overrides the image ENTRYPOINT"`
		Environment      []string                         `json:"environment,omitempty" doc:"Environment variables as KEY=VALUE"`
		Env              []string                         `json:"env,omitempty" doc:"Alias of environment, used when environment is empty"`
		Volumes          []string                         `json:"volumes,omitempty" doc:"Bind and volume specs as source:target[:options]; ignored when hostConfig.binds is non-empty"`
		Networks         []string                         `json:"networks,omitempty" doc:"Networks to join by name; ignored when networkingConfig lists endpoints"`
		Credentials      []ContainerRegistryCredential    `json:"credentials,omitempty" writeOnly:"true" doc:"Registry credentials for pulling a missing image"`

		Memory          int64   `json:"memory,omitempty" doc:"Memory limit in bytes; above 0 it replaces hostConfig.memory"`
		CPUs            float64 `json:"cpus,omitempty" doc:"CPU limit in CPUs, fractions allowed; above 0 it replaces hostConfig.nanoCpus"`
		AttachStdout    bool    `json:"attachStdout,omitempty"`
		AttachStderr    bool    `json:"attachStderr,omitempty"`
		AttachStdin     bool    `json:"attachStdin,omitempty"`
		Tty             bool    `json:"tty,omitempty" doc:"Allocate a pseudo-TTY"`
		OpenStdin       bool    `json:"openStdin,omitempty"`
		StdinOnce       bool    `json:"stdinOnce,omitempty" doc:"Close stdin after the first attached client disconnects"`
		NetworkDisabled bool    `json:"networkDisabled,omitempty"`
		Privileged      bool    `json:"privileged,omitempty" doc:"Privileged mode; hostConfig.privileged wins when set"`
		AutoRemove      bool    `json:"autoRemove,omitempty" doc:"Remove the container when it exits; hostConfig.autoRemove wins when set"`
	}

	ContainerCommitRequest struct {
		Repository string   `json:"repository,omitempty" doc:"Target image repository"`
		Tag        string   `json:"tag,omitempty" doc:"Target image tag"`
		Comment    string   `json:"comment,omitempty" doc:"Commit comment"`
		Author     string   `json:"author,omitempty" doc:"Commit author"`
		Changes    []string `json:"changes,omitempty" doc:"Dockerfile changes to apply"`
		NoPause    bool     `json:"noPause,omitempty" doc:"Do not pause the container during commit"`
	}

	ContainerCommitResult struct {
		ID string `json:"id"`
	}

	ContainerGenerateComposeRequest struct {
		ContainerIDs []string `json:"containerIds" minItems:"1" doc:"IDs of the containers to convert"`
	}

	ContainerGenerateComposeResponse struct {
		ComposeContent string `json:"composeContent"`
	}

	ContainerStatusCounts struct {
		RunningContainers int `json:"runningContainers"`
		StoppedContainers int `json:"stoppedContainers"`
		TotalContainers   int `json:"totalContainers"`

		UnhealthyContainers int `json:"unhealthyContainers" doc:"Containers whose healthcheck reports unhealthy"`

		UpdatesAvailable int `json:"updatesAvailable" doc:"Standalone containers with a pending image update"`
	}

	ContainerActionResult struct {
		ActivityID *string  `json:"activityId,omitempty"`
		Started    []string `json:"started,omitempty"`
		Stopped    []string `json:"stopped,omitempty"`
		Failed     []string `json:"failed,omitempty"`
		Errors     []string `json:"errors,omitempty"`
		Success    bool     `json:"success"`
	}

	ContainerPort struct {
		IP          string `json:"ip,omitempty"`
		Type        string `json:"type"`
		PrivatePort int    `json:"privatePort"`
		PublicPort  int    `json:"publicPort,omitempty"`
	}

	ContainerMount struct {
		Type        string `json:"type"`
		Name        string `json:"name,omitempty"`
		Source      string `json:"source,omitempty"`
		Destination string `json:"destination"`
		Driver      string `json:"driver,omitempty"`
		Mode        string `json:"mode,omitempty"`
		Propagation string `json:"propagation,omitempty"`
		RW          bool   `json:"rw,omitempty"`
	}

	ContainerNetworkEndpoint struct {
		IPAMConfig          any               `json:"ipamConfig,omitempty"`
		DriverOpts          map[string]string `json:"driverOpts,omitempty"`
		MacAddress          string            `json:"macAddress,omitempty"`
		NetworkID           string            `json:"networkId,omitempty"`
		EndpointID          string            `json:"endpointId,omitempty"`
		Gateway             string            `json:"gateway,omitempty"`
		IPAddress           string            `json:"ipAddress,omitempty"`
		IPv6Gateway         string            `json:"ipv6Gateway,omitempty"`
		GlobalIPv6Address   string            `json:"globalIPv6Address,omitempty"`
		Links               []string          `json:"links,omitempty"`
		Aliases             []string          `json:"aliases,omitempty"`
		DNSNames            []string          `json:"dnsNames,omitempty"`
		GwPriority          int               `json:"gwPriority,omitempty"`
		IPPrefixLen         int               `json:"ipPrefixLen,omitempty"`
		GlobalIPv6PrefixLen int               `json:"globalIPv6PrefixLen,omitempty"`
	}

	ContainerNetworkSettings struct {
		Networks map[string]ContainerNetworkEndpoint `json:"networks"`
	}

	ContainerState struct {
		Health     *ContainerHealth `json:"health,omitempty"`
		Status     string           `json:"status"`
		StartedAt  string           `json:"startedAt,omitempty"`
		FinishedAt string           `json:"finishedAt,omitempty"`
		ExitCode   int              `json:"exitCode,omitempty"`
		Running    bool             `json:"running"`
	}

	ContainerHealthLogEntry struct {
		Start    string `json:"start,omitempty"`
		End      string `json:"end,omitempty"`
		Output   string `json:"output,omitempty"`
		ExitCode int    `json:"exitCode"`
	}

	ContainerHealth struct {
		Status        string                    `json:"status"`
		Log           []ContainerHealthLogEntry `json:"log,omitempty"`
		FailingStreak int                       `json:"failingStreak"`
	}

	ContainerHealthcheck struct {
		Test []string `json:"test,omitempty"`

		Interval int64 `json:"interval,omitempty"`

		Timeout int64 `json:"timeout,omitempty"`

		StartPeriod int64 `json:"startPeriod,omitempty"`

		StartInterval int64 `json:"startInterval,omitempty"`
		Retries       int   `json:"retries,omitempty"`
	}

	ContainerConfig struct {
		Healthcheck *ContainerHealthcheck `json:"healthcheck,omitempty"`
		WorkingDir  string                `json:"workingDir,omitempty"`
		User        string                `json:"user,omitempty"`
		Hostname    string                `json:"hostname,omitempty"`
		Env         []string              `json:"env,omitempty"`
		Cmd         []string              `json:"cmd,omitempty"`
		Entrypoint  []string              `json:"entrypoint,omitempty"`
	}

	ContainerHostConfig struct {
		NetworkMode   string `json:"networkMode,omitempty"`
		RestartPolicy string `json:"restartPolicy,omitempty"`
		Privileged    bool   `json:"privileged,omitempty"`
		AutoRemove    bool   `json:"autoRemove,omitempty"`
		NanoCPUs      int64  `json:"nanoCpus,omitempty"`

		Memory int64 `json:"memory,omitempty"`
	}

	ContainerResourceSample struct {
		SampleTime       time.Time `json:"sampleTime"`
		CPUPercent       float64   `json:"cpuPercent"`
		MemoryUsageBytes uint64    `json:"memoryUsageBytes"`
		MemoryLimitBytes uint64    `json:"memoryLimitBytes"`

		BlockReadBytes  uint64 `json:"blockReadBytes"`
		BlockWriteBytes uint64 `json:"blockWriteBytes"`
		NetworkRxBytes  uint64 `json:"networkRxBytes"`
		NetworkTxBytes  uint64 `json:"networkTxBytes"`
		PIDs            uint64 `json:"pids"`
	}

	ContainerResourceSampleSubscription struct {
		ContainerIDs []string `json:"containerIds"`
	}

	ContainerResourceSampleBatch struct {
		Timestamp time.Time                           `json:"timestamp"`
		Samples   map[string]*ContainerResourceSample `json:"samples"`
		Error     string                              `json:"error,omitempty"`
	}

	ContainerSummary struct {
		Labels          map[string]string        `json:"labels"`
		NetworkSettings ContainerNetworkSettings `json:"networkSettings"`
		UpdateInfo      *ImageUpdateInfo         `json:"updateInfo,omitempty"`

		ResourceSample *ContainerResourceSample `json:"resourceSample,omitempty"`
		EnvironmentTag
		ID             string              `json:"id"`
		Image          string              `json:"image"`
		ImageID        string              `json:"imageId"`
		Command        string              `json:"command"`
		State          string              `json:"state"`
		Status         string              `json:"status"`
		IconLightURL   string              `json:"iconLightUrl,omitempty"`
		IconDarkURL    string              `json:"iconDarkUrl,omitempty"`
		UpdateStrategy string              `json:"updateStrategy,omitempty"`
		Names          []string            `json:"names"`
		Ports          []ContainerPort     `json:"ports"`
		Mounts         []ContainerMount    `json:"mounts"`
		HostConfig     ContainerHostConfig `json:"hostConfig"`

		Created          int64 `json:"created"`
		RedeployDisabled bool  `json:"redeployDisabled,omitempty"`

		AutoUpdateEnabled bool `json:"autoUpdateEnabled"`
		Hidden            bool `json:"hidden,omitempty"`
	}

	ContainerComposeInfo struct {
		ProjectName string `json:"projectName"`
		ServiceName string `json:"serviceName"`
		WorkingDir  string `json:"workingDir,omitempty"`
		ConfigFiles string `json:"configFiles,omitempty"`
	}

	ContainerSummaryGroup struct {
		GroupName string             `json:"groupName"`
		Items     []ContainerSummary `json:"items"`
	}

	ContainerDetails struct {
		Config          ContainerConfig          `json:"config"`
		State           ContainerState           `json:"state"`
		NetworkSettings ContainerNetworkSettings `json:"networkSettings"`
		Labels          map[string]string        `json:"labels,omitempty"`

		ComposeInfo      *ContainerComposeInfo `json:"composeInfo,omitempty"`
		UpdateInfo       *ImageUpdateInfo      `json:"updateInfo,omitempty"`
		Hardened         *HardenedImageInfo    `json:"hardened,omitempty"`
		ActivityID       *string               `json:"activityId,omitempty"`
		ID               string                `json:"id"`
		Name             string                `json:"name"`
		Image            string                `json:"image"`
		ImageID          string                `json:"imageId"`
		Created          string                `json:"created"`
		IconLightURL     string                `json:"iconLightUrl,omitempty"`
		IconDarkURL      string                `json:"iconDarkUrl,omitempty"`
		Ports            []ContainerPort       `json:"ports"`
		Mounts           []ContainerMount      `json:"mounts"`
		HostConfig       ContainerHostConfig   `json:"hostConfig"`
		RedeployDisabled bool                  `json:"redeployDisabled,omitempty"`

		AutoUpdateEnabled bool `json:"autoUpdateEnabled"`
	}

	ContainerCreated struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Image   string `json:"image"`
		Status  string `json:"status"`
		Created string `json:"created"`
	}

	ContainerHostConfigEdit struct {
		Binds          *[]string                                `json:"binds,omitempty"`
		Mounts         *[]ContainerMountCreate                  `json:"mounts,omitempty"`
		PortBindings   *map[string][]ContainerPortBindingCreate `json:"portBindings,omitempty"`
		RestartPolicy  *ContainerRestartPolicyCreate            `json:"restartPolicy,omitempty"`
		Privileged     *bool                                    `json:"privileged,omitempty"`
		CapAdd         *[]string                                `json:"capAdd,omitempty"`
		CapDrop        *[]string                                `json:"capDrop,omitempty"`
		AutoRemove     *bool                                    `json:"autoRemove,omitempty"`
		ReadonlyRootfs *bool                                    `json:"readonlyRootfs,omitempty"`

		Memory *int64 `json:"memory,omitempty"`

		MemorySwap *int64 `json:"memorySwap,omitempty"`
		NanoCPUs   *int64 `json:"nanoCpus,omitempty"`
		CPUShares  *int64 `json:"cpuShares,omitempty"`
	}

	ContainerEdit struct {
		Name       *string `json:"name,omitempty" doc:"New name; omitted or empty keeps the current one"`
		Image      *string `json:"image,omitempty" doc:"Omitted or empty keeps the current image; pulled only when missing locally"`
		WorkingDir *string `json:"workingDir,omitempty" doc:"Omitted keeps the current one"`
		User       *string `json:"user,omitempty" doc:"Omitted keeps the current one"`

		Hostname    *string                     `json:"hostname,omitempty" doc:"Omitted keeps the current one, empty clears it"`
		Command     *[]string                   `json:"command,omitempty" doc:"Replaces the current command; omitted keeps it"`
		Entrypoint  *[]string                   `json:"entrypoint,omitempty" doc:"Replaces the current entrypoint; omitted keeps it"`
		Environment *[]string                   `json:"environment,omitempty" doc:"Full KEY=VALUE list replacing the current one; omitted keeps it"`
		Labels      *map[string]string          `json:"labels,omitempty" doc:"Full label set replacing the current one; omitted keeps it"`
		Healthcheck *ContainerHealthcheckCreate `json:"healthcheck,omitempty" doc:"Replaces the current healthcheck, durations in seconds; omitted keeps it"`
		HostConfig  *ContainerHostConfigEdit    `json:"hostConfig,omitempty" doc:"Host settings to change; each omitted field keeps its current value"`

		NetworkingConfig *ContainerNetworkingConfigCreate `json:"networkingConfig,omitempty" doc:"Full set of endpoints keyed by network name; omitted keeps the current ones"`
		Credentials      []ContainerRegistryCredential    `json:"credentials,omitempty" doc:"Registry credentials for pulling a missing image"`
		ClearHealthcheck bool                             `json:"clearHealthcheck,omitempty" doc:"Drop the healthcheck override so the image's applies; wins over healthcheck"`
	}

	ContainerEditConfigNetwork struct {
		IPv4Address string   `json:"ipv4Address,omitempty"`
		IPv6Address string   `json:"ipv6Address,omitempty"`
		Aliases     []string `json:"aliases,omitempty"`
	}

	ContainerEditConfigHostConfig struct {
		PortBindings  map[string][]ContainerPortBindingCreate `json:"portBindings,omitempty"`
		NetworkMode   string                                  `json:"networkMode,omitempty"`
		Binds         []string                                `json:"binds,omitempty"`
		Mounts        []ContainerMountCreate                  `json:"mounts,omitempty"`
		RestartPolicy ContainerRestartPolicyCreate            `json:"restartPolicy"`
		CapAdd        []string                                `json:"capAdd,omitempty"`
		CapDrop       []string                                `json:"capDrop,omitempty"`

		Memory int64 `json:"memory,omitempty"`

		MemorySwap     int64 `json:"memorySwap,omitempty"`
		NanoCPUs       int64 `json:"nanoCpus,omitempty"`
		CPUShares      int64 `json:"cpuShares,omitempty"`
		Privileged     bool  `json:"privileged,omitempty"`
		AutoRemove     bool  `json:"autoRemove,omitempty"`
		ReadonlyRootfs bool  `json:"readonlyRootfs,omitempty"`
	}

	ContainerEditConfig struct {
		Labels map[string]string `json:"labels,omitempty"`

		Healthcheck *ContainerHealthcheckCreate           `json:"healthcheck,omitempty"`
		Networks    map[string]ContainerEditConfigNetwork `json:"networks,omitempty"`
		ID          string                                `json:"id"`
		Name        string                                `json:"name"`
		Image       string                                `json:"image"`
		WorkingDir  string                                `json:"workingDir,omitempty"`
		User        string                                `json:"user,omitempty"`

		Hostname       string                        `json:"hostname,omitempty"`
		ComposeProject string                        `json:"composeProject,omitempty"`
		Command        []string                      `json:"command,omitempty"`
		Entrypoint     []string                      `json:"entrypoint,omitempty"`
		Environment    []string                      `json:"environment,omitempty"`
		HostConfig     ContainerEditConfigHostConfig `json:"hostConfig"`
		IsCompose      bool                          `json:"isCompose,omitempty"`
		EditDisabled   bool                          `json:"editDisabled,omitempty"`
		Running        bool                          `json:"running"`
	}

	ContainerProcesses struct {
		Titles    []string   `json:"titles"`
		Processes [][]string `json:"processes"`
	}
)

// Resource sort keys accepted by the container list endpoint. They are part of
// the shared API contract: the manager's environment proxy inspects the sort
// query parameter to enforce the additional containers:read permission before
// forwarding resource-sorted requests to an agent.
const (
	ContainerSortCPUUsage    = "cpuUsage"
	ContainerSortMemoryUsage = "memoryUsage"
)

func IsContainerResourceSort(sort string) bool {
	return sort == ContainerSortCPUUsage || sort == ContainerSortMemoryUsage
}

func NewContainerSummary(c container.Summary) ContainerSummary {
	names := make([]string, 0, len(c.Names))
	for _, name := range c.Names {
		names = append(names, strings.TrimPrefix(name, "/"))
	}

	ports := make([]ContainerPort, 0, len(c.Ports))
	for _, p := range c.Ports {
		hostIP := ""
		if p.IP.IsValid() {
			hostIP = p.IP.String()
		}
		ports = append(ports, ContainerPort{
			IP:          hostIP,
			PrivatePort: int(p.PrivatePort),
			PublicPort:  int(p.PublicPort),
			Type:        p.Type,
		})
	}

	mounts := make([]ContainerMount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, ContainerMount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			Driver:      m.Driver,
			Mode:        m.Mode,
			RW:          m.RW,
			Propagation: string(m.Propagation),
		})
	}

	networks := map[string]ContainerNetworkEndpoint{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	return ContainerSummary{
		ID:      c.ID,
		Names:   names,
		Image:   c.Image,
		ImageID: c.ImageID,
		Command: c.Command,
		Created: c.Created,
		Ports:   ports,
		Labels:  c.Labels,
		State:   string(c.State),
		Status:  c.Status,
		HostConfig: ContainerHostConfig{
			NetworkMode: c.HostConfig.NetworkMode,
		},
		NetworkSettings: ContainerNetworkSettings{
			Networks: networks,
		},
		Mounts: mounts,
	}
}

func NewContainerDetails(c *container.InspectResponse) ContainerDetails {
	cfg, labels, imageName := mapInspectConfig(c.Config)

	return ContainerDetails{
		ID:         c.ID,
		Name:       strings.TrimPrefix(c.Name, "/"),
		Image:      imageName,
		ImageID:    c.Image,
		Created:    c.Created,
		State:      mapInspectState(c.State),
		Config:     cfg,
		HostConfig: mapInspectHostConfig(c.HostConfig),
		NetworkSettings: ContainerNetworkSettings{
			Networks: mapInspectNetworks(c.NetworkSettings),
		},
		Ports:       mapInspectPorts(c.NetworkSettings),
		Mounts:      mapInspectMounts(c.Mounts),
		Labels:      labels,
		ComposeInfo: mapComposeInfo(labels),
	}
}

func mapInspectPorts(networkSettings *container.NetworkSettings) []ContainerPort {
	ports := make([]ContainerPort, 0)
	if networkSettings == nil || networkSettings.Ports == nil {
		return ports
	}

	for p, bindings := range networkSettings.Ports {
		privatePort := int(p.Num())
		typ := string(p.Proto())

		if len(bindings) == 0 {
			ports = append(ports, ContainerPort{
				PrivatePort: privatePort,
				Type:        typ,
			})
			continue
		}

		for _, b := range bindings {
			pub, _ := strconv.Atoi(b.HostPort)
			hostIP := ""
			if b.HostIP.IsValid() {
				hostIP = b.HostIP.String()
			}
			ports = append(ports, ContainerPort{
				IP:          hostIP,
				PrivatePort: privatePort,
				PublicPort:  pub,
				Type:        typ,
			})
		}
	}

	return ports
}

func mapInspectMounts(mountPoints []container.MountPoint) []ContainerMount {
	mounts := make([]ContainerMount, 0, len(mountPoints))
	for _, m := range mountPoints {
		mounts = append(mounts, ContainerMount{
			Type:        string(m.Type),
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			Driver:      m.Driver,
			Mode:        m.Mode,
			RW:          m.RW,
			Propagation: string(m.Propagation),
		})
	}

	return mounts
}

func mapInspectNetworks(networkSettings *container.NetworkSettings) map[string]ContainerNetworkEndpoint {
	networks := map[string]ContainerNetworkEndpoint{}
	if networkSettings == nil || networkSettings.Networks == nil {
		return networks
	}

	for name, n := range networkSettings.Networks {
		networks[name] = mapEndpointSettings(n)
	}

	return networks
}

func mapInspectHostConfig(hostConfig *container.HostConfig) ContainerHostConfig {
	if hostConfig == nil {
		return ContainerHostConfig{}
	}

	return ContainerHostConfig{
		RestartPolicy: string(hostConfig.RestartPolicy.Name),
		Privileged:    hostConfig.Privileged,
		AutoRemove:    hostConfig.AutoRemove,
		NanoCPUs:      hostConfig.NanoCPUs,
		Memory:        hostConfig.Memory,
	}
}

func mapInspectConfig(config *container.Config) (ContainerConfig, map[string]string, string) {
	labels := map[string]string{}
	if config == nil {
		return ContainerConfig{}, labels, ""
	}

	cfg := ContainerConfig{
		Env:        append([]string{}, config.Env...),
		Cmd:        append([]string{}, config.Cmd...),
		Entrypoint: append([]string{}, config.Entrypoint...),
		WorkingDir: config.WorkingDir,
		User:       config.User,
		Hostname:   config.Hostname,
	}

	if hc := config.Healthcheck; hc != nil {
		cfg.Healthcheck = &ContainerHealthcheck{
			Test:          append([]string{}, hc.Test...),
			Interval:      int64(hc.Interval),
			Timeout:       int64(hc.Timeout),
			StartPeriod:   int64(hc.StartPeriod),
			StartInterval: int64(hc.StartInterval),
			Retries:       hc.Retries,
		}
	}

	if config.Labels != nil {
		maps.Copy(labels, config.Labels)
	}

	return cfg, labels, config.Image
}

func mapInspectState(state *container.State) ContainerState {
	if state == nil {
		return ContainerState{}
	}

	mappedState := ContainerState{
		Status:     string(state.Status),
		Running:    state.Running,
		ExitCode:   state.ExitCode,
		StartedAt:  state.StartedAt,
		FinishedAt: state.FinishedAt,
	}

	if state.Health != nil {
		mappedState.Health = mapInspectHealth(state.Health)
	}

	return mappedState
}

func mapInspectHealth(health *container.Health) *ContainerHealth {
	log := make([]ContainerHealthLogEntry, 0, len(health.Log))
	for _, entry := range health.Log {
		if entry == nil {
			continue
		}

		log = append(log, ContainerHealthLogEntry{
			Start:    formatTimeOrEmpty(entry.Start),
			End:      formatTimeOrEmpty(entry.End),
			ExitCode: entry.ExitCode,
			Output:   entry.Output,
		})
	}

	return &ContainerHealth{
		Status:        string(health.Status),
		FailingStreak: health.FailingStreak,
		Log:           log,
	}
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05.999999999Z07:00")
}

func mapComposeInfo(labels map[string]string) *ContainerComposeInfo {
	projectName, hasProject := labels["com.docker.compose.project"]
	if !hasProject {
		return nil
	}

	serviceName, hasService := labels["com.docker.compose.service"]
	if !hasService {
		return nil
	}

	composeInfo := &ContainerComposeInfo{
		ProjectName: projectName,
		ServiceName: serviceName,
	}
	if workingDir, ok := labels["com.docker.compose.project.working_dir"]; ok {
		composeInfo.WorkingDir = workingDir
	}
	if configFiles, ok := labels["com.docker.compose.project.config_files"]; ok {
		composeInfo.ConfigFiles = configFiles
	}

	return composeInfo
}

func mapEndpointSettings(n *network.EndpointSettings) ContainerNetworkEndpoint {
	if n == nil {
		return ContainerNetworkEndpoint{}
	}

	var driverOpts map[string]string
	if n.DriverOpts != nil {
		driverOpts = n.DriverOpts
	}

	gateway := ""
	if n.Gateway.IsValid() {
		gateway = n.Gateway.String()
	}

	ipAddress := ""
	if n.IPAddress.IsValid() {
		ipAddress = n.IPAddress.String()
	}

	ipv6Gateway := ""
	if n.IPv6Gateway.IsValid() {
		ipv6Gateway = n.IPv6Gateway.String()
	}

	globalIPv6Address := ""
	if n.GlobalIPv6Address.IsValid() {
		globalIPv6Address = n.GlobalIPv6Address.String()
	}

	return ContainerNetworkEndpoint{
		IPAMConfig:          n.IPAMConfig,
		Links:               n.Links,
		Aliases:             n.Aliases,
		MacAddress:          n.MacAddress.String(),
		DriverOpts:          driverOpts,
		GwPriority:          n.GwPriority,
		NetworkID:           n.NetworkID,
		EndpointID:          n.EndpointID,
		Gateway:             gateway,
		IPAddress:           ipAddress,
		IPPrefixLen:         n.IPPrefixLen,
		IPv6Gateway:         ipv6Gateway,
		GlobalIPv6Address:   globalIPv6Address,
		GlobalIPv6PrefixLen: n.GlobalIPv6PrefixLen,
		DNSNames:            n.DNSNames,
	}
}

func NewContainerEditConfigFromInspect(c *container.InspectResponse, isCompose, editDisabled bool) ContainerEditConfig {
	cfg := ContainerEditConfig{
		ID:           c.ID,
		Name:         strings.TrimPrefix(c.Name, "/"),
		IsCompose:    isCompose,
		EditDisabled: editDisabled,
		Running:      c.State != nil && c.State.Running,
	}

	if c.Config != nil {
		cfg.Image = c.Config.Image
		cfg.Command = append([]string{}, c.Config.Cmd...)
		cfg.Entrypoint = append([]string{}, c.Config.Entrypoint...)
		cfg.WorkingDir = c.Config.WorkingDir
		cfg.User = c.Config.User
		if len(c.ID) < 12 || c.Config.Hostname != c.ID[:12] {
			cfg.Hostname = c.Config.Hostname
		}
		cfg.Environment = append([]string{}, c.Config.Env...)
		cfg.Labels = c.Config.Labels
		cfg.ComposeProject = c.Config.Labels["com.docker.compose.project"]
		cfg.Healthcheck = mapEditHealthcheck(c.Config.Healthcheck)
	}

	cfg.HostConfig = mapEditHostConfig(c.HostConfig)
	cfg.Networks = mapEditNetworks(c)

	return cfg
}

func mapEditHealthcheck(hc *container.HealthConfig) *ContainerHealthcheckCreate {
	if hc == nil {
		return nil
	}

	return &ContainerHealthcheckCreate{
		Test:          append([]string{}, hc.Test...),
		Interval:      int64(hc.Interval / time.Second),
		Timeout:       int64(hc.Timeout / time.Second),
		StartPeriod:   int64(hc.StartPeriod / time.Second),
		StartInterval: int64(hc.StartInterval / time.Second),
		Retries:       hc.Retries,
	}
}

func mapEditHostConfig(hc *container.HostConfig) ContainerEditConfigHostConfig {
	if hc == nil {
		return ContainerEditConfigHostConfig{}
	}

	out := ContainerEditConfigHostConfig{
		Binds: append([]string{}, hc.Binds...),
		RestartPolicy: ContainerRestartPolicyCreate{
			Name:              string(hc.RestartPolicy.Name),
			MaximumRetryCount: hc.RestartPolicy.MaximumRetryCount,
		},
		NetworkMode:    string(hc.NetworkMode),
		Privileged:     hc.Privileged,
		CapAdd:         append([]string{}, hc.CapAdd...),
		CapDrop:        append([]string{}, hc.CapDrop...),
		AutoRemove:     hc.AutoRemove,
		ReadonlyRootfs: hc.ReadonlyRootfs,
		Memory:         hc.Memory,
		MemorySwap:     hc.MemorySwap,
		NanoCPUs:       hc.NanoCPUs,
		CPUShares:      hc.CPUShares,
	}

	for _, m := range hc.Mounts {
		out.Mounts = append(out.Mounts, ContainerMountCreate{
			Type:     string(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	if len(hc.PortBindings) > 0 {
		out.PortBindings = make(map[string][]ContainerPortBindingCreate, len(hc.PortBindings))
		for port, bindings := range hc.PortBindings {
			mapped := make([]ContainerPortBindingCreate, 0, len(bindings))
			for _, b := range bindings {
				hostIP := ""
				if b.HostIP.IsValid() {
					hostIP = b.HostIP.String()
				}
				mapped = append(mapped, ContainerPortBindingCreate{
					HostIP:   hostIP,
					HostPort: b.HostPort,
				})
			}
			out.PortBindings[port.String()] = mapped
		}
	}

	return out
}

func mapEditNetworks(c *container.InspectResponse) map[string]ContainerEditConfigNetwork {
	if c.NetworkSettings == nil || len(c.NetworkSettings.Networks) == 0 {
		return nil
	}

	shortID := ""
	if len(c.ID) >= 12 {
		shortID = c.ID[:12]
	}

	out := make(map[string]ContainerEditConfigNetwork, len(c.NetworkSettings.Networks))
	for name, endpoint := range c.NetworkSettings.Networks {
		mapped := ContainerEditConfigNetwork{}
		if endpoint != nil {
			// The daemon injects the container short ID as an implicit alias;
			// it is regenerated on recreate and must not round-trip as a
			// user-defined alias.
			for _, alias := range endpoint.Aliases {
				if alias == "" || alias == shortID {
					continue
				}
				mapped.Aliases = append(mapped.Aliases, alias)
			}
			if endpoint.IPAMConfig != nil {
				if endpoint.IPAMConfig.IPv4Address.IsValid() {
					mapped.IPv4Address = endpoint.IPAMConfig.IPv4Address.String()
				}
				if endpoint.IPAMConfig.IPv6Address.IsValid() {
					mapped.IPv6Address = endpoint.IPAMConfig.IPv6Address.String()
				}
			}
		}
		out[name] = mapped
	}

	return out
}

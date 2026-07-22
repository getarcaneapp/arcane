package docker

import (
	"maps"
	"strconv"
	"strings"
	"time"

	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

// NewContainerSummary creates an API container summary from a Docker container summary.
func NewContainerSummary(c container.Summary) containertypes.Summary {
	names := make([]string, 0, len(c.Names))
	for _, name := range c.Names {
		names = append(names, strings.TrimPrefix(name, "/"))
	}

	ports := make([]containertypes.Port, 0, len(c.Ports))
	for _, p := range c.Ports {
		ports = append(ports, containertypes.Port{
			IP:          p.IP.String(),
			PrivatePort: int(p.PrivatePort),
			PublicPort:  int(p.PublicPort),
			Type:        p.Type,
		})
	}

	mounts := make([]containertypes.Mount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, containertypes.Mount{
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

	networks := map[string]containertypes.NetworkEndpoint{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	return containertypes.Summary{
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
		HostConfig: containertypes.HostConfig{
			NetworkMode: c.HostConfig.NetworkMode,
		},
		NetworkSettings: containertypes.NetworkSettings{
			Networks: networks,
		},
		Mounts: mounts,
	}
}

// NewContainerDetails creates API container details from a Docker inspect response.
func NewContainerDetails(c *container.InspectResponse) containertypes.Details {
	cfg, labels, imageName := mapInspectConfig(c.Config)

	return containertypes.Details{
		ID:         c.ID,
		Name:       strings.TrimPrefix(c.Name, "/"),
		Image:      imageName,
		ImageID:    c.Image,
		Created:    c.Created,
		State:      mapInspectState(c.State),
		Config:     cfg,
		HostConfig: mapInspectHostConfig(c.HostConfig),
		NetworkSettings: containertypes.NetworkSettings{
			Networks: mapInspectNetworks(c.NetworkSettings),
		},
		Ports:       mapInspectPorts(c.NetworkSettings),
		Mounts:      mapInspectMounts(c.Mounts),
		Labels:      labels,
		ComposeInfo: mapComposeInfo(labels),
	}
}

func mapInspectPorts(networkSettings *container.NetworkSettings) []containertypes.Port {
	ports := make([]containertypes.Port, 0)
	if networkSettings == nil || networkSettings.Ports == nil {
		return ports
	}

	for p, bindings := range networkSettings.Ports {
		privatePort := int(p.Num())
		typ := string(p.Proto())

		// When no host bindings exist, still include the private port.
		if len(bindings) == 0 {
			ports = append(ports, containertypes.Port{
				PrivatePort: privatePort,
				Type:        typ,
			})
			continue
		}

		for _, b := range bindings {
			pub, _ := strconv.Atoi(b.HostPort)
			ports = append(ports, containertypes.Port{
				IP:          b.HostIP.String(),
				PrivatePort: privatePort,
				PublicPort:  pub,
				Type:        typ,
			})
		}
	}

	return ports
}

func mapInspectMounts(mountPoints []container.MountPoint) []containertypes.Mount {
	mounts := make([]containertypes.Mount, 0, len(mountPoints))
	for _, m := range mountPoints {
		mounts = append(mounts, containertypes.Mount{
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

func mapInspectNetworks(networkSettings *container.NetworkSettings) map[string]containertypes.NetworkEndpoint {
	networks := map[string]containertypes.NetworkEndpoint{}
	if networkSettings == nil || networkSettings.Networks == nil {
		return networks
	}

	for name, n := range networkSettings.Networks {
		networks[name] = mapEndpointSettings(n)
	}

	return networks
}

func mapInspectHostConfig(hostConfig *container.HostConfig) containertypes.HostConfig {
	if hostConfig == nil {
		return containertypes.HostConfig{}
	}

	return containertypes.HostConfig{
		RestartPolicy: string(hostConfig.RestartPolicy.Name),
		Privileged:    hostConfig.Privileged,
		AutoRemove:    hostConfig.AutoRemove,
		NanoCPUs:      hostConfig.NanoCPUs,
		Memory:        hostConfig.Memory,
	}
}

func mapInspectConfig(config *container.Config) (containertypes.Config, map[string]string, string) {
	labels := map[string]string{}
	if config == nil {
		return containertypes.Config{}, labels, ""
	}

	cfg := containertypes.Config{
		Env:        append([]string{}, config.Env...),
		Cmd:        append([]string{}, config.Cmd...),
		Entrypoint: append([]string{}, config.Entrypoint...),
		WorkingDir: config.WorkingDir,
		User:       config.User,
	}

	if hc := config.Healthcheck; hc != nil {
		cfg.Healthcheck = &containertypes.Healthcheck{
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

func mapInspectState(state *container.State) containertypes.State {
	if state == nil {
		return containertypes.State{}
	}

	mappedState := containertypes.State{
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

func mapInspectHealth(health *container.Health) *containertypes.Health {
	log := make([]containertypes.HealthLogEntry, 0, len(health.Log))
	for _, entry := range health.Log {
		if entry == nil {
			continue
		}

		log = append(log, containertypes.HealthLogEntry{
			Start:    formatTimeOrEmpty(entry.Start),
			End:      formatTimeOrEmpty(entry.End),
			ExitCode: entry.ExitCode,
			Output:   entry.Output,
		})
	}

	return &containertypes.Health{
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

func mapComposeInfo(labels map[string]string) *containertypes.ComposeInfo {
	projectName, hasProject := labels[ComposeProjectLabelKey]
	if !hasProject {
		return nil
	}

	serviceName, hasService := labels[ComposeServiceLabelKey]
	if !hasService {
		return nil
	}

	composeInfo := &containertypes.ComposeInfo{
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

func mapEndpointSettings(n *network.EndpointSettings) containertypes.NetworkEndpoint {
	if n == nil {
		return containertypes.NetworkEndpoint{}
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

	return containertypes.NetworkEndpoint{
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

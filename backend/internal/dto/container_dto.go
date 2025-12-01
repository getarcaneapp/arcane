package dto

import (
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

type ContainerStatusLengthsDto struct {
	RunningContainers int `json:"runningContainers"`
	StoppedContainers int `json:"stoppedContainers"`
	TotalContainers   int `json:"totalContainers"`
}

type ContainerActionResult struct {
	Started []string `json:"started,omitempty"`
	Stopped []string `json:"stopped,omitempty"`
	Failed  []string `json:"failed,omitempty"`
	Success bool     `json:"success"`
	Errors  []string `json:"errors,omitempty"`
}

type PortDto struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort,omitempty"`
	Type        string `json:"type"`
}

type MountDto struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
	Driver      string `json:"driver,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RW          bool   `json:"rw,omitempty"`
	Propagation string `json:"propagation,omitempty"`
}

type NetworkDto struct {
	IPAMConfig          any               `json:"ipamConfig"`
	Links               []string          `json:"links"`
	Aliases             []string          `json:"aliases"`
	MacAddress          string            `json:"macAddress"`
	DriverOpts          map[string]string `json:"driverOpts"`
	GwPriority          int               `json:"gwPriority"`
	NetworkID           string            `json:"networkId"`
	EndpointID          string            `json:"endpointId"`
	Gateway             string            `json:"gateway"`
	IPAddress           string            `json:"ipAddress"`
	IPPrefixLen         int               `json:"ipPrefixLen"`
	IPv6Gateway         string            `json:"ipv6Gateway"`
	GlobalIPv6Address   string            `json:"globalIPv6Address"`
	GlobalIPv6PrefixLen int               `json:"globalIPv6PrefixLen"`
	DNSNames            []string          `json:"dnsNames"`
}

type HostConfigDto struct {
	NetworkMode string `json:"networkMode"`
}

type NetworkSettingsDto struct {
	Networks map[string]NetworkDto `json:"networks"`
}

type ContainerSummaryDto struct {
	ID              string             `json:"id"`
	Names           []string           `json:"names"`
	Image           string             `json:"image"`
	ImageID         string             `json:"imageId"`
	Command         string             `json:"command"`
	Created         int64              `json:"created"`
	Ports           []PortDto          `json:"ports"`
	Labels          map[string]string  `json:"labels"`
	State           string             `json:"state"`
	Status          string             `json:"status"`
	HostConfig      HostConfigDto      `json:"hostConfig"`
	NetworkSettings NetworkSettingsDto `json:"networkSettings"`
	Mounts          []MountDto         `json:"mounts"`
}

func NewContainerSummaryDto(c container.Summary) ContainerSummaryDto {
	ports := make([]PortDto, 0, len(c.Ports))
	for _, p := range c.Ports {
		ports = append(ports, PortDto{
			IP:          p.IP,
			PrivatePort: int(p.PrivatePort),
			PublicPort:  int(p.PublicPort),
			Type:        p.Type,
		})
	}

	mounts := make([]MountDto, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, MountDto{
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

	networks := map[string]NetworkDto{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	return ContainerSummaryDto{
		ID:      c.ID,
		Names:   c.Names,
		Image:   c.Image,
		ImageID: c.ImageID,
		Command: c.Command,
		Created: c.Created,
		Ports:   ports,
		Labels:  c.Labels,
		State:   c.State,
		Status:  c.Status,
		HostConfig: HostConfigDto{
			NetworkMode: c.HostConfig.NetworkMode,
		},
		NetworkSettings: NetworkSettingsDto{
			Networks: networks,
		},
		Mounts: mounts,
	}
}

func mapEndpointSettings(n *network.EndpointSettings) NetworkDto {
	if n == nil {
		return NetworkDto{}
	}
	// DriverOpts can be nil in Docker types; ensure non-nil map for JSON stability if needed
	var driverOpts map[string]string
	if n.DriverOpts != nil {
		driverOpts = n.DriverOpts
	}

	return NetworkDto{
		IPAMConfig:          n.IPAMConfig,
		Links:               n.Links,
		Aliases:             n.Aliases,
		MacAddress:          n.MacAddress,
		DriverOpts:          driverOpts,
		GwPriority:          n.GwPriority,
		NetworkID:           n.NetworkID,
		EndpointID:          n.EndpointID,
		Gateway:             n.Gateway,
		IPAddress:           n.IPAddress,
		IPPrefixLen:         n.IPPrefixLen,
		IPv6Gateway:         n.IPv6Gateway,
		GlobalIPv6Address:   n.GlobalIPv6Address,
		GlobalIPv6PrefixLen: n.GlobalIPv6PrefixLen,
		DNSNames:            n.DNSNames,
	}
}

type ContainerStateDto struct {
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	ExitCode   int    `json:"exitCode,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type ContainerConfigDetailsDto struct {
	Env        []string `json:"env,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	WorkingDir string   `json:"workingDir,omitempty"`
	User       string   `json:"user,omitempty"`
}

type HostConfigDetailsDto struct {
	RestartPolicy string `json:"restartPolicy,omitempty"`
	Privileged    bool   `json:"privileged,omitempty"`
	AutoRemove    bool   `json:"autoRemove,omitempty"`
	NanoCPUs      int64  `json:"nanoCpus,omitempty"`
	Memory        int64  `json:"memory,omitempty"`
}

type ContainerDetailsDto struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Image           string                    `json:"image"`
	ImageID         string                    `json:"imageId"`
	Created         string                    `json:"created"`
	State           ContainerStateDto         `json:"state"`
	Config          ContainerConfigDetailsDto `json:"config"`
	HostConfig      HostConfigDetailsDto      `json:"hostConfig"`
	NetworkSettings NetworkSettingsDto        `json:"networkSettings"`
	Ports           []PortDto                 `json:"ports"`
	Mounts          []MountDto                `json:"mounts"`
	Labels          map[string]string         `json:"labels,omitempty"`
}

type ContainerCreatedDto struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Created string `json:"created"`
}

func NewContainerDetailsDto(c *container.InspectResponse) ContainerDetailsDto {
	ports := make([]PortDto, 0)
	if c.NetworkSettings != nil && c.NetworkSettings.Ports != nil {
		for p, bindings := range c.NetworkSettings.Ports {
			privatePort, _ := strconv.Atoi(p.Port())
			typ := string(p.Proto())

			// When no host bindings exist, still include the private port
			if len(bindings) == 0 {
				ports = append(ports, PortDto{
					PrivatePort: privatePort,
					Type:        typ,
				})
				continue
			}
			for _, b := range bindings {
				pub, _ := strconv.Atoi(b.HostPort)
				ports = append(ports, PortDto{
					IP:          b.HostIP,
					PrivatePort: privatePort,
					PublicPort:  pub,
					Type:        typ,
				})
			}
		}
	}

	mounts := make([]MountDto, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, mapMountPoint(m))
	}

	networks := map[string]NetworkDto{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	var host HostConfigDetailsDto
	if c.HostConfig != nil {
		host = HostConfigDetailsDto{
			RestartPolicy: string(c.HostConfig.RestartPolicy.Name),
			Privileged:    c.HostConfig.Privileged,
			AutoRemove:    c.HostConfig.AutoRemove,
			NanoCPUs:      c.HostConfig.NanoCPUs,
			Memory:        c.HostConfig.Memory,
		}
	}

	var cfg ContainerConfigDetailsDto
	labels := map[string]string{}
	imageName := ""
	if c.Config != nil {
		cfg = ContainerConfigDetailsDto{
			Env:        append([]string{}, c.Config.Env...),
			Cmd:        append([]string{}, c.Config.Cmd...),
			Entrypoint: append([]string{}, c.Config.Entrypoint...),
			WorkingDir: c.Config.WorkingDir,
			User:       c.Config.User,
		}
		imageName = c.Config.Image
		if c.Config.Labels != nil {
			for k, v := range c.Config.Labels {
				labels[k] = v
			}
		}
	}

	name := strings.TrimPrefix(c.Name, "/")

	out := ContainerDetailsDto{
		ID:      c.ID,
		Name:    name,
		Image:   imageName,
		ImageID: c.Image,
		Created: c.Created,
		State: ContainerStateDto{
			Status:     safeStateStatus(c),
			Running:    safeStateRunning(c),
			ExitCode:   safeStateExitCode(c),
			StartedAt:  safeStateStartedAt(c),
			FinishedAt: safeStateFinishedAt(c),
		},
		Config:     cfg,
		HostConfig: host,
		NetworkSettings: NetworkSettingsDto{
			Networks: networks,
		},
		Ports:  ports,
		Mounts: mounts,
		Labels: labels,
	}

	return out
}

func mapMountPoint(m container.MountPoint) MountDto {
	return MountDto{
		Type:        string(m.Type),
		Name:        m.Name,
		Source:      m.Source,
		Destination: m.Destination,
		Driver:      m.Driver,
		Mode:        m.Mode,
		RW:          m.RW,
		Propagation: string(m.Propagation),
	}
}

func safeStateStatus(c *container.InspectResponse) string {
	if c.State == nil {
		return ""
	}
	return c.State.Status
}
func safeStateRunning(c *container.InspectResponse) bool {
	return c.State != nil && c.State.Running
}
func safeStateExitCode(c *container.InspectResponse) int {
	if c.State == nil {
		return 0
	}
	return c.State.ExitCode
}
func safeStateStartedAt(c *container.InspectResponse) string {
	if c.State == nil {
		return ""
	}
	return c.State.StartedAt
}
func safeStateFinishedAt(c *container.InspectResponse) string {
	if c.State == nil {
		return ""
	}
	return c.State.FinishedAt
}

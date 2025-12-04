package container

import (
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	containerregistry "go.getarcane.app/types/containerregistry"
)

// Create is used to create a new container.
type Create struct {
	// Name of the container.
	//
	// Required: true
	Name string `json:"name" binding:"required"`

	// Image to use for the container.
	//
	// Required: true
	Image string `json:"image" binding:"required"`

	// Command to run in the container.
	//
	// Required: false
	Command []string `json:"command,omitempty"`

	// Entrypoint for the container.
	//
	// Required: false
	Entrypoint []string `json:"entrypoint,omitempty"`

	// WorkingDir is the working directory inside the container.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`

	// User to run the container as.
	//
	// Required: false
	User string `json:"user,omitempty"`

	// Environment variables for the container.
	//
	// Required: false
	Environment []string `json:"environment,omitempty"`

	// Ports is a map of port bindings.
	//
	// Required: false
	Ports map[string]string `json:"ports,omitempty"`

	// Volumes is a list of volume mounts.
	//
	// Required: false
	Volumes []string `json:"volumes,omitempty"`

	// Networks is a list of networks to connect to.
	//
	// Required: false
	Networks []string `json:"networks,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy string `json:"restartPolicy,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged bool `json:"privileged,omitempty"`

	// AutoRemove indicates if the container should be removed when stopped.
	//
	// Required: false
	AutoRemove bool `json:"autoRemove,omitempty"`

	// Memory limit for the container in bytes.
	//
	// Required: false
	Memory int64 `json:"memory,omitempty"`

	// CPUs is the number of CPUs to allocate.
	//
	// Required: false
	CPUs float64 `json:"cpus,omitempty"`

	// Credentials for pulling images from private registries.
	//
	// Required: false
	Credentials []containerregistry.Credential `json:"credentials,omitempty"`
}

// StatusCounts contains counts of containers by status.
type StatusCounts struct {
	// RunningContainers is the number of running containers.
	//
	// Required: true
	RunningContainers int `json:"runningContainers"`

	// StoppedContainers is the number of stopped containers.
	//
	// Required: true
	StoppedContainers int `json:"stoppedContainers"`

	// TotalContainers is the total number of containers.
	//
	// Required: true
	TotalContainers int `json:"totalContainers"`
}

// ActionResult represents the result of a container action (start/stop/etc).
type ActionResult struct {
	// Started is a list of container IDs that were started.
	//
	// Required: false
	Started []string `json:"started,omitempty"`

	// Stopped is a list of container IDs that were stopped.
	//
	// Required: false
	Stopped []string `json:"stopped,omitempty"`

	// Failed is a list of container IDs that failed.
	//
	// Required: false
	Failed []string `json:"failed,omitempty"`

	// Success indicates if the overall action was successful.
	//
	// Required: true
	Success bool `json:"success"`

	// Errors is a list of error messages encountered.
	//
	// Required: false
	Errors []string `json:"errors,omitempty"`
}

// Port represents a port binding for a container.
type Port struct {
	// IP address the port is bound to.
	//
	// Required: false
	IP string `json:"ip,omitempty"`

	// PrivatePort is the port inside the container.
	//
	// Required: true
	PrivatePort int `json:"privatePort"`

	// PublicPort is the port on the host.
	//
	// Required: false
	PublicPort int `json:"publicPort,omitempty"`

	// Type is the protocol type (tcp/udp).
	//
	// Required: true
	Type string `json:"type"`
}

// Mount represents a volume mount for a container.
type Mount struct {
	// Type of the mount (bind, volume, tmpfs).
	//
	// Required: true
	Type string `json:"type"`

	// Name of the volume (for volume mounts).
	//
	// Required: false
	Name string `json:"name,omitempty"`

	// Source path on the host.
	//
	// Required: false
	Source string `json:"source,omitempty"`

	// Destination path in the container.
	//
	// Required: true
	Destination string `json:"destination"`

	// Driver is the volume driver (for volume mounts).
	//
	// Required: false
	Driver string `json:"driver,omitempty"`

	// Mode specifies mount permissions.
	//
	// Required: false
	Mode string `json:"mode,omitempty"`

	// RW indicates if the mount is read-write.
	//
	// Required: false
	RW bool `json:"rw,omitempty"`

	// Propagation mode for the mount.
	//
	// Required: false
	Propagation string `json:"propagation,omitempty"`
}

// NetworkEndpoint represents network endpoint settings for a container.
type NetworkEndpoint struct {
	// IPAMConfig contains IP address management configuration.
	//
	// Required: false
	IPAMConfig any `json:"ipamConfig,omitempty"`

	// Links to other containers.
	//
	// Required: false
	Links []string `json:"links,omitempty"`

	// Aliases for the container on this network.
	//
	// Required: false
	Aliases []string `json:"aliases,omitempty"`

	// MacAddress of the container on this network.
	//
	// Required: false
	MacAddress string `json:"macAddress,omitempty"`

	// DriverOpts contains driver-specific options.
	//
	// Required: false
	DriverOpts map[string]string `json:"driverOpts,omitempty"`

	// GwPriority is the gateway priority.
	//
	// Required: false
	GwPriority int `json:"gwPriority,omitempty"`

	// NetworkID is the ID of the network.
	//
	// Required: false
	NetworkID string `json:"networkId,omitempty"`

	// EndpointID is the ID of this endpoint.
	//
	// Required: false
	EndpointID string `json:"endpointId,omitempty"`

	// Gateway address for the network.
	//
	// Required: false
	Gateway string `json:"gateway,omitempty"`

	// IPAddress assigned to the container.
	//
	// Required: false
	IPAddress string `json:"ipAddress,omitempty"`

	// IPPrefixLen is the IP prefix length.
	//
	// Required: false
	IPPrefixLen int `json:"ipPrefixLen,omitempty"`

	// IPv6Gateway address for the network.
	//
	// Required: false
	IPv6Gateway string `json:"ipv6Gateway,omitempty"`

	// GlobalIPv6Address assigned to the container.
	//
	// Required: false
	GlobalIPv6Address string `json:"globalIPv6Address,omitempty"`

	// GlobalIPv6PrefixLen is the IPv6 prefix length.
	//
	// Required: false
	GlobalIPv6PrefixLen int `json:"globalIPv6PrefixLen,omitempty"`

	// DNSNames are DNS names for this endpoint.
	//
	// Required: false
	DNSNames []string `json:"dnsNames,omitempty"`
}

// NetworkSettings contains network configuration for a container.
type NetworkSettings struct {
	// Networks is a map of network name to endpoint settings.
	//
	// Required: true
	Networks map[string]NetworkEndpoint `json:"networks"`
}

// State represents the state of a container.
type State struct {
	// Status is the current status of the container.
	//
	// Required: true
	Status string `json:"status"`

	// Running indicates if the container is running.
	//
	// Required: true
	Running bool `json:"running"`

	// ExitCode is the exit code of the container process.
	//
	// Required: false
	ExitCode int `json:"exitCode,omitempty"`

	// StartedAt is when the container was started.
	//
	// Required: false
	StartedAt string `json:"startedAt,omitempty"`

	// FinishedAt is when the container finished.
	//
	// Required: false
	FinishedAt string `json:"finishedAt,omitempty"`
}

// Config represents configuration details for a container.
type Config struct {
	// Env is a list of environment variables.
	//
	// Required: false
	Env []string `json:"env,omitempty"`

	// Cmd is the command to run.
	//
	// Required: false
	Cmd []string `json:"cmd,omitempty"`

	// Entrypoint is the entrypoint for the container.
	//
	// Required: false
	Entrypoint []string `json:"entrypoint,omitempty"`

	// WorkingDir is the working directory.
	//
	// Required: false
	WorkingDir string `json:"workingDir,omitempty"`

	// User to run as.
	//
	// Required: false
	User string `json:"user,omitempty"`
}

// HostConfig represents host configuration for a container.
type HostConfig struct {
	// NetworkMode for the container.
	//
	// Required: false
	NetworkMode string `json:"networkMode,omitempty"`

	// RestartPolicy for the container.
	//
	// Required: false
	RestartPolicy string `json:"restartPolicy,omitempty"`

	// Privileged indicates if the container runs in privileged mode.
	//
	// Required: false
	Privileged bool `json:"privileged,omitempty"`

	// AutoRemove indicates if the container is removed when stopped.
	//
	// Required: false
	AutoRemove bool `json:"autoRemove,omitempty"`

	// NanoCPUs is CPU allocation in nano CPUs.
	//
	// Required: false
	NanoCPUs int64 `json:"nanoCpus,omitempty"`

	// Memory limit in bytes.
	//
	// Required: false
	Memory int64 `json:"memory,omitempty"`
}

// Summary represents a container summary.
type Summary struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Names is a list of names for the container.
	//
	// Required: true
	Names []string `json:"names"`

	// Image used by the container.
	//
	// Required: true
	Image string `json:"image"`

	// ImageID is the ID of the image.
	//
	// Required: true
	ImageID string `json:"imageId"`

	// Command running in the container.
	//
	// Required: true
	Command string `json:"command"`

	// Created is the Unix timestamp when the container was created.
	//
	// Required: true
	Created int64 `json:"created"`

	// Ports exposed by the container.
	//
	// Required: true
	Ports []Port `json:"ports"`

	// Labels contains user-defined metadata.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// State is the current state of the container.
	//
	// Required: true
	State string `json:"state"`

	// Status provides a human-readable status.
	//
	// Required: true
	Status string `json:"status"`

	// HostConfig contains host configuration.
	//
	// Required: true
	HostConfig HostConfig `json:"hostConfig"`

	// NetworkSettings contains network configuration.
	//
	// Required: true
	NetworkSettings NetworkSettings `json:"networkSettings"`

	// Mounts lists volume mounts.
	//
	// Required: true
	Mounts []Mount `json:"mounts"`
}

// Details represents detailed container information.
type Details struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the container.
	//
	// Required: true
	Name string `json:"name"`

	// Image used by the container.
	//
	// Required: true
	Image string `json:"image"`

	// ImageID is the ID of the image.
	//
	// Required: true
	ImageID string `json:"imageId"`

	// Created is when the container was created.
	//
	// Required: true
	Created string `json:"created"`

	// State contains the container's current state.
	//
	// Required: true
	State State `json:"state"`

	// Config contains container configuration.
	//
	// Required: true
	Config Config `json:"config"`

	// HostConfig contains host-level configuration.
	//
	// Required: true
	HostConfig HostConfig `json:"hostConfig"`

	// NetworkSettings contains network configuration.
	//
	// Required: true
	NetworkSettings NetworkSettings `json:"networkSettings"`

	// Ports exposed by the container.
	//
	// Required: true
	Ports []Port `json:"ports"`

	// Mounts lists volume mounts.
	//
	// Required: true
	Mounts []Mount `json:"mounts"`

	// Labels contains user-defined metadata.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty"`
}

// Created represents a newly created container.
type Created struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the container.
	//
	// Required: true
	Name string `json:"name"`

	// Image used by the container.
	//
	// Required: true
	Image string `json:"image"`

	// Status of the container.
	//
	// Required: true
	Status string `json:"status"`

	// Created is when the container was created.
	//
	// Required: true
	Created string `json:"created"`
}

// NewSummary creates a Summary from a docker container.Summary.
func NewSummary(c container.Summary) Summary {
	ports := make([]Port, 0, len(c.Ports))
	for _, p := range c.Ports {
		ports = append(ports, Port{
			IP:          p.IP,
			PrivatePort: int(p.PrivatePort),
			PublicPort:  int(p.PublicPort),
			Type:        p.Type,
		})
	}

	mounts := make([]Mount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, Mount{
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

	networks := map[string]NetworkEndpoint{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	return Summary{
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
		HostConfig: HostConfig{
			NetworkMode: c.HostConfig.NetworkMode,
		},
		NetworkSettings: NetworkSettings{
			Networks: networks,
		},
		Mounts: mounts,
	}
}

// NewDetails creates a Details from a docker container.InspectResponse.
func NewDetails(c *container.InspectResponse) Details {
	ports := make([]Port, 0)
	if c.NetworkSettings != nil && c.NetworkSettings.Ports != nil {
		for p, bindings := range c.NetworkSettings.Ports {
			privatePort, _ := strconv.Atoi(p.Port())
			typ := string(p.Proto())

			// When no host bindings exist, still include the private port
			if len(bindings) == 0 {
				ports = append(ports, Port{
					PrivatePort: privatePort,
					Type:        typ,
				})
				continue
			}
			for _, b := range bindings {
				pub, _ := strconv.Atoi(b.HostPort)
				ports = append(ports, Port{
					IP:          b.HostIP,
					PrivatePort: privatePort,
					PublicPort:  pub,
					Type:        typ,
				})
			}
		}
	}

	mounts := make([]Mount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, Mount{
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

	networks := map[string]NetworkEndpoint{}
	if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
		for name, n := range c.NetworkSettings.Networks {
			networks[name] = mapEndpointSettings(n)
		}
	}

	var host HostConfig
	if c.HostConfig != nil {
		host = HostConfig{
			RestartPolicy: string(c.HostConfig.RestartPolicy.Name),
			Privileged:    c.HostConfig.Privileged,
			AutoRemove:    c.HostConfig.AutoRemove,
			NanoCPUs:      c.HostConfig.NanoCPUs,
			Memory:        c.HostConfig.Memory,
		}
	}

	var cfg Config
	labels := map[string]string{}
	imageName := ""
	if c.Config != nil {
		cfg = Config{
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

	state := State{}
	if c.State != nil {
		state = State{
			Status:     c.State.Status,
			Running:    c.State.Running,
			ExitCode:   c.State.ExitCode,
			StartedAt:  c.State.StartedAt,
			FinishedAt: c.State.FinishedAt,
		}
	}

	return Details{
		ID:         c.ID,
		Name:       name,
		Image:      imageName,
		ImageID:    c.Image,
		Created:    c.Created,
		State:      state,
		Config:     cfg,
		HostConfig: host,
		NetworkSettings: NetworkSettings{
			Networks: networks,
		},
		Ports:  ports,
		Mounts: mounts,
		Labels: labels,
	}
}

func mapEndpointSettings(n *network.EndpointSettings) NetworkEndpoint {
	if n == nil {
		return NetworkEndpoint{}
	}

	var driverOpts map[string]string
	if n.DriverOpts != nil {
		driverOpts = n.DriverOpts
	}

	return NetworkEndpoint{
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

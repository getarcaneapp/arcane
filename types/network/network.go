package network

import (
	"time"

	"github.com/docker/docker/api/types/network"
)

type Summary struct {
	// ID is the unique identifier of the network.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the network.
	//
	// Required: true
	Name string `json:"name"`

	// Driver is the network driver used.
	//
	// Required: true
	Driver string `json:"driver"`

	// Scope of the network (local, global, etc).
	//
	// Required: true
	Scope string `json:"scope"`

	// Created is the time when the network was created.
	//
	// Required: true
	Created time.Time `json:"created"`

	// Options contains driver-specific options.
	//
	// Required: true
	Options map[string]string `json:"options"`

	// Labels contains user-defined metadata for the network.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// InUse indicates if the network is currently in use by a container.
	//
	// Required: true
	InUse bool `json:"inUse"`

	// IsDefault indicates if this is a default network.
	//
	// Required: true
	IsDefault bool `json:"isDefault"`
}

type ContainerEndpoint struct {
	// ID is the unique identifier of the container.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the container.
	//
	// Required: true
	Name string `json:"name"`

	// EndpointID is the unique identifier of the endpoint.
	//
	// Required: true
	EndpointID string `json:"endpointId"`

	// IPv4Address is the IPv4 address of the container.
	//
	// Required: true
	IPv4Address string `json:"ipv4Address"`

	// IPv6Address is the IPv6 address of the container.
	//
	// Required: true
	IPv6Address string `json:"ipv6Address"`

	// MacAddress is the MAC address of the container.
	//
	// Required: true
	MacAddress string `json:"macAddress"`
}

type UsageCounts struct {
	// Inuse is the number of networks currently in use.
	//
	// Required: true
	Inuse int `json:"inuse"`

	// Unused is the number of networks not in use.
	//
	// Required: true
	Unused int `json:"unused"`

	// Total is the total number of networks.
	//
	// Required: true
	Total int `json:"total"`
}

type Inspect struct {
	// ID is the unique identifier of the network.
	//
	// Required: true
	ID string `json:"id"`

	// Name of the network.
	//
	// Required: true
	Name string `json:"name"`

	// Driver is the network driver used.
	//
	// Required: true
	Driver string `json:"driver"`

	// Scope of the network (local, global, etc).
	//
	// Required: true
	Scope string `json:"scope"`

	// Created is the time when the network was created.
	//
	// Required: true
	Created time.Time `json:"created"`

	// EnableIPv4 represents whether IPv4 is enabled.
	EnableIPv4 bool `json:"enableIPv4"`

	// EnableIPv6 represents whether IPv6 is enabled.
	EnableIPv6 bool `json:"enableIPv6"`

	// IPAM contains IP address management configuration.
	//
	// Required: true
	IPAM network.IPAM `json:"ipam"`

	// Internal indicates if the network is internal.
	//
	// Required: true
	Internal bool `json:"internal"`

	// Attachable indicates if the network is attachable.
	//
	// Required: true
	Attachable bool `json:"attachable"`

	// Ingress indicates if the network is an ingress network.
	//
	// Required: true
	Ingress bool `json:"ingress"`

	// ConfigFrom specifies the source which will provide the configuration for this network.
	ConfigFrom network.ConfigReference `json:"configFrom"`

	// ConfigOnly networks are place-holder networks.
	ConfigOnly bool `json:"configOnly"`

	// Containers is a map of containers connected to this network.
	//
	// Required: true
	Containers map[string]network.EndpointResource `json:"containers"`

	// Options contains driver-specific options.
	//
	// Required: true
	Options map[string]string `json:"options"`

	// Labels contains user-defined metadata for the network.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// Peers is the list of peer nodes for an overlay network.
	Peers []network.PeerInfo `json:"peers,omitempty"`

	// Services contains service info.
	Services map[string]network.ServiceInfo `json:"services,omitempty"`

	// ContainersList is a sorted list of containers connected to this network.
	//
	// Required: true
	ContainersList []ContainerEndpoint `json:"containersList"`
}

type CreateResponse struct {
	// ID is the unique identifier of the created network.
	//
	// Required: true
	ID string `json:"id"`

	// Warning contains any warning messages from the network creation.
	//
	// Required: false
	Warning string `json:"warning,omitempty"`
}

// CreateRequest contains the parameters for creating a network.
type CreateRequest struct {
	// Name is the name of the network to create.
	//
	// Required: true
	Name string `json:"name" minLength:"1" doc:"Name of the network"`

	// Options contains network creation options.
	//
	// Required: false
	Options CreateOptions `json:"options" doc:"Network creation options"`
}

// IPAMConfig contains IP address management configuration for a subnet.
type IPAMConfig struct {
	Subnet     string            `json:"subnet,omitempty"`
	Gateway    string            `json:"gateway,omitempty"`
	IPRange    string            `json:"ipRange,omitempty"`
	AuxAddress map[string]string `json:"auxAddress,omitempty"`
}

// IPAM contains IP Address Management configuration.
type IPAM struct {
	Driver  string            `json:"driver,omitempty"`
	Options map[string]string `json:"options,omitempty"`
	Config  []IPAMConfig      `json:"config,omitempty"`
}

// CreateOptions contains options for creating a network.
type CreateOptions struct {
	// Driver is the network driver to use (e.g., bridge, overlay).
	Driver string `json:"driver,omitempty" doc:"Network driver (e.g., bridge, overlay)"`

	// CheckDuplicate requests daemon to check for networks with same name.
	CheckDuplicate bool `json:"checkDuplicate,omitempty" doc:"Check for duplicate network names"`

	// Internal restricts external access to the network.
	Internal bool `json:"internal,omitempty" doc:"Restrict external access to the network"`

	// Attachable allows manual container attachment in swarm mode.
	Attachable bool `json:"attachable,omitempty" doc:"Allow manual container attachment"`

	// Ingress enables routing-mesh for swarm cluster.
	Ingress bool `json:"ingress,omitempty" doc:"Enable routing-mesh for swarm cluster"`

	// IPAM configuration for the network.
	IPAM *IPAM `json:"ipam,omitempty" doc:"IP Address Management configuration"`

	// EnableIPv6 enables IPv6 networking.
	EnableIPv6 bool `json:"enableIPv6,omitempty" doc:"Enable IPv6 networking"`

	// Options are driver-specific options.
	Options map[string]string `json:"options,omitempty" doc:"Driver-specific options"`

	// Labels are user-defined metadata.
	Labels map[string]string `json:"labels,omitempty" doc:"User-defined labels"`
}

// ToDockerCreateOptions converts to Docker SDK CreateOptions.
func (o CreateOptions) ToDockerCreateOptions() network.CreateOptions {
	var enableIPv6 *bool
	if o.EnableIPv6 {
		enableIPv6 = &o.EnableIPv6
	}

	opts := network.CreateOptions{
		Driver:     o.Driver,
		Internal:   o.Internal,
		Attachable: o.Attachable,
		Ingress:    o.Ingress,
		EnableIPv6: enableIPv6,
		Options:    o.Options,
		Labels:     o.Labels,
	}

	// Convert IPAM if present
	if o.IPAM != nil {
		dockerIPAM := &network.IPAM{
			Driver:  o.IPAM.Driver,
			Options: o.IPAM.Options,
		}
		for _, cfg := range o.IPAM.Config {
			dockerIPAM.Config = append(dockerIPAM.Config, network.IPAMConfig{
				Subnet:     cfg.Subnet,
				Gateway:    cfg.Gateway,
				IPRange:    cfg.IPRange,
				AuxAddress: cfg.AuxAddress,
			})
		}
		opts.IPAM = dockerIPAM
	}

	return opts
}

type PruneReport struct {
	// NetworksDeleted is a list of network IDs that were deleted.
	//
	// Required: true
	NetworksDeleted []string `json:"networksDeleted"`

	// SpaceReclaimed is the amount of space reclaimed in bytes.
	//
	// Required: true
	SpaceReclaimed uint64 `json:"spaceReclaimed"`
}

// NewSummary creates a Summary from a docker network.Summary, calculating InUse and IsDefault fields.
func NewSummary(s network.Summary) Summary {
	return Summary{
		ID:      s.ID,
		Name:    s.Name,
		Driver:  s.Driver,
		Scope:   s.Scope,
		Created: s.Created,
		Options: s.Options,
		Labels:  s.Labels,
		// InUse is set to true if the network has any connected containers, false otherwise.
		InUse: len(s.Containers) > 0,
		// IsDefault is set to true if the network is one of the default Docker networks
		// (bridge, host, or none), false otherwise.
		IsDefault: s.Name == "bridge" || s.Name == "host" || s.Name == "none",
	}
}

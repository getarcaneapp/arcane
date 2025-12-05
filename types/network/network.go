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

type UsageCounts struct {
	// Inuse is the number of networks currently in use.
	//
	// Required: true
	Inuse int `json:"networksInuse"`

	// Unused is the number of networks not in use.
	//
	// Required: true
	Unused int `json:"networksUnused"`

	// Total is the total number of networks.
	//
	// Required: true
	Total int `json:"totalNetworks"`
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

	// Options contains driver-specific options.
	//
	// Required: true
	Options map[string]string `json:"options"`

	// Labels contains user-defined metadata for the network.
	//
	// Required: true
	Labels map[string]string `json:"labels"`

	// Containers is a map of containers connected to this network.
	//
	// Required: true
	Containers map[string]network.EndpointResource `json:"containers"`

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

// CreateOptions contains options for creating a network.
// This matches the frontend's expected format.
type CreateOptions struct {
	// Driver is the network driver to use (e.g., bridge, overlay).
	Driver string `json:"Driver,omitempty"`

	// CheckDuplicate requests daemon to check for networks with same name.
	CheckDuplicate bool `json:"CheckDuplicate,omitempty"`

	// Internal restricts external access to the network.
	Internal bool `json:"Internal,omitempty"`

	// Attachable allows manual container attachment in swarm mode.
	Attachable bool `json:"Attachable,omitempty"`

	// Ingress enables routing-mesh for swarm cluster.
	Ingress bool `json:"Ingress,omitempty"`

	// IPAM configuration for the network.
	IPAM *network.IPAM `json:"IPAM,omitempty"`

	// EnableIPv6 enables IPv6 networking.
	EnableIPv6 bool `json:"EnableIPv6,omitempty"`

	// Options are driver-specific options.
	Options map[string]string `json:"Options,omitempty"`

	// Labels are user-defined metadata.
	Labels map[string]string `json:"Labels,omitempty"`
}

// ToDockerCreateOptions converts to Docker SDK CreateOptions.
func (o CreateOptions) ToDockerCreateOptions() network.CreateOptions {
	var enableIPv6 *bool
	if o.EnableIPv6 {
		enableIPv6 = &o.EnableIPv6
	}

	return network.CreateOptions{
		Driver:     o.Driver,
		Internal:   o.Internal,
		Attachable: o.Attachable,
		Ingress:    o.Ingress,
		IPAM:       o.IPAM,
		EnableIPv6: enableIPv6,
		Options:    o.Options,
		Labels:     o.Labels,
	}
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

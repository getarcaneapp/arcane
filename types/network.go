package types

import (
	"time"

	"github.com/moby/moby/api/types/network"
)

type (
	NetworkSummary struct {
		EnvironmentTag
		ID        string            `json:"id"`
		Name      string            `json:"name"`
		Driver    string            `json:"driver"`
		Scope     string            `json:"scope"`
		Created   time.Time         `json:"created"`
		Options   map[string]string `json:"options"`
		Labels    map[string]string `json:"labels"`
		IPAM      NetworkIPAM       `json:"ipam"`
		InUse     bool              `json:"inUse"`
		IsDefault bool              `json:"isDefault"`
	}

	NetworkContainerEndpoint struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		EndpointID  string `json:"endpointId"`
		IPv4Address string `json:"ipv4Address"`
		IPv6Address string `json:"ipv6Address"`
		MacAddress  string `json:"macAddress"`
	}

	NetworkUsageCounts struct {
		Inuse  int `json:"inuse"`
		Unused int `json:"unused"`
		Total  int `json:"total"`
	}

	NetworkInspect struct {
		ID             string                              `json:"id"`
		Name           string                              `json:"name"`
		Driver         string                              `json:"driver"`
		Scope          string                              `json:"scope"`
		Created        time.Time                           `json:"created"`
		IPAM           NetworkIPAM                         `json:"ipam"`
		ConfigFrom     network.ConfigReference             `json:"configFrom"`
		Containers     map[string]network.EndpointResource `json:"containers"`
		Options        map[string]string                   `json:"options"`
		Labels         map[string]string                   `json:"labels"`
		Peers          []network.PeerInfo                  `json:"peers,omitempty"`
		Services       map[string]network.ServiceInfo      `json:"services,omitempty"`
		ContainersList []NetworkContainerEndpoint          `json:"containersList"`
		EnableIPv4     bool                                `json:"enableIPv4"`
		EnableIPv6     bool                                `json:"enableIPv6"`
		Internal       bool                                `json:"internal"`
		Attachable     bool                                `json:"attachable"`
		Ingress        bool                                `json:"ingress"`
		ConfigOnly     bool                                `json:"configOnly"`
	}

	NetworkCreateResponse struct {
		ActivityID *string `json:"activityId,omitempty"`
		ID         string  `json:"id"`
		Warning    string  `json:"warning,omitempty"`
	}

	Network struct {
		Name    string               `json:"name" required:"false" doc:"Name of the network"`
		Options NetworkCreateOptions `json:"options" required:"false" doc:"Network creation options"`
	}

	NetworkIPAMConfig struct {
		AuxAddress map[string]string `json:"auxAddress,omitempty"`
		Subnet     string            `json:"subnet,omitempty"`
		Gateway    string            `json:"gateway,omitempty"`
		IPRange    string            `json:"ipRange,omitempty"`
	}

	NetworkIPAM struct {
		Driver  string              `json:"driver,omitempty"`
		Options map[string]string   `json:"options,omitempty"`
		Config  []NetworkIPAMConfig `json:"config,omitempty"`
	}

	NetworkCreateOptions struct {
		IPAM           *NetworkIPAM      `json:"ipam,omitempty" doc:"IP Address Management configuration"`
		Options        map[string]string `json:"options,omitempty" doc:"Driver-specific options"`
		Labels         map[string]string `json:"labels,omitempty" doc:"User-defined labels"`
		Driver         string            `json:"driver,omitempty" doc:"Network driver (e.g., bridge, overlay)"`
		CheckDuplicate bool              `json:"checkDuplicate,omitempty" doc:"Check for duplicate network names"`
		Internal       bool              `json:"internal,omitempty" doc:"Restrict external access to the network"`
		Attachable     bool              `json:"attachable,omitempty" doc:"Allow manual container attachment"`
		Ingress        bool              `json:"ingress,omitempty" doc:"Enable routing-mesh for swarm cluster"`
		EnableIPv6     bool              `json:"enableIPv6,omitempty" doc:"Enable IPv6 networking"`
	}

	NetworkContainerRequest struct {
		ContainerID string   `json:"containerId" minLength:"1" doc:"Container ID or name"`
		IPv4Address string   `json:"ipv4Address,omitempty" doc:"Static IPv4 address (connect only)"`
		IPv6Address string   `json:"ipv6Address,omitempty" doc:"Static IPv6 address (connect only)"`
		Aliases     []string `json:"aliases,omitempty" doc:"Network-scoped aliases (connect only)"`
		Force       bool     `json:"force,omitempty" doc:"Force the disconnect (disconnect only)"`
	}

	NetworkPruneReport struct {
		ActivityID      *string  `json:"activityId,omitempty"`
		NetworksDeleted []string `json:"networksDeleted"`

		SpaceReclaimed uint64 `json:"spaceReclaimed"`
	}

	NetworkTopologyNodeType string

	NetworkTopologyNodeMetadata struct {
		Driver    string `json:"driver,omitempty"`
		Scope     string `json:"scope,omitempty"`
		Status    string `json:"status,omitempty"`
		Image     string `json:"image,omitempty"`
		IsDefault bool   `json:"isDefault,omitempty"`
	}

	NetworkTopologyNode struct {
		EnvironmentTag
		ID       string                      `json:"id"`
		Name     string                      `json:"name"`
		Type     NetworkTopologyNodeType     `json:"type"`
		Metadata NetworkTopologyNodeMetadata `json:"metadata"`
	}

	NetworkTopologyEdge struct {
		ID          string `json:"id"`
		Source      string `json:"source"`
		Target      string `json:"target"`
		IPv4Address string `json:"ipv4Address,omitempty"`
		IPv6Address string `json:"ipv6Address,omitempty"`
	}

	NetworkTopology struct {
		Nodes []NetworkTopologyNode `json:"nodes"`
		Edges []NetworkTopologyEdge `json:"edges"`
	}
)

const (
	NetworkTopologyNodeTypeNetwork   NetworkTopologyNodeType = "network"
	NetworkTopologyNodeTypeContainer NetworkTopologyNodeType = "container"
)

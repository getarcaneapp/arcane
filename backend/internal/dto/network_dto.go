package dto

import (
	"time"

	"github.com/docker/docker/api/types/network"
)

type NetworkSummaryDto struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Driver    string            `json:"driver"`
	Scope     string            `json:"scope"`
	Created   time.Time         `json:"created"`
	Options   map[string]string `json:"options"`
	Labels    map[string]string `json:"labels"`
	InUse     bool              `json:"inUse"`
	IsDefault bool              `json:"isDefault"`
}

type NetworkUsageCounts struct {
	Inuse  int `json:"networksInuse"`
	Unused int `json:"networksUnused"`
	Total  int `json:"totalNetworks"`
}

// EndpointResourceDto represents a container endpoint in a network
type EndpointResourceDto struct {
	Name        string `json:"name"`
	EndpointID  string `json:"endpointId"`
	MacAddress  string `json:"macAddress"`
	IPv4Address string `json:"ipv4Address"`
	IPv6Address string `json:"ipv6Address"`
}

// IPAMConfigDto represents IPAM pool configuration
type IPAMConfigDto struct {
	Subnet     string            `json:"subnet"`
	IPRange    string            `json:"ipRange"`
	Gateway    string            `json:"gateway"`
	AuxAddress map[string]string `json:"auxAddress"`
}

// IPAMDto represents IP Address Management configuration
type IPAMDto struct {
	Driver  string          `json:"driver"`
	Options map[string]string `json:"options"`
	Config  []IPAMConfigDto `json:"config"`
}

type NetworkInspectDto struct {
	ID         string                         `json:"id"`
	Name       string                         `json:"name"`
	Driver     string                         `json:"driver"`
	Scope      string                         `json:"scope"`
	Created    time.Time                      `json:"created"`
	Options    map[string]string              `json:"options"`
	Labels     map[string]string              `json:"labels"`
	Containers map[string]EndpointResourceDto `json:"containers"`
	IPAM       IPAMDto                        `json:"ipam"`
	Internal   bool                           `json:"internal"`
	Attachable bool                           `json:"attachable"`
	Ingress    bool                           `json:"ingress"`
}

// NewNetworkInspectDto converts a Docker network inspect response to our DTO
func NewNetworkInspectDto(n network.Inspect) NetworkInspectDto {
	containers := make(map[string]EndpointResourceDto)
	for id, ep := range n.Containers {
		containers[id] = EndpointResourceDto{
			Name:        ep.Name,
			EndpointID:  ep.EndpointID,
			MacAddress:  ep.MacAddress,
			IPv4Address: ep.IPv4Address,
			IPv6Address: ep.IPv6Address,
		}
	}

	ipamConfigs := make([]IPAMConfigDto, len(n.IPAM.Config))
	for i, cfg := range n.IPAM.Config {
		ipamConfigs[i] = IPAMConfigDto{
			Subnet:     cfg.Subnet,
			IPRange:    cfg.IPRange,
			Gateway:    cfg.Gateway,
			AuxAddress: cfg.AuxAddress,
		}
	}

	return NetworkInspectDto{
		ID:         n.ID,
		Name:       n.Name,
		Driver:     n.Driver,
		Scope:      n.Scope,
		Created:    n.Created,
		Options:    n.Options,
		Labels:     n.Labels,
		Containers: containers,
		IPAM: IPAMDto{
			Driver:  n.IPAM.Driver,
			Options: n.IPAM.Options,
			Config:  ipamConfigs,
		},
		Internal:   n.Internal,
		Attachable: n.Attachable,
		Ingress:    n.Ingress,
	}
}

type NetworkCreateResponseDto struct {
	ID      string `json:"id"`
	Warning string `json:"warning,omitempty"`
}

type NetworkPruneReportDto struct {
	NetworksDeleted []string `json:"networksDeleted"`
	SpaceReclaimed  uint64   `json:"spaceReclaimed"`
}

func NewNetworkSummaryDto(s network.Summary) NetworkSummaryDto {
	iu := len(s.Containers) > 0

	return NetworkSummaryDto{
		ID:      s.ID,
		Name:    s.Name,
		Driver:  s.Driver,
		Scope:   s.Scope,
		Created: s.Created,
		Options: s.Options,
		Labels:  s.Labels,
		InUse:   iu,
	}
}

package swarm

import (
	"fmt"
	"time"

	"github.com/docker/docker/api/types/swarm"
)

type NodeSummary struct {
	// ID is the unique identifier of the node.
	//
	// Required: true
	ID string `json:"id"`

	// Hostname is the node hostname.
	//
	// Required: true
	Hostname string `json:"hostname"`

	// Role indicates whether the node is a manager or worker.
	//
	// Required: true
	Role string `json:"role"`

	// Availability indicates if the node is active, paused, or drained.
	//
	// Required: true
	Availability string `json:"availability"`

	// Status is the node readiness state.
	//
	// Required: true
	Status string `json:"status"`

	// Address is the node address.
	//
	// Required: false
	Address string `json:"address,omitempty"`

	// ManagerStatus is the manager status string if applicable.
	//
	// Required: false
	ManagerStatus string `json:"managerStatus,omitempty"`

	// Reachability is the manager reachability if applicable.
	//
	// Required: false
	Reachability string `json:"reachability,omitempty"`

	// Labels contains node labels.
	//
	// Required: false
	Labels map[string]string `json:"labels,omitempty"`

	// EngineVersion is the Docker engine version.
	//
	// Required: false
	EngineVersion string `json:"engineVersion,omitempty"`

	// Platform is the node platform string.
	//
	// Required: false
	Platform string `json:"platform,omitempty"`

	// CreatedAt is when the node was created.
	//
	// Required: true
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the node was last updated.
	//
	// Required: true
	UpdatedAt time.Time `json:"updatedAt"`
}

func NewNodeSummary(node swarm.Node) NodeSummary {
	managerStatus := ""
	reachability := ""
	if node.ManagerStatus != nil {
		if node.ManagerStatus.Leader {
			managerStatus = "leader"
		} else {
			managerStatus = "manager"
		}
		reachability = string(node.ManagerStatus.Reachability)
	}

	platform := ""
	if node.Description.Platform.OS != "" || node.Description.Platform.Architecture != "" {
		platform = fmt.Sprintf("%s/%s", node.Description.Platform.OS, node.Description.Platform.Architecture)
	}

	return NodeSummary{
		ID:            node.ID,
		Hostname:      node.Description.Hostname,
		Role:          string(node.Spec.Role),
		Availability:  string(node.Spec.Availability),
		Status:        string(node.Status.State),
		Address:       node.Status.Addr,
		ManagerStatus: managerStatus,
		Reachability:  reachability,
		Labels:        node.Spec.Labels,
		EngineVersion: node.Description.Engine.EngineVersion,
		Platform:      platform,
		CreatedAt:     node.CreatedAt,
		UpdatedAt:     node.UpdatedAt,
	}
}

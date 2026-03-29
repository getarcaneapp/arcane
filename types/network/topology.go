package network

type TopologyNodeType string

const (
	TopologyNodeTypeNetwork   TopologyNodeType = "network"
	TopologyNodeTypeContainer TopologyNodeType = "container"
)

type TopologyNodeMetadata struct {
	Driver    string `json:"driver,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Status    string `json:"status,omitempty"`
	Image     string `json:"image,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

type TopologyNode struct {
	// ID is the unique node identifier.
	//
	// Required: true
	ID string `json:"id"`

	// Name is the display label for the node.
	//
	// Required: true
	Name string `json:"name"`

	// Type distinguishes network and container nodes.
	//
	// Required: true
	Type TopologyNodeType `json:"type"`

	// Metadata carries additional node context used by the UI.
	//
	// Required: true
	Metadata TopologyNodeMetadata `json:"metadata"`
}

type TopologyEdge struct {
	// ID is the unique edge identifier.
	//
	// Required: true
	ID string `json:"id"`

	// Source is the network node ID.
	//
	// Required: true
	Source string `json:"source"`

	// Target is the container node ID.
	//
	// Required: true
	Target string `json:"target"`

	// IPv4Address is the assigned IPv4 address on the network.
	//
	// Required: false
	IPv4Address string `json:"ipv4Address,omitempty"`

	// IPv6Address is the assigned IPv6 address on the network.
	//
	// Required: false
	IPv6Address string `json:"ipv6Address,omitempty"`
}

type Topology struct {
	// Nodes contains all topology nodes.
	//
	// Required: true
	Nodes []TopologyNode `json:"nodes"`

	// Edges contains all topology edges.
	//
	// Required: true
	Edges []TopologyEdge `json:"edges"`
}

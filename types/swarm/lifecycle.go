package swarm

import (
	"encoding/json"

	"github.com/moby/moby/api/types/swarm"
)

type SwarmInitRequest struct {
	ListenAddr       string                 `json:"listenAddr,omitempty"`
	AdvertiseAddr    string                 `json:"advertiseAddr,omitempty"`
	DataPathAddr     string                 `json:"dataPathAddr,omitempty"`
	DataPathPort     uint32                 `json:"dataPathPort,omitempty"`
	ForceNewCluster  bool                   `json:"forceNewCluster,omitempty"`
	Spec             json.RawMessage        `json:"spec" doc:"Swarm specification"`
	AutoLockManagers bool                   `json:"autoLockManagers,omitempty"`
	Availability     swarm.NodeAvailability `json:"availability,omitempty"`
	DefaultAddrPool  []string               `json:"defaultAddrPool,omitempty"`
	SubnetSize       uint32                 `json:"subnetSize,omitempty"`
}

type SwarmInitResponse struct {
	NodeID string `json:"nodeId"`
}

type SwarmJoinRequest struct {
	ListenAddr    string                 `json:"listenAddr,omitempty"`
	AdvertiseAddr string                 `json:"advertiseAddr,omitempty"`
	DataPathAddr  string                 `json:"dataPathAddr,omitempty"`
	RemoteAddrs   []string               `json:"remoteAddrs"`
	JoinToken     string                 `json:"joinToken"`
	Availability  swarm.NodeAvailability `json:"availability,omitempty"`
}

type SwarmLeaveRequest struct {
	Force bool `json:"force,omitempty"`
}

type SwarmUnlockRequest struct {
	Key string `json:"key"`
}

type SwarmUnlockKeyResponse struct {
	UnlockKey string `json:"unlockKey"`
}

type SwarmJoinTokensResponse struct {
	Worker  string `json:"worker"`
	Manager string `json:"manager"`
}

type SwarmRotateJoinTokensRequest struct {
	RotateWorkerToken  bool `json:"rotateWorkerToken,omitempty"`
	RotateManagerToken bool `json:"rotateManagerToken,omitempty"`
}

type SwarmUpdateRequest struct {
	Version                uint64          `json:"version,omitempty"`
	Spec                   json.RawMessage `json:"spec" doc:"Updated swarm specification"`
	RotateWorkerToken      bool            `json:"rotateWorkerToken,omitempty"`
	RotateManagerToken     bool            `json:"rotateManagerToken,omitempty"`
	RotateManagerUnlockKey bool            `json:"rotateManagerUnlockKey,omitempty"`
}

type NodeAgentReconcileRequest struct{}

type NodeAgentReconcileResult struct {
	NodeID        string               `json:"nodeId"`
	State         NodeAgentState       `json:"state"`
	EnvironmentID *string              `json:"environmentId,omitempty"`
	Candidates    []NodeAgentCandidate `json:"candidates,omitempty"`
}

type NodeAgentReconcileResponse struct {
	Results []NodeAgentReconcileResult `json:"results"`
}

type NodeAgentBindingRequest struct {
	EnvironmentID     string `json:"environmentId"`
	Rebind            bool   `json:"rebind,omitempty"`
	ReplaceDeployment bool   `json:"replaceDeployment,omitempty"`
}

type SwarmJoinEnvironmentRole string

const (
	SwarmJoinEnvironmentRoleWorker  SwarmJoinEnvironmentRole = "worker"
	SwarmJoinEnvironmentRoleManager SwarmJoinEnvironmentRole = "manager"
)

type SwarmJoinEnvironmentResultState string

const (
	SwarmJoinEnvironmentResultJoined           SwarmJoinEnvironmentResultState = "joined"
	SwarmJoinEnvironmentResultAlreadyMember    SwarmJoinEnvironmentResultState = "already_member"
	SwarmJoinEnvironmentResultJoinedUnverified SwarmJoinEnvironmentResultState = "joined_unverified"
	SwarmJoinEnvironmentResultFailed           SwarmJoinEnvironmentResultState = "failed"
)

type SwarmJoinCandidate struct {
	EnvironmentID   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	EnvironmentType string `json:"environmentType"`
	Status          string `json:"status"`
}

type SwarmJoinEnvironmentTarget struct {
	EnvironmentID string                   `json:"environmentId"`
	Role          SwarmJoinEnvironmentRole `json:"role"`
	Availability  swarm.NodeAvailability   `json:"availability,omitempty"`
	ListenAddr    string                   `json:"listenAddr,omitempty"`
	AdvertiseAddr string                   `json:"advertiseAddr,omitempty"`
	DataPathAddr  string                   `json:"dataPathAddr,omitempty"`
}

type SwarmJoinEnvironmentsRequest struct {
	RemoteAddrs []string                     `json:"remoteAddrs"`
	Targets     []SwarmJoinEnvironmentTarget `json:"targets"`
}

type SwarmJoinEnvironmentResult struct {
	EnvironmentID string                          `json:"environmentId"`
	State         SwarmJoinEnvironmentResultState `json:"state"`
	NodeID        *string                         `json:"nodeId,omitempty"`
	Error         *string                         `json:"error,omitempty"`
}

type SwarmJoinEnvironmentsResponse struct {
	Results []SwarmJoinEnvironmentResult `json:"results"`
}

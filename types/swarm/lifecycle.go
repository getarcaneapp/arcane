package swarm

import (
	"github.com/moby/moby/api/types/swarm"
)

type SwarmInitRequest struct {
	ListenAddr       string                 `json:"listenAddr,omitempty"`
	AdvertiseAddr    string                 `json:"advertiseAddr,omitempty"`
	DataPathAddr     string                 `json:"dataPathAddr,omitempty"`
	DataPathPort     uint32                 `json:"dataPathPort,omitempty"`
	ForceNewCluster  bool                   `json:"forceNewCluster,omitempty"`
	Spec             swarm.Spec             `json:"spec"`
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
	Version                uint64     `json:"version,omitempty"`
	Spec                   swarm.Spec `json:"spec"`
	RotateWorkerToken      bool       `json:"rotateWorkerToken,omitempty"`
	RotateManagerToken     bool       `json:"rotateManagerToken,omitempty"`
	RotateManagerUnlockKey bool       `json:"rotateManagerUnlockKey,omitempty"`
}

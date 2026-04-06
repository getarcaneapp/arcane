package services

import (
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	environmenttypes "github.com/getarcaneapp/arcane/types/environment"
)

// ApplyEnvironmentRuntimeState normalizes edge environment runtime status using
// in-memory tunnel and poll registries without mutating persisted state.
func ApplyEnvironmentRuntimeState(env *environmenttypes.Environment) {
	if env == nil || !env.IsEdge {
		return
	}

	connected := false
	env.Connected = &connected
	env.ConnectedAt = nil
	env.LastHeartbeat = nil
	env.LastPollAt = nil
	env.EdgeTransport = nil

	if pollState, ok := edge.GetPollRuntimeRegistry().Get(env.ID, time.Now()); ok {
		env.LastPollAt = pollState.LastPollAt
	}

	if runtimeState, ok := edge.GetTunnelRuntimeState(env.ID); ok {
		connected = true
		env.Connected = &connected
		env.Status = string(models.EnvironmentStatusOnline)
		env.ConnectedAt = runtimeState.ConnectedAt
		env.LastHeartbeat = runtimeState.LastHeartbeat
		if transport, ok := edge.GetActiveTunnelTransport(env.ID); ok {
			env.EdgeTransport = &transport
		} else if runtimeState.Transport != "" {
			env.EdgeTransport = &runtimeState.Transport
		}
		return
	}

	if env.LastPollAt != nil {
		env.Status = string(models.EnvironmentStatusStandby)
		return
	}

	if env.Status != string(models.EnvironmentStatusPending) {
		env.Status = string(models.EnvironmentStatusOffline)
	}
}

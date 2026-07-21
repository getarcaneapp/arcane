package handlers

import (
	"context"
	"testing"

	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/stretchr/testify/require"
)

func TestRequireEasyJoinManagerPermissionsUsesSwarmJoinOnly(t *testing.T) {
	legacyPermissions := authz.NewPermissionSet()
	legacyPermissions.AddEnv("manager", authz.PermSwarmNodes, authz.PermSwarmUnlock)
	legacyContext := context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, legacyPermissions)
	require.Error(t, requireEasyJoinManagerPermissionsInternal(legacyContext, "manager"))

	joinPermissions := authz.NewPermissionSet()
	joinPermissions.AddEnv("manager", authz.PermSwarmJoin)
	joinContext := context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, joinPermissions)
	require.NoError(t, requireEasyJoinManagerPermissionsInternal(joinContext, "manager"))
}

func TestJoinEnvironmentsRequiresSwarmJoinOnEveryTarget(t *testing.T) {
	permissions := authz.NewPermissionSet()
	permissions.AddEnv("manager", authz.PermSwarmJoin)
	ctx := context.WithValue(context.Background(), humamw.ContextKeyUserPermissions, permissions)
	handler := &SwarmHandler{swarmService: &services.SwarmService{}}

	_, err := handler.JoinEnvironments(ctx, &JoinSwarmEnvironmentsInput{
		EnvironmentID: "manager",
		Body: swarmtypes.SwarmJoinEnvironmentsRequest{
			RemoteAddrs: []string{"manager:2377"},
			Targets: []swarmtypes.SwarmJoinEnvironmentTarget{
				{EnvironmentID: "target", Role: swarmtypes.SwarmJoinEnvironmentRoleWorker},
			},
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "swarm:join permission is required for every target environment")
}

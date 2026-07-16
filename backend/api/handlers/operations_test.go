package handlers

import (
	"encoding/json"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	operationstypes "github.com/getarcaneapp/arcane/types/v2/operations"
	"github.com/stretchr/testify/require"
)

func TestOperationsStreamEventUsesLiveStateContract(t *testing.T) {
	payload, err := json.Marshal(operationstypes.StreamEvent{
		Type:  "update",
		State: &operationstypes.State{Compatibility: operationstypes.CompatibilityCurrent},
	})
	require.NoError(t, err)
	require.Contains(t, string(payload), `"type":"update"`)
	require.Contains(t, string(payload), `"state":`)
	require.NotContains(t, string(payload), `"snapshot"`)
}

func TestOperationsReadPermissionsOnlyIncludeStateCategories(t *testing.T) {
	require.ElementsMatch(t, []string{
		authz.PermImageUpdatesRead,
		authz.PermProjectsList,
		authz.PermProjectsRead,
		authz.PermContainersList,
		authz.PermContainersRead,
		authz.PermVulnsRead,
		authz.PermApiKeysList,
		authz.PermApiKeysRead,
	}, operationsReadPermissionsInternal)
}

func TestOperationsStateFromLegacyDashboardPreservesTotalsWithoutInventingBreakdowns(t *testing.T) {
	state := operationsStateFromLegacyDashboardInternal(&dashboardtypes.Snapshot{
		ActionItems: dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{
			{Kind: dashboardtypes.ActionItemKindStoppedContainers, Count: 4},
			{Kind: dashboardtypes.ActionItemKindImageUpdates, Count: 3},
		}},
	})

	require.Equal(t, operationstypes.CompatibilityLegacy, state.Compatibility)
	require.Equal(t, 4, state.Stopped.Total)
	require.Nil(t, state.Stopped.Projects)
	require.Nil(t, state.Stopped.StandaloneContainers)
	require.Equal(t, 3, state.Updates.Total)
	require.Nil(t, state.Updates.Projects)
	require.Nil(t, state.Updates.StandaloneContainers)
}

func TestFilterOperationsStateOmitsUnauthorizedCategories(t *testing.T) {
	projects := 2
	containers := 3
	vulnerabilities := 5
	keys := 1
	state := &operationstypes.State{
		Compatibility:   operationstypes.CompatibilityCurrent,
		Updates:         &operationstypes.WorkloadCount{Total: 5, Projects: &projects, StandaloneContainers: &containers},
		Stopped:         &operationstypes.WorkloadCount{Total: 5, Projects: &projects, StandaloneContainers: &containers},
		Vulnerabilities: &vulnerabilities,
		ExpiringAPIKeys: &keys,
	}
	permissions := authz.NewPermissionSet()
	permissions.AddEnv("env-1", authz.PermContainersList)

	filterOperationsStateInternal(permissions, "env-1", state)

	require.Nil(t, state.Updates)
	require.NotNil(t, state.Stopped)
	require.Nil(t, state.Stopped.Projects)
	require.Equal(t, 3, *state.Stopped.StandaloneContainers)
	require.Nil(t, state.Vulnerabilities)
	require.Nil(t, state.ExpiringAPIKeys)
}

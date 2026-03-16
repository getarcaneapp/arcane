package services

import (
	"net/netip"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
)

func TestPaginateContainerProjectGroupsKeepsProjectWhole(t *testing.T) {
	items := []containertypes.Summary{
		newGroupedContainerSummary("other-1", "other-1"),
		newGroupedContainerSummary("other-2", "other-2"),
		newGroupedContainerSummary("other-3", "other-3"),
		newGroupedContainerSummary("other-4", "other-4"),
		newGroupedContainerSummary("other-5", "other-5"),
		newGroupedContainerSummary("other-6", "other-6"),
		newGroupedContainerSummary("other-7", "other-7"),
		newGroupedContainerSummary("other-8", "other-8"),
		newGroupedContainerSummary("other-9", "other-9"),
		newGroupedContainerSummary("other-10", "other-10"),
		newGroupedContainerSummary("other-11", "other-11"),
		newGroupedContainerSummary("other-12", "other-12"),
		newGroupedContainerSummary("other-13", "other-13"),
		newGroupedContainerSummary("other-14", "other-14"),
		newGroupedContainerSummary("other-15", "other-15"),
		newGroupedContainerSummary("other-16", "other-16"),
		newGroupedContainerSummary("other-17", "other-17"),
		newGroupedContainerSummary("other-18", "other-18"),
		newGroupedContainerSummary("immich-server", "immich"),
		newGroupedContainerSummary("immich-ml", "immich"),
		newGroupedContainerSummary("immich-redis", "immich"),
		newGroupedContainerSummary("immich-postgres", "immich"),
	}

	groupedItems, resp := paginateContainerProjectGroupsInternal(
		pagination.FilterResult[containertypes.Summary]{Items: items, TotalCount: int64(len(items)), TotalAvailable: int64(len(items))},
		pagination.QueryParams{PaginationParams: pagination.PaginationParams{Start: 0, Limit: 20}},
	)

	require.Len(t, groupedItems, 19)
	require.Equal(t, int64(1), resp.TotalPages)
	require.Equal(t, 1, resp.CurrentPage)
	require.Equal(t, 20, resp.ItemsPerPage)
	require.Equal(t, int64(22), resp.TotalItems)

	projectCounts := make(map[string]int)
	for _, group := range groupedItems {
		projectCounts[group.GroupName] += len(group.Items)
	}

	require.Equal(t, 4, projectCounts["immich"])
	require.Equal(t, 1, projectCounts["other-1"])
	require.Equal(t, 1, projectCounts["other-18"])
}

func TestGroupContainersByProjectUsesNoProjectBucket(t *testing.T) {
	groups := groupContainersByProjectInternal([]containertypes.Summary{
		{ID: "1", Labels: map[string]string{"com.docker.compose.project": "alpha"}},
		{ID: "2", Labels: map[string]string{}},
		{ID: "3", Labels: nil},
	})

	require.Len(t, groups, 2)
	require.Equal(t, "alpha", groups[0].GroupName)
	require.Len(t, groups[0].Items, 1)
	require.Equal(t, containerNoProjectGroup, groups[1].GroupName)
	require.Len(t, groups[1].Items, 2)
	require.Equal(t, containerNoProjectGroup, getContainerProjectNameInternal(groups[1].Items[0]))
	require.Equal(t, containerNoProjectGroup, getContainerProjectNameInternal(groups[1].Items[1]))
}

func TestBuildCleanNetworkingConfigInternalPreservesEndpointSettings(t *testing.T) {
	containerInspect := container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {
					Aliases:    []string{"svc"},
					IPAddress:  netip.MustParseAddr("172.17.0.2"),
					IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: netip.MustParseAddr("172.17.0.5")},
				},
			},
		},
	}

	out := buildCleanNetworkingConfigInternal(containerInspect, "1.44")
	require.NotNil(t, out)
	require.Contains(t, out.EndpointsConfig, "bridge")
	require.Equal(t, []string{"svc"}, out.EndpointsConfig["bridge"].Aliases)
	require.Equal(t, netip.MustParseAddr("172.17.0.2"), out.EndpointsConfig["bridge"].IPAddress)
	require.Nil(t, out.EndpointsConfig["bridge"].IPAMConfig)
}

func newGroupedContainerSummary(name string, project string) containertypes.Summary {
	labels := map[string]string{}
	if project != "" {
		labels["com.docker.compose.project"] = project
	}

	return containertypes.Summary{
		ID:     name,
		Names:  []string{name},
		Labels: labels,
		State:  "running",
	}
}

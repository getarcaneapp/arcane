package container

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"

	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"

	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	imagetypes "github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
		pagination.QueryParams{Start: 0, Limit: 20},
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

func TestPaginateContainerProjectGroupsSelectsRequestedPage(t *testing.T) {
	// With Limit=4 the groups partition into three pages:
	// page 1: proj-a (4), page 2: proj-b (3) + solo-1 (1), page 3: proj-c (2) + solo-2 (1).
	items := []containertypes.Summary{
		newGroupedContainerSummary("a-1", "proj-a"),
		newGroupedContainerSummary("a-2", "proj-a"),
		newGroupedContainerSummary("a-3", "proj-a"),
		newGroupedContainerSummary("a-4", "proj-a"),
		newGroupedContainerSummary("b-1", "proj-b"),
		newGroupedContainerSummary("b-2", "proj-b"),
		newGroupedContainerSummary("b-3", "proj-b"),
		newGroupedContainerSummary("solo-1", "solo-1"),
		newGroupedContainerSummary("c-1", "proj-c"),
		newGroupedContainerSummary("c-2", "proj-c"),
		newGroupedContainerSummary("solo-2", "solo-2"),
	}
	result := pagination.FilterResult[containertypes.Summary]{Items: items, TotalCount: int64(len(items)), TotalAvailable: int64(len(items))}

	pageGroups, resp := paginateContainerProjectGroupsInternal(result, pagination.QueryParams{Start: 4, Limit: 4})

	require.Equal(t, []string{"proj-b", "solo-1"}, groupNamesOf(pageGroups))
	require.Equal(t, 2, resp.CurrentPage)
	require.Equal(t, int64(3), resp.TotalPages)
	require.Equal(t, int64(11), resp.TotalItems)

	pageGroups, resp = paginateContainerProjectGroupsInternal(result, pagination.QueryParams{Start: 400, Limit: 4})

	require.Equal(t, []string{"proj-c", "solo-2"}, groupNamesOf(pageGroups))
	require.Equal(t, 3, resp.CurrentPage)
	require.Equal(t, int64(3), resp.TotalPages)
}

func TestContainerSummaryIconsAppliedAfterGrouping(t *testing.T) {
	service := &ContainerService{}
	dockerContainers := []container.Summary{
		{ID: "app", Names: []string{"/app"}, Labels: map[string]string{
			"com.docker.compose.project": "media",
			projects.ArcaneIconLabel:     "immich",
		}},
		{ID: "db", Names: []string{"/db"}, Labels: map[string]string{
			"com.docker.compose.project": "media",
		}},
	}

	items := service.BuildSummaries(dockerContainers, nil, "", nil)
	for _, item := range items {
		require.Empty(t, item.IconLightURL, "icons must be deferred until after pagination")
	}

	groups, _ := paginateContainerProjectGroupsInternal(
		pagination.FilterResult[containertypes.Summary]{Items: items, TotalCount: int64(len(items)), TotalAvailable: int64(len(items))},
		pagination.QueryParams{Start: 0, Limit: 20},
	)
	for gi := range groups {
		service.ApplySummaryIcons(t.Context(), groups[gi].Items, map[string]projects.ArcaneComposeMetadata{})
	}
	flattened := flattenContainerProjectGroupsInternal(groups)

	require.Len(t, groups, 1)
	require.NotEmpty(t, groups[0].Items[0].IconLightURL)
	require.NotEmpty(t, flattened[0].IconLightURL, "flattened items must carry icons applied to the groups")
}

func groupNamesOf(groups []containertypes.SummaryGroup) []string {
	names := make([]string, 0, len(groups))
	for _, group := range groups {
		names = append(names, group.GroupName)
	}
	return names
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

func TestBuildContainerFilterAccessors_FiltersStandaloneContainers(t *testing.T) {
	service := &ContainerService{}
	updateInfo := &imagetypes.UpdateInfo{HasUpdate: true}
	items := []containertypes.Summary{
		{ID: "standalone", Labels: map[string]string{}, UpdateInfo: updateInfo},
		{ID: "compose", Labels: map[string]string{"com.docker.compose.project": "alpha"}, UpdateInfo: updateInfo},
	}

	result := pagination.SearchOrderAndPaginate(
		items,
		pagination.QueryParams{Filters: map[string]string{"standalone": "true", "updates": "has_update"}},
		pagination.Config[containertypes.Summary]{FilterAccessors: service.buildContainerFilterAccessors()},
	)

	require.Len(t, result.Items, 1)
	require.Equal(t, "standalone", result.Items[0].ID)
	require.Equal(t, int64(1), result.TotalCount)
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

func TestCompareContainerPortsForSortDesc_KeepsContainersWithoutPortsLast(t *testing.T) {
	withPublished := containertypes.Summary{
		ID:    "published",
		Names: []string{"/published"},
		Ports: []containertypes.Port{{PublicPort: 8080, PrivatePort: 80, Type: "tcp"}},
	}
	withPrivateOnly := containertypes.Summary{
		ID:    "private",
		Names: []string{"/private"},
		Ports: []containertypes.Port{{PrivatePort: 3000, Type: "tcp"}},
	}
	withoutPorts := containertypes.Summary{
		ID:    "none",
		Names: []string{"/none"},
	}

	require.Equal(t, -1, compareContainerPortsForSortDescInternal(withPublished, withPrivateOnly))
	require.Equal(t, -1, compareContainerPortsForSortDescInternal(withPrivateOnly, withoutPorts))
	require.Equal(t, 1, compareContainerPortsForSortDescInternal(withoutPorts, withPublished))
}

func TestContainerServiceCommitContainerCallsDockerAPIInternal(t *testing.T) {
	db := setupProjectTestDBInternal(t)
	var gotRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || dockerTestPathInternal(r.URL.Path) != "/commit" {
			http.NotFound(w, r)
			return
		}
		gotRequest = map[string]any{
			"container": r.URL.Query().Get("container"),
			"repo":      r.URL.Query().Get("repo"),
			"tag":       r.URL.Query().Get("tag"),
			"comment":   r.URL.Query().Get("comment"),
			"author":    r.URL.Query().Get("author"),
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ID": "sha256:new-image"})
	}))
	t.Cleanup(server.Close)

	svc := NewContainerService(
		context.Background(),
		event.NewEventService(db, nil, nil),
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)

	out, err := svc.CommitContainer(context.Background(), "container-1", containertypes.CommitRequest{
		Repository: "registry.example.com/team/app",
		Tag:        "snapshot",
		Comment:    "manual snapshot",
		Author:     "arcane",
	}, common.SystemUser)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, "sha256:new-image", out.ID)
	require.Equal(t, map[string]any{
		"container": "container-1",
		"repo":      "registry.example.com/team/app",
		"tag":       "snapshot",
		"comment":   "manual snapshot",
		"author":    "arcane",
	}, gotRequest)

	var evt event.Event
	require.NoError(t, db.WithContext(context.Background()).Where("type = ?", event.EventTypeImageCommit).First(&evt).Error)
	require.Equal(t, "container-1", *evt.ResourceID)
}

func TestContainerServiceCommitContainerOmitsReferenceWhenRepositoryEmptyInternal(t *testing.T) {
	db := setupProjectTestDBInternal(t)
	var gotRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || dockerTestPathInternal(r.URL.Path) != "/commit" {
			http.NotFound(w, r)
			return
		}
		gotRequest = map[string]any{
			"container": r.URL.Query().Get("container"),
			"repo":      r.URL.Query().Get("repo"),
			"tag":       r.URL.Query().Get("tag"),
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ID": "sha256:new-image"})
	}))
	t.Cleanup(server.Close)

	svc := NewContainerService(
		context.Background(),
		event.NewEventService(db, nil, nil),
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)

	out, err := svc.CommitContainer(context.Background(), "container-1", containertypes.CommitRequest{
		Tag: "latest",
	}, common.SystemUser)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, "sha256:new-image", out.ID)
	require.Equal(t, map[string]any{
		"container": "container-1",
		"repo":      "",
		"tag":       "",
	}, gotRequest)
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

// Test fixtures shared by this package's tests.

// dockerTestPathInternal strips the /v1.NN version prefix Docker clients prepend, so
// fake Docker servers can match on the bare path.
func dockerTestPathInternal(path string) string {
	return dockerAPIVersionPrefixInternal.ReplaceAllString(path, "")
}

// newTestDockerClientInternal builds a Docker client pointed at a fake Docker HTTP server.
func newTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()

	httpClient := server.Client()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(httpClient),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

// setupProjectTestDBInternal builds an in-memory DB migrated for project-related tests.
func setupProjectTestDBInternal(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&project.Project{}, &settings.SettingVariable{}, &imageupdate.ImageUpdateRecord{}, &event.Event{}))
	return &database.DB{DB: db}
}

var dockerAPIVersionPrefixInternal = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)

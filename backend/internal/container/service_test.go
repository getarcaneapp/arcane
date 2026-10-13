package container

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	containertypes "github.com/getarcaneapp/arcane/types/v2/container"
	"github.com/getarcaneapp/arcane/types/v2/image"
	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/container/children/stats"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
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
			"com.docker.compose.project":      "media",
			"com.getarcaneapp.arcane.updater": "false",
		}},
	}

	items := service.BuildSummaries(t.Context(), dockerContainers, nil, "", nil)
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
	for _, item := range flattened {
		require.Equal(t, item.ID != "db", item.AutoUpdateEnabled, "grouped items carry the label-derived auto-update status for %s", item.ID)
	}
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
	updateInfo := &image.UpdateInfo{HasUpdate: true}
	items := []containertypes.Summary{
		{ID: "standalone", Labels: map[string]string{}, UpdateInfo: updateInfo},
		{ID: "compose", Labels: map[string]string{"com.docker.compose.project": "alpha"}, UpdateInfo: updateInfo},
		{ID: "swarm-task", Labels: map[string]string{"com.docker.swarm.service.name": "web"}, UpdateInfo: updateInfo},
	}

	result := pagination.Config[containertypes.Summary]{FilterAccessors: service.buildContainerFilterAccessors()}.SearchOrderAndPaginate(
		items,
		pagination.QueryParams{Filters: map[string]string{"standalone": "true", "updates": "has_update"}},
	)

	require.Len(t, result.Items, 1)
	require.Equal(t, "standalone", result.Items[0].ID)
	require.Equal(t, int64(1), result.TotalCount)
}

func TestBuildContainerFilterAccessors_FiltersByLabel(t *testing.T) {
	service := &ContainerService{}
	items := []containertypes.Summary{
		{ID: "tagged", Labels: map[string]string{"heal": "true", "tags": "a,b"}},
		{ID: "other", Labels: map[string]string{"heal": "false"}},
		{ID: "bare", Labels: map[string]string{}},
	}
	config := pagination.Config[containertypes.Summary]{FilterAccessors: service.buildContainerFilterAccessors()}

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{name: "key only", filter: "heal", want: []string{"tagged", "other"}},
		{name: "key and value", filter: "heal=true", want: []string{"tagged"}},
		{name: "missing key", filter: "missing", want: nil},
		{name: "value containing comma", filter: "tags=a,b", want: []string{"tagged"}},
		{name: "blank is a no-op", filter: " ", want: []string{"tagged", "other", "bare"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.SearchOrderAndPaginate(items, pagination.QueryParams{Filters: map[string]string{"label": tt.filter}})
			var got []string
			for _, item := range result.Items {
				got = append(got, item.ID)
			}
			require.Equal(t, tt.want, got)
		})
	}
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
		event.NewEventService(db, nil, nil),
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)

	out, err := svc.CommitContainer(t.Context(), "container-1", containertypes.CommitRequest{
		Repository: "registry.example.com/team/app",
		Tag:        "snapshot",
		Comment:    "manual snapshot",
		Author:     "arcane",
	}, user.SystemUser)
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
	require.NoError(t, db.WithContext(t.Context()).Where("type = ?", event.EventTypeImageCommit).First(&evt).Error)
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
		event.NewEventService(db, nil, nil),
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)

	out, err := svc.CommitContainer(t.Context(), "container-1", containertypes.CommitRequest{
		Tag: "latest",
	}, user.SystemUser)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, "sha256:new-image", out.ID)
	require.Equal(t, map[string]any{
		"container": "container-1",
		"repo":      "",
		"tag":       "",
	}, gotRequest)
}

func newGroupedContainerSummary(name, localProject string) containertypes.Summary {
	labels := map[string]string{}
	labels["com.docker.compose.project"] = cmp.Or(localProject, labels["com.docker.compose.project"])

	return containertypes.Summary{
		ID:     name,
		Names:  []string{name},
		Labels: labels,
		State:  "running",
	}
}

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

var dockerAPIVersionPrefixInternal = regexp.MustCompile(`^/v\d+\.\d+`)

func TestApplyEditPreservesUnmanagedSettingsInternal(t *testing.T) {
	cfg := container.Config{
		Image:        "nginx:1.25",
		Env:          []string{"OLD=1"},
		StopSignal:   "SIGQUIT",
		ExposedPorts: network.PortSet{network.MustParsePort("80/tcp"): {}, network.MustParsePort("9000/tcp"): {}},
	}
	hc := container.HostConfig{
		Binds:        []string{"/data:/data"},
		PortBindings: network.PortMap{network.MustParsePort("80/tcp"): {{HostPort: "8080"}}},
		Sysctls:      map[string]string{"net.core.somaxconn": "1024"},
		LogConfig:    container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "5m"}},

		Ulimits: []*container.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}},
	}

	env := []string{"NEW=1"}
	req := containertypes.Edit{Environment: &env}

	require.NoError(t, applyEditToContainerConfigInternal(&cfg, hc.PortBindings, req))
	require.NoError(t, applyEditToHostConfigInternal(&hc, req))

	require.Equal(t, []string{"NEW=1"}, cfg.Env)
	require.Equal(t, "SIGQUIT", cfg.StopSignal)
	require.Equal(t, map[string]string{"net.core.somaxconn": "1024"}, hc.Sysctls)
	require.Equal(t, "json-file", hc.LogConfig.Type)
	require.Len(t, hc.Ulimits, 1)
	require.Equal(t, []string{"/data:/data"}, hc.Binds)
	require.Contains(t, cfg.ExposedPorts, network.MustParsePort("80/tcp"))
}

func TestMergeEditMountsPreservesMountOptionsInternal(t *testing.T) {
	existing := []mount.Mount{
		{
			Type:          mount.TypeVolume,
			Source:        "data",
			Target:        "/data",
			VolumeOptions: &mount.VolumeOptions{NoCopy: true, Labels: map[string]string{"keep": "me"}},
		},
		{Type: mount.TypeTmpfs, Target: "/tmp/scratch", TmpfsOptions: &mount.TmpfsOptions{SizeBytes: 1024}},
	}

	// Round-trip the volume mount (toggling read-only) and drop the tmpfs one.
	requested := []containertypes.MountCreate{{Type: "volume", Source: "data", Target: "/data", ReadOnly: true}}

	out := mergeEditMountsInternal(existing, requested)
	require.Len(t, out, 1)
	require.True(t, out[0].ReadOnly)
	require.NotNil(t, out[0].VolumeOptions)
	require.Equal(t, map[string]string{"keep": "me"}, out[0].VolumeOptions.Labels)
}

func TestMergeEditMountsAppliesSourceChangeInternal(t *testing.T) {
	existing := []mount.Mount{
		{
			Type:          mount.TypeVolume,
			Source:        "old-volume",
			Target:        "/data",
			VolumeOptions: &mount.VolumeOptions{NoCopy: true, Labels: map[string]string{"keep": "me"}},
		},
		{
			Type:        mount.TypeBind,
			Source:      "/old/path",
			Target:      "/config",
			BindOptions: &mount.BindOptions{Propagation: mount.PropagationRPrivate},
		},
	}

	// Same type and target but a different source must honor the new source
	// while retaining the original driver options.
	requested := []containertypes.MountCreate{
		{Type: "volume", Source: "new-volume", Target: "/data"},
		{Type: "bind", Source: "/new/path", Target: "/config", ReadOnly: true},
	}

	out := mergeEditMountsInternal(existing, requested)
	require.Len(t, out, 2)
	require.Equal(t, "new-volume", out[0].Source)
	require.NotNil(t, out[0].VolumeOptions)
	require.Equal(t, map[string]string{"keep": "me"}, out[0].VolumeOptions.Labels)
	require.Equal(t, "/new/path", out[1].Source)
	require.True(t, out[1].ReadOnly)
	require.NotNil(t, out[1].BindOptions)
	require.Equal(t, mount.PropagationRPrivate, out[1].BindOptions.Propagation)
}

func TestApplyEditRecomputesExposedPortsInternal(t *testing.T) {
	cfg := container.Config{
		ExposedPorts: network.PortSet{
			network.MustParsePort("80/tcp"):   {},
			network.MustParsePort("9000/tcp"): {},
		},
	}
	hc := container.HostConfig{
		PortBindings: network.PortMap{network.MustParsePort("80/tcp"): {{HostPort: "8080"}}},
	}

	newBindings := map[string][]containertypes.PortBindingCreate{"443/tcp": {{HostPort: "8443"}}}
	req := containertypes.Edit{HostConfig: &containertypes.HostConfigEdit{PortBindings: &newBindings}}

	require.NoError(t, applyEditToContainerConfigInternal(&cfg, hc.PortBindings, req))
	require.NoError(t, applyEditToHostConfigInternal(&hc, req))

	// new binding key + previously-exposed-but-unbound port survive; the old bound port is gone
	require.Contains(t, cfg.ExposedPorts, network.MustParsePort("443/tcp"))
	require.Contains(t, cfg.ExposedPorts, network.MustParsePort("9000/tcp"))
	require.NotContains(t, cfg.ExposedPorts, network.MustParsePort("80/tcp"))
	require.Contains(t, hc.PortBindings, network.MustParsePort("443/tcp"))
}

func TestBuildEditNetworkingConfigInternal(t *testing.T) {
	containerInspect := container.InspectResponse{
		HostConfig: &container.HostConfig{NetworkMode: "my-net"},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"my-net": {Aliases: []string{"svc"}, DNSNames: []string{"svc"}},
			},
		},
	}

	// nil request preserves existing attachments
	out, err := buildEditNetworkingConfigInternal(containerInspect, nil, "1.44")
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Contains(t, out.EndpointsConfig, "my-net")

	// explicit desired set: keep my-net with new alias + static IP, add other-net
	req := &containertypes.NetworkingConfigCreate{
		EndpointsConfig: map[string]containertypes.EndpointSettingsCreate{
			"my-net":    {Aliases: []string{"renamed"}, IPv4Address: "10.5.0.9"},
			"other-net": {},
		},
	}
	out, err = buildEditNetworkingConfigInternal(containerInspect, req, "1.44")
	require.NoError(t, err)
	require.Len(t, out.EndpointsConfig, 2)
	require.Equal(t, []string{"renamed"}, out.EndpointsConfig["my-net"].Aliases)
	require.Equal(t, netip.MustParseAddr("10.5.0.9"), out.EndpointsConfig["my-net"].IPAMConfig.IPv4Address)
	require.Equal(t, []string{"svc"}, out.EndpointsConfig["my-net"].DNSNames)

	// host network mode: endpoint edits are skipped entirely
	hostModeInspect := container.InspectResponse{HostConfig: &container.HostConfig{NetworkMode: "host"}}
	out, err = buildEditNetworkingConfigInternal(hostModeInspect, req, "1.44")
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestRecreateContainerDaemonCorrelationInternal(t *testing.T) {
	db := setupProjectTestDBInternal(t)
	events := event.NewEventService(db, nil, nil)
	observed := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := dockerTestPathInternal(r.URL.Path)
		id, name := "old-id", "web"
		if path == "/containers/create" {
			id = ""
		}
		if path == "/containers/new-id/start" {
			id = "new-id"
		}
		if !events.ShouldSuppressDaemonEvent("container", id, name, "") {
			t.Errorf("missing correlation before Docker mutation %s", path)
		}
		if events.ShouldSuppressDaemonEvent("container", "unrelated", "other", "") {
			t.Errorf("unrelated container suppressed during %s", path)
		}
		observed <- path
		if path == "/containers/create" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(map[string]any{"Id": "new-id", "Warnings": []string{}}); err != nil {
				t.Errorf("encode container creation response: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	dockerClient := newTestDockerClientInternal(t, server)
	svc := NewContainerService(events, nil, nil, nil, nil)
	info := container.InspectResponse{
		ID:         "old-id",
		Name:       "/web",
		State:      &container.State{Running: true},
		Config:     &container.Config{Image: "app:latest"},
		HostConfig: &container.HostConfig{},
	}
	id, err := svc.recreateContainerInternal(
		t.Context(),
		dockerClient,
		info,
		"redeploy",
		"web",
		info.Config,
		info.HostConfig,
		nil,
		"1.41",
		event.EventTypeContainerDeploy,
		user.SystemUser,
	)
	require.NoError(t, err)
	require.Equal(t, "new-id", id)
	require.Len(t, observed, 5)
	require.Equal(
		t,
		[]string{
			"/containers/old-id/rename",
			"/containers/old-id/stop",
			"/containers/create",
			"/containers/new-id/start",
			"/containers/old-id",
		},
		[]string{
			<-observed,
			<-observed,
			<-observed,
			<-observed,
			<-observed,
		},
	)
	require.True(t, events.ShouldSuppressDaemonEvent("container", "old-id", "", ""))
	require.True(t, events.ShouldSuppressDaemonEvent("container", "new-id", "", ""))
}

func TestBuildSummariesUsesContainerTagPolicyUpdates(t *testing.T) {
	service := &ContainerService{}
	strategyLabel := "com.getarcaneapp.arcane.updater.strategy"
	containers := []container.Summary{
		{ID: "first", Image: "app:3.1.0", ImageID: "shared-image"},
		{ID: "second", Image: "app:3.1.0", ImageID: "shared-image", Labels: map[string]string{strategyLabel: "auto"}},
		{ID: "unchecked", Image: "app:3.1.0", ImageID: "shared-image", Labels: map[string]string{strategyLabel: "tag"}},
		{ID: "digest", Image: "app:3.1.0", ImageID: "shared-image", Labels: map[string]string{strategyLabel: "digest"}},
		{ID: "opted-out", Names: []string{"/opted-out"}, Image: "app:3.1.0", ImageID: "shared-image", Labels: map[string]string{"com.getarcaneapp.arcane.updater": "false"}},
		{ID: "unmonitored", Image: "app:3.1.0", ImageID: "shared-image", Labels: map[string]string{"com.getarcaneapp.arcane.update-check": "false"}},
	}
	// The lookup keys results by container ID; containers opted out of checks have no entry.
	updates := map[string]*image.UpdateInfo{
		"first":     {HasUpdate: true, UpdateType: "digest"},
		"second":    {HasUpdate: true, UpdateType: "tag", LatestVersion: "4.0.0"},
		"digest":    {HasUpdate: true, UpdateType: "digest"},
		"opted-out": {HasUpdate: true, UpdateType: "digest"},
	}
	items := service.BuildSummaries(t.Context(), containers, updates, "", nil)
	require.Equal(t, "digest", items[0].UpdateStrategy, "an unlabeled container follows the digest")
	require.Equal(t, "tag", items[1].UpdateStrategy)
	require.Equal(t, "digest", items[3].UpdateStrategy)
	require.Equal(t, "digest", items[0].UpdateInfo.UpdateType)
	require.Equal(t, "4.0.0", items[1].UpdateInfo.LatestVersion)
	require.Nil(t, items[2].UpdateInfo)
	require.Equal(t, "digest", items[3].UpdateInfo.UpdateType)

	require.True(t, items[0].AutoUpdateEnabled, "containers without opt-out are eligible")
	require.False(t, items[4].AutoUpdateEnabled, "the updater label disables auto-update")
	require.Equal(t, "digest", items[4].UpdateInfo.UpdateType, "disabling automatic updates keeps check results visible")
	require.Nil(t, items[5].UpdateInfo, "a container without a lookup result reports no status")
	require.True(t, items[5].AutoUpdateEnabled, "the update-check label does not affect installation eligibility")
	encoded, err := json.Marshal(items[4])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"autoUpdateEnabled":false`, "false status must stay serialized")
}

func TestContainerServiceGetContainerProcessesInternal(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		fallback *struct {
			status int
			body   string
		}
		wantTitles []string
		wantRows   [][]string
		wantErr    func(error) bool
	}{
		{
			name:   "docker snapshot",
			status: http.StatusOK,
			body: "{\"Titles\":[\"PID\",\"USER\",\"%CPU\",\"%MEM\",\"ELAPSED\",\"CMD\"],\"Processes\":[[\"1\",\"root\",\"0.0\",\"0.1\",\"01:02:0" +
				"3\",\"nginx: master process nginx -g daemon off;\"],[\"29\",\"nginx\",\"0.0\",\"0.1\",\"01:02:03\",\"nginx: worker" +
				" process\"]]}",
			wantTitles: []string{"PID", "USER", "%CPU", "%MEM", "ELAPSED", "CMD"},
			wantRows: [][]string{
				{
					"1",
					"root",
					"0.0",
					"0.1",
					"01:02:03",
					"nginx: master process nginx -g daemon off;",
				},
				{
					"29",
					"nginx",
					"0.0",
					"0.1",
					"01:02:03",
					"nginx: worker process",
				},
			},
		},
		{name: "empty", status: http.StatusOK, body: `{}`, wantTitles: []string{}, wantRows: [][]string{}},
		{name: "not found", status: http.StatusNotFound, body: `{"message":"No such container: container-1"}`, wantErr: errdefs.IsNotFound},
		{name: "not running", status: http.StatusConflict, body: `{"message":"container container-1 is not running"}`, wantErr: errdefs.IsConflict},
		{
			name:   "busybox ps falls back",
			status: http.StatusInternalServerError,
			body:   `{"message":"ps: bad -o argument '%cpu', supported arguments: user,group,comm,args,pid,ppid,pgid,etime,nice,rgroup,ruser,time,tty,vsz,stat,rss"}`,
			fallback: &struct {
				status int
				body   string
			}{http.StatusOK, `{"Titles":["PID","USER","ELAPSED","COMMAND"],"Processes":[["1","root","01:02:03","nginx: master process nginx -g daemon off;"]]}`},
			wantTitles: []string{"PID", "USER", "ELAPSED", "COMMAND"},
			wantRows:   [][]string{{"1", "root", "01:02:03", "nginx: master process nginx -g daemon off;"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotArgs []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || dockerTestPathInternal(r.URL.Path) != "/containers/container-1/top" {
					http.NotFound(w, r)
					return
				}
				gotArgs = append(gotArgs, r.URL.Query().Get("ps_args"))
				status, body := tt.status, tt.body
				if tt.fallback != nil && len(gotArgs) > 1 {
					status, body = tt.fallback.status, tt.fallback.body
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if _, err := io.WriteString(w, body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)
			svc := NewContainerService(nil, docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)), nil, nil, nil)

			processes, err := svc.GetContainerProcesses(t.Context(), "container-1")
			wantArgs := []string{strings.Join(containerProcessesPsArgs[0], " ")}
			if tt.fallback != nil {
				wantArgs = append(wantArgs, strings.Join(containerProcessesPsArgs[1], " "))
			}
			require.Equal(t, wantArgs, gotArgs)
			if tt.wantErr != nil {
				require.True(t, tt.wantErr(err), "unexpected error: %v", err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTitles, processes.Titles)
			require.Equal(t, tt.wantRows, processes.Processes)
		})
	}
}

func TestContainerServiceGetContainerProcessesPropagatesContextInternal(t *testing.T) {
	requestDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(requestDone)
	}))
	t.Cleanup(server.Close)
	svc := NewContainerService(nil, docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)), nil, nil, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := svc.GetContainerProcesses(ctx, "container-1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	select {
	case <-requestDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Docker request was not cancelled")
	}
}

type resourceStatsServerInternal struct {
	mu          sync.Mutex
	containers  []container.Summary
	stats       map[string]container.StatsResponse
	statsStatus map[string]int
	statsCalls  map[string]int
	delay       time.Duration
	slowIDs     map[string]bool

	current atomic.Int32
	maxSeen atomic.Int32
}

func newResourceStatsServerInternal() *resourceStatsServerInternal {
	return &resourceStatsServerInternal{
		stats:       map[string]container.StatsResponse{},
		statsStatus: map[string]int{},
		statsCalls:  map[string]int{},
		slowIDs:     map[string]bool{},
	}
}

func (f *resourceStatsServerInternal) addContainer(id, name, state string, labels ...map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var l map[string]string
	if len(labels) > 0 {
		l = labels[0]
	}
	f.containers = append(f.containers, container.Summary{ID: id, Names: []string{"/" + name}, State: container.ContainerState(state), Image: "img:latest", Labels: l})
}

func (f *resourceStatsServerInternal) setStats(id string, cpuPercent float64, memoryUsage, memoryLimit uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// cpuPercent maps to cpuDelta/systemDelta*100 with a fixed systemDelta of 1000.
	f.stats[id] = container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: uint64(1000 + cpuPercent*10)},
			SystemUsage: 2000,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1000},
			SystemUsage: 1000,
		},
		MemoryStats: container.MemoryStats{
			Usage: memoryUsage,
			Limit: memoryLimit,
		},
		Read: time.Now(),
	}
}

func (f *resourceStatsServerInternal) callsFor(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statsCalls[id]
}

func (f *resourceStatsServerInternal) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := dockerTestPathInternal(r.URL.Path)
		switch {
		case path == "/containers/json":
			f.mu.Lock()
			list := append([]container.Summary{}, f.containers...)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/json"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/json")
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, summary := range f.containers {
				if summary.ID == id {
					inspect := container.InspectResponse{
						ID:    summary.ID,
						Name:  summary.Names[0],
						Image: summary.ImageID,
						Config: &container.Config{
							Image:  summary.Image,
							Labels: summary.Labels,
						},
						State: &container.State{Status: summary.State},
					}
					if err := json.NewEncoder(w).Encode(inspect); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
					}
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		case strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/stats"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/stats")
			cur := f.current.Add(1)
			for {
				seen := f.maxSeen.Load()
				if cur <= seen || f.maxSeen.CompareAndSwap(seen, cur) {
					break
				}
			}
			defer f.current.Add(-1)

			f.mu.Lock()
			f.statsCalls[id]++
			statsData, hasStats := f.stats[id]
			status, hasStatus := f.statsStatus[id]
			delay := f.delay
			slow := f.slowIDs[id]
			f.mu.Unlock()

			wait := kit.Ternary(slow, time.Second, delay)
			if wait > 0 {
				select {
				case <-time.After(wait):
				case <-r.Context().Done():
					return
				}
			}
			if hasStatus {
				w.WriteHeader(status)
				return
			}
			if !hasStats {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(statsData)
		default:
			http.NotFound(w, r)
		}
	})
}

func newResourceSortServiceInternal(t *testing.T, fixture *resourceStatsServerInternal) *ContainerService {
	t.Helper()
	server := httptest.NewServer(fixture.handler())
	t.Cleanup(server.Close)

	return NewContainerService(
		nil,
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)
}

func resourceSortParamsInternal(sort, order string, start, limit int) pagination.QueryParams {
	return pagination.QueryParams{
		Sort: sort, Order: pagination.SortOrder(order),
		Start: start, Limit: limit,
		Filters: map[string]string{},
	}
}

func summaryIDsInternal(items []containertypes.Summary) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func listResourceSortedInternal(t *testing.T, svc *ContainerService, params pagination.QueryParams) ContainerListResult {
	t.Helper()
	result, err := svc.ListContainersPaginated(t.Context(), params, true, true, true, "")
	require.NoError(t, err)
	return result
}

func TestResourceSortRanksMemoryByBytesNotPercent(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	// high-percent: 800 of 1000 bytes (80%); high-bytes: 900 of 10000 (9%).
	fixture.addContainer("high-percent", "high-percent", "running")
	fixture.setStats("high-percent", 10, 800, 1000)
	fixture.addContainer("high-bytes", "high-bytes", "running")
	fixture.setStats("high-bytes", 10, 900, 10000)
	fixture.addContainer("opted-out", "opted-out", "running", map[string]string{"com.getarcaneapp.arcane.updater": "false"})
	fixture.setStats("opted-out", 10, 100, 10000)
	svc := newResourceSortServiceInternal(t, fixture)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, []string{"high-bytes", "high-percent", "opted-out"}, summaryIDsInternal(result.Items))
	require.True(t, result.Items[0].AutoUpdateEnabled)
	require.False(t, result.Items[2].AutoUpdateEnabled)

	result = listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "asc", 0, 20))
	require.Equal(t, []string{"opted-out", "high-percent", "high-bytes"}, summaryIDsInternal(result.Items))

	flat, err := svc.ListContainersPaginated(t.Context(), resourceSortParamsInternal("name", "asc", 0, 20), true, true, true, "")
	require.NoError(t, err)
	require.Equal(t, []string{"high-bytes", "high-percent", "opted-out"}, summaryIDsInternal(flat.Items))
	require.True(t, flat.Items[0].AutoUpdateEnabled)
	require.False(t, flat.Items[2].AutoUpdateEnabled)

	details, err := svc.GetContainerDetails(t.Context(), "opted-out")
	require.NoError(t, err)
	require.False(t, details.AutoUpdateEnabled, "detail status must match the list status")
	details, err = svc.GetContainerDetails(t.Context(), "high-bytes")
	require.NoError(t, err)
	require.True(t, details.AutoUpdateEnabled)
}

func TestResourceSortOrdersAcrossPages(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	memoryByID := map[string]uint64{"c1": 100, "c2": 200, "c3": 300, "c4": 400, "c5": 500}
	for id, memory := range memoryByID {
		fixture.addContainer(id, id, "running")
		fixture.setStats(id, 0, memory, 10000)
	}
	svc := newResourceSortServiceInternal(t, fixture)

	tests := []struct {
		name  string
		order string
		start int
		want  []string
	}{
		{name: "desc page 1", order: "desc", start: 0, want: []string{"c5", "c4"}},
		{name: "desc page 2", order: "desc", start: 2, want: []string{"c3", "c2"}},
		{name: "desc page 3", order: "desc", start: 4, want: []string{"c1"}},
		{name: "asc page 1", order: "asc", start: 0, want: []string{"c1", "c2"}},
		{name: "asc page 2", order: "asc", start: 2, want: []string{"c3", "c4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, tt.order, tt.start, 2))
			require.Equal(t, tt.want, summaryIDsInternal(result.Items))
			require.Equal(t, int64(5), result.Pagination.TotalItems)
		})
	}
}

func TestResourceSortOrdersByCPUPercent(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("busy", "busy", "running")
	fixture.setStats("busy", 90, 100, 1000)
	fixture.addContainer("idle", "idle", "running")
	fixture.setStats("idle", 5, 100, 1000)
	svc := newResourceSortServiceInternal(t, fixture)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortCPUUsage, "desc", 0, 20))
	require.Equal(t, []string{"busy", "idle"}, summaryIDsInternal(result.Items))
	require.InDelta(t, 90, result.Items[0].ResourceSample.CPUPercent, 0.2)
	require.InDelta(t, 5, result.Items[1].ResourceSample.CPUPercent, 0.2)
}

func TestResourceSortAppliesSearchAndFiltersBeforeCollectingStats(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running", map[string]string{"team": "backend"})
	fixture.setStats("web", 10, 100, 1000)
	fixture.addContainer("db", "db", "running")
	fixture.setStats("db", 20, 200, 1000)
	svc := newResourceSortServiceInternal(t, fixture)

	params := resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20)
	params.Search = "web"
	result := listResourceSortedInternal(t, svc, params)
	require.Equal(t, []string{"web"}, summaryIDsInternal(result.Items))
	require.Equal(t, 1, fixture.callsFor("web"))
	require.Equal(t, 0, fixture.callsFor("db"), "filtered-out containers must not trigger stats collection")

	params = resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20)
	params.Filters["label"] = "team=backend"
	result = listResourceSortedInternal(t, svc, params)
	require.Equal(t, []string{"web"}, summaryIDsInternal(result.Items))
	require.Equal(t, 0, fixture.callsFor("db"))
}

func TestResourceSortHandlesZeroUnavailableStoppedAndTies(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("zero", "zero", "running")
	fixture.setStats("zero", 0, 0, 1000)
	fixture.addContainer("tie-b", "alpha", "running")
	fixture.setStats("tie-b", 0, 100, 1000)
	fixture.addContainer("tie-a", "alpha", "running")
	fixture.setStats("tie-a", 0, 100, 1000)
	fixture.addContainer("broken", "broken", "running")
	fixture.statsStatus["broken"] = http.StatusInternalServerError
	fixture.addContainer("stopped", "stopped", "exited")
	svc := newResourceSortServiceInternal(t, fixture)

	asc := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "asc", 0, 20))
	require.Equal(t, []string{"zero", "tie-a", "tie-b", "broken", "stopped"}, summaryIDsInternal(asc.Items),
		"zeros first, unavailable and stopped last, ties by name then ID")
	require.NotNil(t, asc.Items[0].ResourceSample, "zero usage is a valid sample, not unavailable")
	require.Nil(t, asc.Items[3].ResourceSample)
	require.Nil(t, asc.Items[4].ResourceSample)

	desc := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, []string{"tie-a", "tie-b", "zero", "broken", "stopped"}, summaryIDsInternal(desc.Items),
		"unavailable stays last in descending order too")

	require.Equal(t, 0, fixture.callsFor("stopped"), "stopped containers must not trigger stats collection")
}

func TestResourceSortCachesSamples(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	svc := newResourceSortServiceInternal(t, fixture)

	for range 2 {
		listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	}
	require.Equal(t, 1, fixture.callsFor("web"), "second list within the TTL must reuse the cached sample")
}

func TestResourceSortCacheExpires(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	svc := newResourceSortServiceInternal(t, fixture)
	svc.stats = stats.New(svc.dockerService.GetClient, svc.dockerService.DockerHost, 50*time.Millisecond, 0, 0)

	listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	time.Sleep(100 * time.Millisecond)
	listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, 2, fixture.callsFor("web"), "expired entries must be refetched")
}

func TestResourceSortCoalescesConcurrentFetches(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	fixture.delay = 100 * time.Millisecond
	svc := newResourceSortServiceInternal(t, fixture)

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() {
			_, err := svc.stats.Collect(t.Context(), []containertypes.Summary{{ID: "web", State: "running"}})
			errs[i] = err
		})
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, fixture.callsFor("web"), "concurrent fetches for the same container must coalesce")
}

func TestResourceSortBoundsCollectionConcurrency(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.delay = 50 * time.Millisecond
	for i := range 32 {
		id := fmt.Sprintf("c%02d", i)
		fixture.addContainer(id, id, "running")
		fixture.setStats(id, 0, uint64(i), 10000)
	}
	svc := newResourceSortServiceInternal(t, fixture)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 40))
	require.Len(t, result.Items, 32)
	require.LessOrEqual(t, fixture.maxSeen.Load(), int32(stats.ContainerResourceCollectConcurrency))
}

func TestResourceSortBatchTimeoutFailsRequest(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("slow", "slow", "running")
	fixture.setStats("slow", 10, 100, 1000)
	fixture.delay = time.Second
	svc := newResourceSortServiceInternal(t, fixture)
	svc.stats = stats.New(svc.dockerService.GetClient, svc.dockerService.DockerHost, stats.ContainerResourceSampleTTL, 0, 50*time.Millisecond)

	_, err := svc.ListContainersPaginated(t.Context(), resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20), true, true, true, "")
	require.Error(t, err, "batch timeout must fail the refresh instead of presenting a partial sort")
}

func TestResourceSortPerContainerTimeoutMarksUnavailable(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("fast", "fast", "running")
	fixture.setStats("fast", 10, 100, 1000)
	fixture.addContainer("slow", "slow", "running")
	fixture.setStats("slow", 10, 200, 1000)
	fixture.slowIDs["slow"] = true
	svc := newResourceSortServiceInternal(t, fixture)
	svc.stats = stats.New(svc.dockerService.GetClient, svc.dockerService.DockerHost, stats.ContainerResourceSampleTTL, 50*time.Millisecond, 0)

	result := listResourceSortedInternal(t, svc, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20))
	require.Equal(t, []string{"fast", "slow"}, summaryIDsInternal(result.Items))
	require.NotNil(t, result.Items[0].ResourceSample)
	require.Nil(t, result.Items[1].ResourceSample, "per-container timeout leaves the sample unavailable")
}

func TestResourceSortCancelledContextFailsRequest(t *testing.T) {
	fixture := newResourceStatsServerInternal()
	fixture.addContainer("web", "web", "running")
	fixture.setStats("web", 10, 100, 1000)
	fixture.delay = time.Second
	svc := newResourceSortServiceInternal(t, fixture)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := svc.ListContainersPaginated(ctx, resourceSortParamsInternal(containertypes.SortMemoryUsage, "desc", 0, 20), true, true, true, "")
	require.Error(t, err)
}

func newLogsTestServiceInternal(t *testing.T, tty bool, logsBody []byte, gotQuery *url.Values) *ContainerService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch dockerTestPathInternal(r.URL.Path) {
		case "/containers/container-1/json":
			ttyJSON := kit.Ternary(tty, "true", "false")
			_, _ = io.WriteString(w, `{"Id":"0123456789abcdef0123456789abcdef","Config":{"Tty":`+ttyJSON+`}}`)
		case "/containers/container-1/logs":
			*gotQuery = r.URL.Query()
			_, _ = w.Write(logsBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return NewContainerService(
		event.NewEventService(setupProjectTestDBInternal(t), nil, nil),
		docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(newTestDockerClientInternal(t, server)),
		nil,
		nil,
		nil,
	)
}

func TestContainerServiceDownloadLogsDemultiplexesNonTTYInternal(t *testing.T) {
	var frames bytes.Buffer
	longLine := strings.Repeat("x", 70*1024) + "\n"
	for _, frame := range []struct {
		stream  byte
		payload string
	}{
		{1, "out one\n"},
		{2, "err one\n"},
		{1, "\n"},
		{1, longLine},
		{2, "err two\n"},
	} {
		header := make([]byte, 8)
		header[0] = frame.stream
		binary.BigEndian.PutUint32(header[4:], uint32(len(frame.payload)))
		frames.Write(header)
		frames.WriteString(frame.payload)
	}

	var gotQuery url.Values
	svc := newLogsTestServiceInternal(t, false, frames.Bytes(), &gotQuery)

	reader, filename, err := svc.DownloadLogs(t.Context(), "container-1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = reader.Close() })

	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	require.Equal(t, "container-0123456789ab-logs.log", filename)
	require.Equal(t, "out one\nerr one\n\n"+longLine+"err two\n", string(content))
	require.Equal(t, "1", gotQuery.Get("stdout"))
	require.Equal(t, "1", gotQuery.Get("stderr"))
	require.Contains(t, []string{"", "all"}, gotQuery.Get("tail"))
	require.Equal(t, "1", gotQuery.Get("timestamps"))
	require.NotEqual(t, "1", gotQuery.Get("follow"))
	require.Empty(t, gotQuery.Get("since"))
}

func TestContainerServiceDownloadLogsPassesTTYOutputThroughInternal(t *testing.T) {
	var gotQuery url.Values
	svc := newLogsTestServiceInternal(t, true, []byte("raw tty line\r\nno trailing newline"), &gotQuery)

	reader, filename, err := svc.DownloadLogs(t.Context(), "container-1")
	require.NoError(t, err)

	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	require.Equal(t, "container-0123456789ab-logs.log", filename)
	require.Equal(t, "raw tty line\r\nno trailing newline", string(content))
}

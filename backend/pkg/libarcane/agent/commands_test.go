package agent

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveCommandName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		method    string
		path      string
		stream    bool
		command   string
		shouldHit bool
	}{
		{name: "container list", method: "GET", path: "/api/environments/0/containers", command: "container.list", shouldHit: true},
		{name: "container edit config", method: "GET", path: "/api/environments/0/containers/abc/config", command: "container.edit_config", shouldHit: true},
		{name: "container edit", method: "PUT", path: "/api/environments/0/containers/abc/config", command: "container.edit", shouldHit: true},
		{name: "container edit config wrong method", method: "POST", path: "/api/environments/0/containers/abc/config", command: "container.edit_config", shouldHit: false},
		{name: "container edit wrong method", method: "DELETE", path: "/api/environments/0/containers/abc/config", command: "container.edit", shouldHit: false},
		{
			name:      "container edit config stream rejected",
			method:    "GET",
			path:      "/api/environments/0/containers/abc/config",
			stream:    true,
			command:   "container.edit_config",
			shouldHit: false,
		},
		{name: "container edit stream rejected", method: "PUT", path: "/api/environments/0/containers/abc/config", stream: true, command: "container.edit", shouldHit: false},
		{name: "container action", method: "POST", path: "/api/environments/0/containers/abc?action=start", command: "container.action", shouldHit: true},
		{
			name:      "volume workspace download",
			method:    "GET",
			path:      "/api/environments/0/volumes/data/workspace/file/download?relativePath=notes/readme.txt",
			command:   "volume.workspace.download",
			shouldHit: true,
		},
		{name: "volume workspace list", method: "GET", path: "/api/environments/0/volumes/data/workspace", command: "volume.workspace.list", shouldHit: true},
		{name: "volume named backups inspect", method: "GET", path: "/api/environments/0/volumes/backups", command: "volume.inspect", shouldHit: true},
		{name: "volume named backups workspace", method: "GET", path: "/api/environments/0/volumes/backups/workspace", command: "volume.workspace.list", shouldHit: true},
		{name: "volume named counts usage", method: "GET", path: "/api/environments/0/volumes/counts/usage", command: "volume.usage", shouldHit: true},
		{name: "volume named sizes backups", method: "GET", path: "/api/environments/0/volumes/sizes/backups", command: "volume.backup.list", shouldHit: true},
		{name: "volume backup download keeps static branch", method: "GET", path: "/api/environments/0/volumes/backups/b1/download", command: "volume.backup.download", shouldHit: true},
		{
			name:      "volume workspace read file",
			method:    "GET",
			path:      "/api/environments/0/volumes/data/workspace/file?relativePath=notes/readme.txt",
			command:   "volume.workspace.read_file",
			shouldHit: true,
		},
		{name: "volume workspace update", method: "PATCH", path: "/api/environments/0/volumes/data/workspace", command: "volume.workspace.update", shouldHit: true},
		{name: "project workspace list", method: "GET", path: "/api/environments/0/projects/p1/workspace", command: "project.workspace.list", shouldHit: true},
		{
			name:      "project workspace read file",
			method:    "GET",
			path:      "/api/environments/0/projects/p1/workspace/file?relativePath=notes/readme.txt",
			command:   "project.workspace.read_file",
			shouldHit: true,
		},
		{
			name:      "project workspace download",
			method:    "GET",
			path:      "/api/environments/0/projects/p1/workspace/file/download?relativePath=notes/readme.txt",
			command:   "project.workspace.download",
			shouldHit: true,
		},
		{name: "project workspace update", method: "PATCH", path: "/api/environments/0/projects/p1/workspace", command: "project.workspace.update", shouldHit: true},
		{name: "build workspace browse retained", method: "GET", path: "/api/environments/0/builds/browse", command: "build_workspace.browse.list", shouldHit: true},
		{name: "legacy project files absent", method: "GET", path: "/api/environments/0/projects/p1/files", shouldHit: false},
		{name: "legacy project includes absent", method: "PUT", path: "/api/environments/0/projects/p1/includes", shouldHit: false},
		{name: "legacy volume browse absent", method: "GET", path: "/api/environments/0/volumes/data/browse", shouldHit: false},
		{name: "legacy volume files absent", method: "GET", path: "/api/environments/0/volumes/data/files", shouldHit: false},
		{name: "project logs stream", method: "GET", path: "/api/environments/0/ws/projects/p1/logs", stream: true, command: "project.logs.stream", shouldHit: true},
		{name: "project updates", method: "GET", path: "/api/environments/0/projects/p1/updates", command: "project.updates", shouldHit: true},
		{name: "project action", method: "POST", path: "/api/environments/0/projects/p1?action=archive", command: "project.action", shouldHit: true},
		{name: "activity list", method: "GET", path: "/api/environments/0/activities?limit=50", command: "activity.list", shouldHit: true},
		{name: "activity inspect", method: "GET", path: "/api/environments/0/activities/activity-1", command: "activity.inspect", shouldHit: true},
		{name: "activity cancel", method: "POST", path: "/api/environments/0/activities/activity-1?action=cancel", command: "activity.action", shouldHit: true},
		{name: "activity history clear", method: "DELETE", path: "/api/environments/0/activities/history", command: "activity.history.clear", shouldHit: true},
		{name: "health", method: "HEAD", path: "/api/environments/0/system/health", command: "system.health", shouldHit: true},
		{name: "swarm node identity", method: "GET", path: "/api/swarm/identity", command: "swarm.node_identity", shouldHit: true},
		{name: "ports list", method: "GET", path: "/api/environments/0/ports?limit=20", command: "port.list", shouldHit: true},
		{name: "network topology", method: "GET", path: "/api/environments/0/networks/topology", command: "network.topology", shouldHit: true},
		{name: "container auto-update", method: "PUT", path: "/api/environments/0/containers/abc/autoupdate", command: "container.auto_update.set", shouldHit: true},
		{name: "project update services", method: "POST", path: "/api/environments/0/projects/p1?action=updateservices&services=web", command: "project.action", shouldHit: true},
		{name: "swarm services list", method: "GET", path: "/api/environments/0/swarm/services", command: "swarm.service.list", shouldHit: true},
		{name: "unknown", method: "PATCH", path: "/api/environments/0/containers", shouldHit: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			command, ok := ResolveCommandName(tc.method, tc.path, tc.stream).Get()
			require.Equal(t, tc.shouldHit, ok)
			if tc.shouldHit {
				require.Equal(t, tc.command, command)
			}
		})
	}
}

func TestBuildCommandRouteIndexPanicsOnDuplicateRoute(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		buildCommandRouteIndex([]commandRoute{
			{Method: http.MethodGet, PathPattern: "/api/test/{id}", CommandName: "test.one"},
			{Method: http.MethodGet, PathPattern: "/api/test/{id}", CommandName: "test.two"},
		})
	})
}

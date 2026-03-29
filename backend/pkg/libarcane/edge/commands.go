package edge

import (
	"net/http"
	"strings"
)

type commandRoute struct {
	Method      string
	PathPattern string
	CommandName string
	Stream      bool
}

var commandRoutes = []commandRoute{
	{Method: http.MethodGet, PathPattern: "/api/health", CommandName: "system.health"},
	{Method: http.MethodHead, PathPattern: "/api/health", CommandName: "system.health"},
	{Method: http.MethodGet, PathPattern: "/api/app-version", CommandName: "system.version"},
	{Method: http.MethodPost, PathPattern: "/api/container-registries/sync", CommandName: "container_registry.sync"},
	{Method: http.MethodPost, PathPattern: "/api/git-repositories/sync", CommandName: "git_repository.sync"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers", CommandName: "container.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/counts", CommandName: "container.counts"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers", CommandName: "container.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/{containerId}", CommandName: "container.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/start", CommandName: "container.start"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/stop", CommandName: "container.stop"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/restart", CommandName: "container.restart"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/redeploy", CommandName: "container.redeploy"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/containers/{containerId}", CommandName: "container.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/update", CommandName: "container.update"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images", CommandName: "image.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/counts", CommandName: "image.counts"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}", CommandName: "image.inspect"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/images/{imageId}", CommandName: "image.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/pull", CommandName: "image.pull"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/build", CommandName: "image.build.start"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/builds", CommandName: "image.build.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/builds/{buildId}", CommandName: "image.build.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/prune", CommandName: "image.prune"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/upload", CommandName: "image.upload"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/image-updates/check", CommandName: "image_update.check"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/image-updates/check/{imageId}", CommandName: "image_update.check_one"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/image-updates/check/{imageId}", CommandName: "image_update.check_one"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/image-updates/check-batch", CommandName: "image_update.check_batch"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/image-updates/check-all", CommandName: "image_update.check_all"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/image-updates/summary", CommandName: "image_update.summary"},

	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities/scan", CommandName: "vulnerability.scan"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities", CommandName: "vulnerability.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities/summary", CommandName: "vulnerability.summary"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/vulnerabilities/summaries", CommandName: "vulnerability.summary"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities/list", CommandName: "vulnerability.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/scanner-status", CommandName: "vulnerability.scanner_status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/summary", CommandName: "vulnerability.summary"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/all", CommandName: "vulnerability.all"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/image-options", CommandName: "vulnerability.image_options"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/vulnerabilities/ignore", CommandName: "vulnerability.ignore.create"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/vulnerabilities/ignore/{ignoreId}", CommandName: "vulnerability.ignore.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/ignored", CommandName: "vulnerability.ignore.list"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects", CommandName: "project.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/counts", CommandName: "project.counts"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/up", CommandName: "project.up"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/down", CommandName: "project.down"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects", CommandName: "project.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}", CommandName: "project.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/redeploy", CommandName: "project.redeploy"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/projects/{projectId}/destroy", CommandName: "project.destroy"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/projects/{projectId}", CommandName: "project.update"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/projects/{projectId}/includes", CommandName: "project.includes"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/restart", CommandName: "project.restart"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/pull", CommandName: "project.pull"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/build", CommandName: "project.build"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops-syncs", CommandName: "gitops_sync.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops-syncs", CommandName: "gitops_sync.create"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops-syncs/import", CommandName: "gitops_sync.import"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops-syncs/{syncId}", CommandName: "gitops_sync.inspect"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/gitops-syncs/{syncId}", CommandName: "gitops_sync.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/gitops-syncs/{syncId}", CommandName: "gitops_sync.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops-syncs/{syncId}/sync", CommandName: "gitops_sync.run"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops-syncs/{syncId}/status", CommandName: "gitops_sync.status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops-syncs/{syncId}/files", CommandName: "gitops_sync.files"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/counts", CommandName: "volume.counts"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes", CommandName: "volume.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}", CommandName: "volume.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes", CommandName: "volume.create"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/volumes/{volumeName}", CommandName: "volume.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/prune", CommandName: "volume.prune"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/usage", CommandName: "volume.usage"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/sizes", CommandName: "volume.sizes"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/browse", CommandName: "volume.browse.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/browse/content", CommandName: "volume.browse.read_file"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/browse/download", CommandName: "volume.browse.download"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/browse/upload", CommandName: "volume.browse.upload"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/browse/mkdir", CommandName: "volume.browse.mkdir"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/volumes/{volumeName}/browse", CommandName: "volume.browse.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups", CommandName: "volume.backup.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups", CommandName: "volume.backup.create"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore", CommandName: "volume.backup.restore"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/{backupId}/restore-files", CommandName: "volume.backup.restore_files"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}", CommandName: "volume.backup.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/download", CommandName: "volume.backup.download"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/has-path", CommandName: "volume.backup.has_path"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/files", CommandName: "volume.backup.files"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/upload", CommandName: "volume.backup.upload"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/builds/browse", CommandName: "build_workspace.browse.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/builds/browse/content", CommandName: "build_workspace.browse.read_file"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/builds/browse/download", CommandName: "build_workspace.browse.download"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/builds/browse/upload", CommandName: "build_workspace.browse.upload"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/builds/browse/mkdir", CommandName: "build_workspace.browse.mkdir"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/builds/browse", CommandName: "build_workspace.browse.delete"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/networks", CommandName: "network.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/networks/counts", CommandName: "network.counts"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/networks", CommandName: "network.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/networks/{networkId}", CommandName: "network.inspect"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/networks/{networkId}", CommandName: "network.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/networks/prune", CommandName: "network.prune"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/settings/public", CommandName: "settings.public.get"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/settings", CommandName: "settings.get"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/settings", CommandName: "settings.update"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/job-schedules", CommandName: "job_schedule.list"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/job-schedules", CommandName: "job_schedule.upsert"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/jobs", CommandName: "job.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/jobs/{jobId}/run", CommandName: "job.run"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/notifications/settings", CommandName: "notification.settings.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/notifications/settings/{provider}", CommandName: "notification.settings.get"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/notifications/settings", CommandName: "notification.settings.upsert"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/notifications/settings/{provider}", CommandName: "notification.settings.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/notifications/test/{provider}", CommandName: "notification.test"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/notifications/apprise", CommandName: "notification.apprise.get"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/notifications/apprise", CommandName: "notification.apprise.upsert"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/notifications/apprise/test", CommandName: "notification.apprise.test"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/dashboard/action-items", CommandName: "dashboard.action_items"},

	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/updater/run", CommandName: "updater.run"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/updater/status", CommandName: "updater.status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/updater/history", CommandName: "updater.history"},

	{Method: http.MethodHead, PathPattern: "/api/environments/{id}/system/health", CommandName: "system.health"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/system/docker/info", CommandName: "system.docker_info"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/prune", CommandName: "system.prune"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/containers/start-all", CommandName: "system.containers.start_all"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/containers/start-stopped", CommandName: "system.containers.start_stopped"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/containers/stop-all", CommandName: "system.containers.stop_all"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/convert", CommandName: "system.convert"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/system/upgrade/check", CommandName: "system.upgrade.check"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/upgrade", CommandName: "system.upgrade.run"},

	{PathPattern: "/api/environments/{id}/ws/projects/{projectId}/logs", CommandName: "project.logs.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/{containerId}/logs", CommandName: "container.logs.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/{containerId}/stats", CommandName: "container.stats.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/{containerId}/terminal", CommandName: "container.exec.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/system/stats", CommandName: "system.stats.stream", Stream: true},
}

func ResolveEdgeCommandName(method, requestPath string, stream bool) (string, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	requestPath = normalizeCommandPathInternal(requestPath)

	for _, route := range commandRoutes {
		if route.Stream != stream {
			continue
		}
		if !stream && route.Method != method {
			continue
		}
		if matchCommandPathInternal(route.PathPattern, requestPath) {
			return route.CommandName, true
		}
	}

	return "", false
}

func ValidateEdgeCommand(commandName, method, requestPath string, stream bool) bool {
	resolved, ok := ResolveEdgeCommandName(method, requestPath, stream)
	return ok && resolved == strings.TrimSpace(commandName)
}

func AdvertisedEdgeCommands() []string {
	seen := make(map[string]struct{}, len(commandRoutes))
	commands := make([]string, 0, len(commandRoutes))
	for _, route := range commandRoutes {
		if _, ok := seen[route.CommandName]; ok {
			continue
		}
		seen[route.CommandName] = struct{}{}
		commands = append(commands, route.CommandName)
	}
	return commands
}

func normalizeCommandPathInternal(requestPath string) string {
	if idx := strings.IndexByte(requestPath, '?'); idx >= 0 {
		requestPath = requestPath[:idx]
	}
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		return "/"
	}
	if requestPath[0] != '/' {
		return "/" + requestPath
	}
	return requestPath
}

func matchCommandPathInternal(pattern, requestPath string) bool {
	pattern = normalizeCommandPathInternal(pattern)
	requestPath = normalizeCommandPathInternal(requestPath)

	if pattern == requestPath {
		return true
	}

	patternParts := splitCommandPathInternal(pattern)
	requestParts := splitCommandPathInternal(requestPath)
	if len(patternParts) != len(requestParts) {
		return false
	}

	for i := range patternParts {
		pp := patternParts[i]
		rp := requestParts[i]
		if isCommandParamInternal(pp) {
			if rp == "" {
				return false
			}
			continue
		}
		if pp != rp {
			return false
		}
	}

	return true
}

func splitCommandPathInternal(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func isCommandParamInternal(segment string) bool {
	return strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}")
}

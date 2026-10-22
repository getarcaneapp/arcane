package edge

import (
	"cmp"
	"net/http"
	"strings"

	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
)

var commandRoutes = []commandRoute{
	{Method: http.MethodGet, PathPattern: "/api/health", CommandName: "system.health"},
	{Method: http.MethodHead, PathPattern: "/api/health", CommandName: "system.health"},
	{Method: http.MethodGet, PathPattern: "/api/app/version", CommandName: "system.version"},
	{Method: http.MethodGet, PathPattern: "/api/swarm/identity", CommandName: "swarm.node_identity"},
	{Method: http.MethodPost, PathPattern: "/api/registries/sync", CommandName: "container_registry.sync"},
	{Method: http.MethodPost, PathPattern: "/api/git/repositories/sync", CommandName: "git_repository.sync"},
	{Method: http.MethodPost, PathPattern: "/api/backups/s3/sync", CommandName: "s3_destination.sync"},
	{Method: http.MethodGet, PathPattern: "/api/backups/s3/{id}/usage", CommandName: "s3_destination.in_use"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers", CommandName: "container.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/counts", CommandName: "container.counts"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers", CommandName: "container.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/{containerId}", CommandName: "container.inspect"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/{containerId}/config", CommandName: "container.edit_config"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/containers/{containerId}/config", CommandName: "container.edit"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}", CommandName: "container.action"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/redeploy", CommandName: "container.redeploy"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/containers/{containerId}", CommandName: "container.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/update", CommandName: "container.update"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/containers/{containerId}/autoupdate", CommandName: "container.auto_update.set"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/{containerId}/processes", CommandName: "container.processes"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/containers/{containerId}/logs/download", CommandName: "container.logs.download"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/{containerId}/commit", CommandName: "container.commit"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/containers/compose", CommandName: "container.generate_compose"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/ports", CommandName: "port.list"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images", CommandName: "image.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/counts", CommandName: "image.counts"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}", CommandName: "image.inspect"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/images/{imageId}", CommandName: "image.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/pull", CommandName: "image.pull"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/build", CommandName: "image.build.start"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/builds", CommandName: "image.build.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/builds/{buildId}", CommandName: "image.build.inspect"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/patches", CommandName: "image.patch.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/patches/targets", CommandName: "image.patch.targets"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/prune", CommandName: "image.prune"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/upload", CommandName: "image.upload"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/history", CommandName: "image.history"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/attestations", CommandName: "image.attestations"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/export", CommandName: "image.export"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/{imageId}/tag", CommandName: "image.tag"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/{imageId}/patch", CommandName: "image.patch"},

	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/uploads/{kind}", CommandName: "upload.session.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/uploads/{kind}/{uploadId}", CommandName: "upload.session.get"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/uploads/{kind}/{uploadId}", CommandName: "upload.session.delete"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/uploads/{kind}/{uploadId}/chunks/{index}", CommandName: "upload.session.chunk"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/updates/check", CommandName: "image_update.check"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/updates/check/{imageId}", CommandName: "image_update.check_one"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/updates/check/{imageId}", CommandName: "image_update.check_one"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/updates/check", CommandName: "image_update.check_images"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/updates/summary", CommandName: "image_update.summary"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/updates/refs", CommandName: "image_update.by_refs"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/activities", CommandName: "activity.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/activities/{activityId}", CommandName: "activity.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/activities/{activityId}", CommandName: "activity.action"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/activities/history", CommandName: "activity.history.clear"},

	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities/scan", CommandName: "vulnerability.scan"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities", CommandName: "vulnerability.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities/summary", CommandName: "vulnerability.summary"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/images/vulnerabilities/summaries", CommandName: "vulnerability.summary"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/images/{imageId}/vulnerabilities/list", CommandName: "vulnerability.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/scanner", CommandName: "vulnerability.scanner_status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/summary", CommandName: "vulnerability.summary"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/overview", CommandName: "vulnerability.overview"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/all", CommandName: "vulnerability.all"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/export", CommandName: "vulnerability.export"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/images", CommandName: "vulnerability.image_options"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/vulnerabilities/ignored", CommandName: "vulnerability.ignore.create"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/vulnerabilities/ignored/{ignoreId}", CommandName: "vulnerability.ignore.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/vulnerabilities/ignored", CommandName: "vulnerability.ignore.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/vulnerabilities/ignored/bulk", CommandName: "vulnerability.ignore.bulk.action"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects", CommandName: "project.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/counts", CommandName: "project.counts"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}", CommandName: "project.action"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/deploy", CommandName: "project.deploy"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects", CommandName: "project.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}", CommandName: "project.inspect"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}/compose", CommandName: "project.compose"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}/workspace", CommandName: "project.workspace.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}/runtime", CommandName: "project.runtime"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}/updates", CommandName: "project.updates"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}/workspace/file", CommandName: "project.workspace.read_file"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/projects/{projectId}/workspace/file/download", CommandName: "project.workspace.download"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/projects/{projectId}/workspace", CommandName: "project.workspace.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/projects/{projectId}", CommandName: "project.destroy"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/projects/{projectId}", CommandName: "project.update"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/projects/{projectId}/build", CommandName: "project.build"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/projects/{projectId}/tags", CommandName: "project.tags.update"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs", CommandName: "gitops_sync.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops/syncs", CommandName: "gitops_sync.create"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops/syncs/import", CommandName: "gitops_sync.import"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}", CommandName: "gitops_sync.inspect"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}", CommandName: "gitops_sync.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}", CommandName: "gitops_sync.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}", CommandName: "gitops_sync.action"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}/status", CommandName: "gitops_sync.status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}/files", CommandName: "gitops_sync.files"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}/backup/history", CommandName: "gitops_sync.backup.history"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}/backup/history/{commit}", CommandName: "gitops_sync.backup.history.inspect"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}/backup/preview", CommandName: "gitops_sync.backup.preview"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/gitops/syncs/{syncId}/backup/resolve", CommandName: "gitops_sync.backup.resolve"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/counts", CommandName: "volume.counts"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes", CommandName: "volume.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}", CommandName: "volume.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes", CommandName: "volume.create"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/volumes/{volumeName}", CommandName: "volume.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/prune", CommandName: "volume.prune"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/usage", CommandName: "volume.usage"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/rename", CommandName: "volume.rename"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/sizes", CommandName: "volume.sizes"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/workspace", CommandName: "volume.workspace.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/workspace/file", CommandName: "volume.workspace.read_file"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/workspace/file/download", CommandName: "volume.workspace.download"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/volumes/{volumeName}/workspace", CommandName: "volume.workspace.update"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups", CommandName: "volume.backup.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups", CommandName: "volume.backup.create"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/{backupId}", CommandName: "volume.backup.action"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}", CommandName: "volume.backup.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/download", CommandName: "volume.backup.download"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/policy", CommandName: "volume.backup_policy.get"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/policy", CommandName: "volume.backup_policy.update"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/files/exists", CommandName: "volume.backup.has_path"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/files", CommandName: "volume.backup.files"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/files/browse", CommandName: "volume.backup.files_browse"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/backups/{backupId}/upload", CommandName: "volume.backup.upload"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/{volumeName}/backups/upload", CommandName: "volume.backup.import"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/volumes/backups/discover", CommandName: "volume.backup.discover"},

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
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/networks/topology", CommandName: "network.topology"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/networks/{networkId}", CommandName: "network.action"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/templates/variables", CommandName: "template.variables.get"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/templates/variables", CommandName: "template.variables.update"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/settings/public", CommandName: "settings.public.get"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/settings", CommandName: "settings.get"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/settings", CommandName: "settings.update"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/jobs/schedules", CommandName: "job_schedule.list"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/jobs/schedules", CommandName: "job_schedule.upsert"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/jobs", CommandName: "job.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/jobs/{jobId}", CommandName: "job.action"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/jobs/{jobId}/runs", CommandName: "job.run.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/jobs/{jobId}/runs/{runId}", CommandName: "job.run.inspect"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/jobs/{jobId}/runs/{runId}", CommandName: "job.run.action"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/notifications/settings", CommandName: "notification.settings.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/notifications/settings/{provider}", CommandName: "notification.settings.get"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/notifications/settings", CommandName: "notification.settings.upsert"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/notifications/settings/{provider}", CommandName: "notification.settings.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/notifications/settings/{provider}", CommandName: "notification.settings.action"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/dashboard", CommandName: "dashboard.snapshot"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/dashboard/action-items", CommandName: "dashboard.action_items"},

	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/updater/run", CommandName: "updater.run"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/updater/status", CommandName: "updater.status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/updater/history", CommandName: "updater.history"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/updater/projects/{projectId}/check", CommandName: "updater.project.check"},

	{Method: http.MethodHead, PathPattern: "/api/environments/{id}/system/health", CommandName: "system.health"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/system/docker/info", CommandName: "system.docker_info"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/prune", CommandName: "system.prune"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/containers", CommandName: "system.containers.action"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/convert", CommandName: "system.convert"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/system/upgrade/check", CommandName: "system.upgrade.check"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/system/upgrade", CommandName: "system.upgrade.run"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/system/upgrade/progress", CommandName: "system.upgrade.progress"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/diagnostics/actors", CommandName: "diagnostics.actors"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/diagnostics/goroutines", CommandName: "diagnostics.goroutines"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/services", CommandName: "swarm.service.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/services", CommandName: "swarm.service.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/services/{serviceId}", CommandName: "swarm.service.inspect"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/swarm/services/{serviceId}", CommandName: "swarm.service.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/swarm/services/{serviceId}", CommandName: "swarm.service.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/services/{serviceId}/tasks", CommandName: "swarm.service.tasks"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/services/{serviceId}", CommandName: "swarm.service.action"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/swarm/services/{serviceId}", CommandName: "swarm.service.scale"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/nodes", CommandName: "swarm.node.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/nodes/{nodeId}", CommandName: "swarm.node.inspect"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/swarm/nodes/{nodeId}", CommandName: "swarm.node.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/swarm/nodes/{nodeId}", CommandName: "swarm.node.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/nodes/{nodeId}/agent/deployment", CommandName: "swarm.node.agent_deployment"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/nodes/{nodeId}", CommandName: "swarm.node.action"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/nodes/{nodeId}/tasks", CommandName: "swarm.node.tasks"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/tasks", CommandName: "swarm.task.list"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/stacks", CommandName: "swarm.stack.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/stacks", CommandName: "swarm.stack.deploy"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/stacks/{name}", CommandName: "swarm.stack.inspect"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/swarm/stacks/{name}", CommandName: "swarm.stack.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/stacks/{name}/source", CommandName: "swarm.stack.source.get"},
	{Method: http.MethodPatch, PathPattern: "/api/environments/{id}/swarm/stacks/{name}/source", CommandName: "swarm.stack.source.update"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/stacks/{name}/services", CommandName: "swarm.stack.services"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/stacks/{name}/tasks", CommandName: "swarm.stack.tasks"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/stacks/config/render", CommandName: "swarm.stack.config.render"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/status", CommandName: "swarm.status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/info", CommandName: "swarm.info"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/init", CommandName: "swarm.init"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/join", CommandName: "swarm.join"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/leave", CommandName: "swarm.leave"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/unlock", CommandName: "swarm.unlock"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/unlock/key", CommandName: "swarm.unlock_key"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/join/tokens", CommandName: "swarm.join_tokens.get"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/join/tokens", CommandName: "swarm.join_tokens.action"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/swarm/spec", CommandName: "swarm.spec.update"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/configs", CommandName: "swarm.config.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/configs", CommandName: "swarm.config.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/configs/{configId}", CommandName: "swarm.config.inspect"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/swarm/configs/{configId}", CommandName: "swarm.config.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/swarm/configs/{configId}", CommandName: "swarm.config.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/secrets", CommandName: "swarm.secret.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/swarm/secrets", CommandName: "swarm.secret.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/swarm/secrets/{secretId}", CommandName: "swarm.secret.inspect"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/swarm/secrets/{secretId}", CommandName: "swarm.secret.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/swarm/secrets/{secretId}", CommandName: "swarm.secret.delete"},

	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/status", CommandName: "podman.status"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/info", CommandName: "podman.info"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/autoupdate", CommandName: "podman.autoupdate"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/pods", CommandName: "podman.pod.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/pods", CommandName: "podman.pod.create"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/pods/prune", CommandName: "podman.pod.prune"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/pods/stats", CommandName: "podman.pod.stats"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/pods/{podId}", CommandName: "podman.pod.inspect"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/pods/{podId}/top", CommandName: "podman.pod.top"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/pods/{podId}", CommandName: "podman.pod.action"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/podman/pods/{podId}", CommandName: "podman.pod.delete"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/quadlets", CommandName: "podman.quadlet.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/quadlets", CommandName: "podman.quadlet.install"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/quadlets/{name}", CommandName: "podman.quadlet.inspect"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/quadlets/{name}/file", CommandName: "podman.quadlet.file"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/podman/quadlets/{name}", CommandName: "podman.quadlet.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/podman/quadlets/{name}", CommandName: "podman.quadlet.delete"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/quadlets/{name}", CommandName: "podman.quadlet.action"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/kube/play", CommandName: "podman.kube.play"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/kube/down", CommandName: "podman.kube.down"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/kube/generate", CommandName: "podman.kube.generate"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/secrets", CommandName: "podman.secret.list"},
	{Method: http.MethodPost, PathPattern: "/api/environments/{id}/podman/secrets", CommandName: "podman.secret.create"},
	{Method: http.MethodGet, PathPattern: "/api/environments/{id}/podman/secrets/{secretId}", CommandName: "podman.secret.inspect"},
	{Method: http.MethodPut, PathPattern: "/api/environments/{id}/podman/secrets/{secretId}", CommandName: "podman.secret.update"},
	{Method: http.MethodDelete, PathPattern: "/api/environments/{id}/podman/secrets/{secretId}", CommandName: "podman.secret.delete"},

	{PathPattern: "/api/environments/{id}/ws/projects/{projectId}/logs", CommandName: "project.logs.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/stats", CommandName: "container.stats.batch.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/{containerId}/logs", CommandName: "container.logs.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/{containerId}/stats", CommandName: "container.stats.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/containers/{containerId}/terminal", CommandName: "container.exec.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/swarm/services/{serviceId}/logs", CommandName: "swarm.service.logs.stream", Stream: true},
	{PathPattern: "/api/environments/{id}/ws/system/stats", CommandName: "system.stats.stream", Stream: true},
}

var commandRoutesIndex = buildCommandRouteIndex(commandRoutes)

// ResolveEdgeCommandName returns the edge command for a request, or None when the allowlist does not cover it.
func ResolveEdgeCommandName(method, requestPath string, stream bool) mo.Option[string] {
	requestPath, _, _ = strings.Cut(strings.TrimSpace(requestPath), "?")
	root := commandRoutesIndex[commandRouteKey{Method: kit.Ternary(stream, "", strings.ToUpper(strings.TrimSpace(method))), Stream: stream}]
	route := root.match(strings.Split(strings.Trim(requestPath, "/"), "/"))
	if route == nil {
		return mo.None[string]()
	}
	return mo.Some(route.CommandName)
}

func (n *commandRouteNode) match(parts []string) *commandRoute {
	if n == nil {
		return nil
	}
	if len(parts) == 0 {
		return n.route
	}
	if route := n.static[parts[0]].match(parts[1:]); route != nil {
		return route
	}
	if parts[0] == "" {
		return nil
	}
	return n.param.match(parts[1:])
}

// buildCommandRouteIndex builds one path trie per method (or for streams), panicking on duplicate routes.
func buildCommandRouteIndex(routes []commandRoute) commandRouteIndex {
	index := make(commandRouteIndex)
	for i := range routes {
		route := &routes[i]
		key := commandRouteKey{Method: kit.Ternary(route.Stream, "", route.Method), Stream: route.Stream}
		current := index[key]
		if current == nil {
			current = &commandRouteNode{}
			index[key] = current
		}
		for part := range strings.SplitSeq(strings.Trim(route.PathPattern, "/"), "/") {
			if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
				current.param = cmp.Or(current.param, &commandRouteNode{})
				current = current.param
				continue
			}
			if current.static == nil {
				current.static = make(map[string]*commandRouteNode)
			}
			if current.static[part] == nil {
				current.static[part] = &commandRouteNode{}
			}
			current = current.static[part]
		}
		if current.route != nil {
			panic("duplicate edge command route: " + route.PathPattern)
		}
		current.route = route
	}
	return index
}

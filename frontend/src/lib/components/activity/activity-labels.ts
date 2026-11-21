import { jobNameLabel, jobStatusLabel } from '#lib/components/job-card/job-status.js';
import { m } from '#lib/paraglide/messages.js';
import type { Activity, ActivityStatus, ActivityType } from '#lib/types/activity.js';
import { parseInstant } from '#lib/utils/formatting.js';

const statusDisplay = new Map(
	Object.entries({
		queued: { label: m.activity_status_queued, accentClass: 'bg-warning' },
		running: { label: m.common_running, accentClass: 'bg-info' },
		success: { label: m.common_success, accentClass: 'bg-success' },
		failed: { label: m.common_failed, accentClass: 'bg-destructive' },
		cancelled: { label: m.activity_status_cancelled, accentClass: 'bg-muted-foreground/40' }
	} satisfies Record<ActivityStatus, { label: () => string; accentClass: string }>)
);

const typeDisplay = new Map(
	Object.entries({
		job_run: m.activity_type_job_run,
		image_pull: m.activity_type_image_pull,
		image_build: m.activity_type_image_build,
		image_update_check: m.activity_type_image_update_check,
		image_patch: m.activity_type_image_patch,
		project_pull: m.activity_type_project_pull,
		project_build: m.activity_type_project_build,
		project_deploy: m.activity_type_project_deploy,
		project_redeploy: m.activity_type_project_redeploy,
		project_down: m.activity_type_project_down,
		project_restart: m.activity_type_project_restart,
		project_destroy: m.activity_type_project_destroy,
		project_job_run: m.activity_type_project_job_run,
		container_start: m.activity_type_container_start,
		container_stop: m.activity_type_container_stop,
		container_restart: m.activity_type_container_restart,
		container_redeploy: m.activity_type_container_redeploy,
		container_edit: m.activity_type_container_edit,
		container_delete: m.activity_type_container_delete,
		container_kill: m.activity_type_container_kill,
		container_pause: m.activity_type_container_pause,
		container_unpause: m.activity_type_container_unpause,
		vulnerability_scan: m.activity_type_vulnerability_scan,
		auto_update: m.auto_update,
		system_prune: m.activity_type_system_prune,
		resource_action: m.activity_type_resource_action
	} satisfies Record<ActivityType, () => string>)
);

const resourceActionDisplay = new Map<string, () => string>(
	Object.entries({
		create_network: m.create_network,
		remove_network: m.remove_network,
		prune_networks: m.prune_networks,
		create_volume: m.create_volume,
		remove_volume: m.remove_volume,
		rename_volume: m.volumes_rename_title,
		prune_volumes: m.prune_volumes,
		update_volume_workspace: m.update_volume_workspace,
		create_project: m.compose_create_project,
		update_project: m.update_project,
		update_project_workspace: m.update_project_workspace,
		update_tags: m.update_tags,
		sync_environment: m.sync_environment,
		create_volume_backup: m.volumes_backup_create,
		delete_volume_backup: m.delete_volume_backup,
		upload_volume_backup: m.upload_volume_backup,
		restore_volume_backup: m.volumes_backup_restore_title,
		restore_volume_backup_files: m.restore_volume_files,
		upload_restore_volume_backup: m.upload_and_restore_volume_backup,
		update_volume_backup_policy: m.update_volume_backup_policy,
		scheduled_volume_backup: m.scheduled_volume_backup,
		create_system_backup: m.create_system_backup,
		scheduled_system_backup: m.scheduled_system_backup,
		delete_system_backup: m.delete_system_backup,
		upload_system_backup: m.upload_system_backup,
		restore_system_backup: m.system_backups_restore_title,
		restore_system_backup_files: m.restore_system_backup_files,
		discover_system_backups: m.discover_system_backups,
		set_system_backup_recovery_key: m.set_recovery_key,
		update_system_backup_policies: m.update_system_backup_policies,
		run_system_volume_backups: m.back_up_volumes,
		scheduled_system_volume_backups: m.scheduled_volume_backups,
		transfer_project_move: m.move_project,
		transfer_project_copy: m.copy_project,
		import_project: m.import_project,
		export_project: m.export_project,
		connect_network: m.connect_container,
		disconnect_network: m.disconnect_container
	})
);

export function activityTitle(activity: Activity): string {
	const metadata = activity.metadata ?? {};
	const jobId = metadata['jobId'];
	if (activity.type === 'job_run' && typeof jobId === 'string') {
		return jobNameLabel({ id: jobId, name: activity.resourceName || jobId });
	}
	const action = activity.type === 'resource_action' ? metadata['action'] : undefined;
	const key = action === 'transfer_project' ? `${action}_${String(metadata['mode'])}` : action;
	return (typeof key === 'string' ? resourceActionDisplay.get(key)?.() : undefined) ?? activityTypeLabel(activity.type);
}

export function activityTarget(activity: Activity): string {
	return activity.type === 'job_run' ? '' : activity.resourceName || activity.resourceId || '';
}

export function activityStatusLabel(status: ActivityStatus): string {
	return statusDisplay.get(status)?.label() as string;
}

export function activityTypeLabel(type: ActivityType): string {
	return typeDisplay.get(type)?.() as string;
}

export function activityStatusAccentClass(status: ActivityStatus): string {
	return statusDisplay.get(status)?.accentClass as string;
}

export function activityRunStatusLabel(activity: Activity): string {
	const jobStatus = activity.metadata?.['jobStatus'];
	if (activity.type === 'job_run' && typeof jobStatus === 'string') return jobStatusLabel(jobStatus);
	return activityStatusLabel(activity.status);
}

export function activityDurationLabel(activity: Activity | null | undefined): string {
	const startedAt = parseInstant(activity?.startedAt);
	const durationMs = activity?.durationMs ?? (startedAt ? startedAt.until(Temporal.Now.instant()).total('milliseconds') : 0);
	if (!durationMs || Number.isNaN(durationMs)) {
		return m.common_na();
	}
	if (durationMs < 1000) {
		return m.activity_duration_ms({ ms: Math.max(0, Math.round(durationMs)) });
	}

	const totalSeconds = Math.round(durationMs / 1000);
	if (totalSeconds < 60) {
		return m.activity_duration_seconds({ seconds: totalSeconds });
	}

	const minutes = Math.floor(totalSeconds / 60);
	const seconds = totalSeconds % 60;
	return m.activity_duration_minutes({ minutes, seconds });
}

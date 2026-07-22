import { m } from '$lib/paraglide/messages';
import type { ActivityStatus, ActivityType } from '$lib/types/activity.type';
import type { IconType } from '$lib/icons';
import {
	ActivityIcon,
	DownloadIcon,
	HammerIcon,
	RedeployIcon,
	RefreshIcon,
	RestartIcon,
	ScanIcon,
	StartIcon,
	StopIcon,
	TrashIcon
} from '$lib/icons';

export type ActivityBadgeVariant = 'red' | 'green' | 'blue' | 'gray' | 'amber' | 'purple';

export function activityStatusLabel(status: ActivityStatus): string {
	switch (status) {
		case 'queued':
			return m.activity_status_queued();
		case 'running':
			return m.common_running();
		case 'success':
			return m.common_success();
		case 'failed':
			return m.common_failed();
		case 'cancelled':
			return m.activity_status_cancelled();
	}
}

export function activityStatusVariant(status: ActivityStatus): ActivityBadgeVariant {
	switch (status) {
		case 'queued':
			return 'amber';
		case 'running':
			return 'blue';
		case 'success':
			return 'green';
		case 'failed':
			return 'red';
		case 'cancelled':
			return 'gray';
	}
}

export function activityTypeLabel(type: ActivityType): string {
	switch (type) {
		case 'image_pull':
			return m.activity_type_image_pull();
		case 'image_build':
			return m.activity_type_image_build();
		case 'image_update_check':
			return m.activity_type_image_update_check();
		case 'project_pull':
			return m.activity_type_project_pull();
		case 'project_build':
			return m.activity_type_project_build();
		case 'project_deploy':
			return m.activity_type_project_deploy();
		case 'project_redeploy':
			return m.activity_type_project_redeploy();
		case 'project_down':
			return m.activity_type_project_down();
		case 'project_restart':
			return m.activity_type_project_restart();
		case 'project_destroy':
			return m.activity_type_project_destroy();
		case 'container_start':
			return m.activity_type_container_start();
		case 'container_stop':
			return m.activity_type_container_stop();
		case 'container_restart':
			return m.activity_type_container_restart();
		case 'container_redeploy':
			return m.activity_type_container_redeploy();
		case 'container_delete':
			return m.activity_type_container_delete();
		case 'vulnerability_scan':
			return m.activity_type_vulnerability_scan();
		case 'auto_update':
			return m.auto_update();
		case 'system_prune':
			return m.activity_type_system_prune();
		case 'resource_action':
			return m.activity_type_resource_action();
	}
}

export function activityTypeIcon(type: ActivityType): IconType {
	switch (type) {
		case 'image_pull':
		case 'project_pull':
			return DownloadIcon;
		case 'image_build':
		case 'project_build':
			return HammerIcon;
		case 'image_update_check':
			return RefreshIcon;
		case 'project_deploy':
			return ActivityIcon;
		case 'container_start':
			return StartIcon;
		case 'project_redeploy':
		case 'container_redeploy':
			return RedeployIcon;
		case 'project_down':
		case 'container_stop':
			return StopIcon;
		case 'project_restart':
		case 'container_restart':
			return RestartIcon;
		case 'project_destroy':
		case 'container_delete':
			return TrashIcon;
		case 'vulnerability_scan':
			return ScanIcon;
		case 'auto_update':
			return RefreshIcon;
		case 'system_prune':
			return TrashIcon;
		case 'resource_action':
			return ActivityIcon;
		default:
			return ActivityIcon;
	}
}

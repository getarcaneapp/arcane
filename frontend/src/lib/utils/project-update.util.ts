import { m } from '$lib/paraglide/messages';
import type { ProjectUpdateInfo } from '$lib/types/project.type';

type ProjectUpdateStatus = ProjectUpdateInfo['status'];
export type ProjectUpdateBadgeVariant = 'blue' | 'green' | 'gray' | 'red';

export function getProjectUpdateStatus(updateInfo?: ProjectUpdateInfo): ProjectUpdateStatus {
	return updateInfo?.status ?? 'unknown';
}

export function getProjectUpdateText(updateInfo?: ProjectUpdateInfo): string {
	switch (getProjectUpdateStatus(updateInfo)) {
		case 'has_update':
			return m.images_has_updates();
		case 'up_to_date':
			return m.image_update_up_to_date_title();
		case 'error':
			return m.common_error();
		default:
			return m.image_update_status_unknown();
	}
}

export function getProjectUpdateVariant(updateInfo?: ProjectUpdateInfo): ProjectUpdateBadgeVariant {
	switch (getProjectUpdateStatus(updateInfo)) {
		case 'has_update':
			return 'blue';
		case 'up_to_date':
			return 'green';
		case 'error':
			return 'red';
		default:
			return 'gray';
	}
}

export function getProjectUpdateTooltip(updateInfo?: ProjectUpdateInfo): string | undefined {
	switch (getProjectUpdateStatus(updateInfo)) {
		case 'error':
			return m.image_update_check_failed_title();
		case 'unknown':
			return m.image_update_click_to_check();
		default:
			return undefined;
	}
}

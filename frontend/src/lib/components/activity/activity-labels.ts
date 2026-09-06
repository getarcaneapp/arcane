import { m } from '#lib/paraglide/messages.js';
import type { ActivityStatus, ActivityType } from '#lib/types/activity.type.js';
import type { IconType } from '#lib/icons/index.js';
import {
	ActivityIcon,
	DownloadIcon,
	HammerIcon,
	RedeployIcon,
	RefreshIcon,
	RestartIcon,
	ScanIcon,
	ShieldCheckIcon,
	StartIcon,
	StopIcon,
	TrashIcon
} from '#lib/icons/index.js';

export type ActivityBadgeVariant = 'red' | 'green' | 'blue' | 'gray' | 'amber' | 'purple';

const statusDisplay = new Map(
	Object.entries({
		queued: { label: m.activity_status_queued, variant: 'amber', accentClass: 'bg-amber-500' },
		running: { label: m.common_running, variant: 'blue', accentClass: 'bg-blue-500' },
		success: { label: m.common_success, variant: 'green', accentClass: 'bg-emerald-500' },
		failed: { label: m.common_failed, variant: 'red', accentClass: 'bg-red-500' },
		cancelled: { label: m.activity_status_cancelled, variant: 'gray', accentClass: 'bg-muted-foreground/40' }
	} satisfies Record<ActivityStatus, { label: () => string; variant: ActivityBadgeVariant; accentClass: string }>)
);

const typeDisplay = new Map(
	Object.entries({
		image_pull: { label: m.activity_type_image_pull, icon: DownloadIcon },
		image_build: { label: m.activity_type_image_build, icon: HammerIcon },
		image_update_check: { label: m.activity_type_image_update_check, icon: RefreshIcon },
		image_patch: { label: m.activity_type_image_patch, icon: ShieldCheckIcon },
		project_pull: { label: m.activity_type_project_pull, icon: DownloadIcon },
		project_build: { label: m.activity_type_project_build, icon: HammerIcon },
		project_deploy: { label: m.activity_type_project_deploy, icon: ActivityIcon },
		project_redeploy: { label: m.activity_type_project_redeploy, icon: RedeployIcon },
		project_down: { label: m.activity_type_project_down, icon: StopIcon },
		project_restart: { label: m.activity_type_project_restart, icon: RestartIcon },
		project_destroy: { label: m.activity_type_project_destroy, icon: TrashIcon },
		container_start: { label: m.activity_type_container_start, icon: StartIcon },
		container_stop: { label: m.activity_type_container_stop, icon: StopIcon },
		container_restart: { label: m.activity_type_container_restart, icon: RestartIcon },
		container_redeploy: { label: m.activity_type_container_redeploy, icon: RedeployIcon },
		container_delete: { label: m.activity_type_container_delete, icon: TrashIcon },
		vulnerability_scan: { label: m.activity_type_vulnerability_scan, icon: ScanIcon },
		auto_update: { label: m.auto_update, icon: RefreshIcon },
		system_prune: { label: m.activity_type_system_prune, icon: TrashIcon },
		resource_action: { label: m.activity_type_resource_action, icon: ActivityIcon }
	} satisfies Record<ActivityType, { label: () => string; icon: IconType }>)
);

export function activityStatusLabel(status: ActivityStatus): string {
	return statusDisplay.get(status)?.label() as string;
}

export function activityStatusVariant(status: ActivityStatus): ActivityBadgeVariant {
	return statusDisplay.get(status)?.variant as ActivityBadgeVariant;
}

export function activityTypeLabel(type: ActivityType): string {
	return typeDisplay.get(type)?.label() as string;
}

export function activityTypeIcon(type: ActivityType): IconType {
	return typeDisplay.get(type)?.icon ?? ActivityIcon;
}

export function activityStatusAccentClass(status: ActivityStatus): string {
	return statusDisplay.get(status)?.accentClass as string;
}

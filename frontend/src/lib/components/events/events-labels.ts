import { m } from '$lib/paraglide/messages';
import type { IconType } from '$lib/icons';
import {
	ClockIcon,
	ContainersIcon,
	EnvironmentsIcon,
	EventsIcon,
	GitBranchIcon,
	GlobeIcon,
	ImagesIcon,
	NetworksIcon,
	NotificationsIcon,
	ProjectsIcon,
	SettingsIcon,
	UserIcon,
	VolumesIcon
} from '$lib/icons';
import type { FilterOption } from '$lib/components/arcane-table';

export type EventBadgeVariant = 'blue' | 'green' | 'amber' | 'red';

export function eventSeverityVariant(severity: string): EventBadgeVariant {
	switch (severity) {
		case 'success':
			return 'green';
		case 'error':
			return 'red';
		case 'warning':
			return 'amber';
		case 'info':
		default:
			return 'blue';
	}
}

export function eventSeverityLabel(severity: string): string {
	switch (severity) {
		case 'success':
			return m.events_success();
		case 'error':
			return m.events_error();
		case 'warning':
			return m.events_warning();
		case 'info':
		default:
			return m.events_info();
	}
}

export function eventSeverityIconVariant(severity: string): 'emerald' | 'red' | 'amber' | 'blue' {
	switch (eventSeverityVariant(severity)) {
		case 'green':
			return 'emerald';
		case 'red':
			return 'red';
		case 'amber':
			return 'amber';
		default:
			return 'blue';
	}
}

export function eventTypeCategory(type: string): string {
	return type.split('.')[0] ?? '';
}

const categoryLabels: Record<string, () => string> = {
	container: m.resource_container_cap,
	image: m.resource_image_cap,
	project: m.project,
	git: m.git_title,
	volume: m.resource_volume_cap,
	network: m.resource_network_cap,
	environment: m.resource_environment_cap,
	user: m.resource_user_cap,
	system: m.sidebar_system_mode,
	webhook: m.events_category_webhook,
	notification: m.events_category_notification,
	lifecycle: m.security_lifecycle_tab
};

export function eventTypeCategoryLabel(category: string): string {
	return categoryLabels[category]?.() ?? humanize(category);
}

const categoryIcons: Record<string, IconType> = {
	container: ContainersIcon,
	image: ImagesIcon,
	project: ProjectsIcon,
	git: GitBranchIcon,
	volume: VolumesIcon,
	network: NetworksIcon,
	environment: EnvironmentsIcon,
	user: UserIcon,
	system: SettingsIcon,
	webhook: GlobeIcon,
	notification: NotificationsIcon,
	lifecycle: ClockIcon
};

export function eventTypeIcon(type: string): IconType {
	return categoryIcons[eventTypeCategory(type)] ?? EventsIcon;
}

const actionLabels: Record<string, () => string> = {
	start: m.common_started,
	stop: m.common_stopped,
	restart: m.events_action_restart,
	create: m.common_created,
	delete: m.events_action_delete,
	update: m.common_updated,
	deploy: m.events_action_deploy,
	pull: m.events_action_pull,
	scan: m.events_action_scan,
	error: m.common_error,
	login: m.events_action_login,
	logout: m.common_logout,
	send: m.events_action_send,
	execute: m.events_action_execute,
	run: m.webhook_action_type_run,
	prune: m.events_action_prune
};

function humanize(value: string): string {
	const words = value.replace(/[._]/g, ' ').trim();
	return words ? words.charAt(0).toUpperCase() + words.slice(1) : value;
}

export function eventTypeLabel(type: string): string {
	const [category, ...rest] = type.split('.');
	const tail = rest.join('.');
	if (!category || !tail) {
		return humanize(type);
	}
	const action = actionLabels[tail]?.() ?? humanize(tail);
	return `${eventTypeCategoryLabel(category)} · ${action}`;
}

export const eventTypeFilters: FilterOption[] = Object.keys(categoryLabels).map((category) => ({
	value: category,
	label: eventTypeCategoryLabel(category),
	icon: categoryIcons[category]
}));

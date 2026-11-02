import { resolve } from '$app/paths';
import type { ResolvedPathname } from '$app/types';

import { ContainersIcon, ImagesIcon, NetworksIcon, ProjectsIcon, VolumesIcon, type IconType } from '#lib/icons/index.js';
import { m } from '#lib/paraglide/messages.js';
import type { DashboardResourceKind } from '#lib/types/auth.js';

export type ResourceFilterKey = 'status' | 'updates' | 'inUse' | 'label';

export type ResourceOption = { value: string; label: () => string };

export interface ResourceKindDefinition {
	label: () => string;
	icon: IconType;
	permission: string;
	listHref: () => ResolvedPathname;
	filters: ResourceFilterKey[];
	statuses: ResourceOption[];
}

export const RESOURCE_KINDS: Record<DashboardResourceKind, ResourceKindDefinition> = {
	containers: {
		label: m.containers,
		icon: ContainersIcon,
		permission: 'containers:list',
		listHref: () => resolve('/(app)/containers'),
		filters: ['status', 'updates', 'label'],
		statuses: [
			{ value: 'running', label: m.common_running },
			{ value: 'exited', label: m.containers_status_exited },
			{ value: 'paused', label: m.paused },
			{ value: 'restarting', label: m.containers_status_restarting },
			{ value: 'created', label: m.common_created },
			{ value: 'dead', label: m.containers_status_dead },
			{ value: 'stopped', label: m.common_stopped },
			{ value: 'unhealthy', label: m.health_status_unhealthy }
		]
	},
	projects: {
		label: m.projects_title,
		icon: ProjectsIcon,
		permission: 'projects:list',
		listHref: () => resolve('/(app)/projects'),
		filters: ['status', 'updates', 'label'],
		statuses: [
			{ value: 'running', label: m.common_running },
			{ value: 'partially running', label: m.projects_status_partial },
			{ value: 'stopped', label: m.common_stopped },
			{ value: 'deploying', label: m.deploying },
			{ value: 'stopping', label: m.stopping },
			{ value: 'restarting', label: m.containers_status_restarting },
			{ value: 'unknown', label: m.common_unknown }
		]
	},
	images: {
		label: m.images,
		icon: ImagesIcon,
		permission: 'images:list',
		listHref: () => resolve('/(app)/images'),
		filters: ['inUse', 'updates', 'label'],
		statuses: []
	},
	volumes: {
		label: m.resource_volumes_cap,
		icon: VolumesIcon,
		permission: 'volumes:list',
		listHref: () => resolve('/(app)/volumes'),
		filters: ['inUse', 'label'],
		statuses: []
	},
	networks: {
		label: m.resource_networks_cap,
		icon: NetworksIcon,
		permission: 'networks:list',
		listHref: () => resolve('/(app)/networks'),
		filters: ['inUse', 'label'],
		statuses: []
	}
};

export const RESOURCE_KIND_KEYS = Object.keys(RESOURCE_KINDS) as DashboardResourceKind[];

export const DEFAULT_RESOURCE_LIMIT = 5;

export const MAX_RESOURCE_LIMIT = 50;

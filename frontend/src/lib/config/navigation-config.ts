import { resolve } from '$app/paths';
import type { ResolvedPathname } from '$app/types';

import {
	ApiKeyIcon,
	UsersIcon,
	LockIcon,
	NotificationsIcon,
	DashboardIcon,
	ProjectsIcon,
	EnvironmentsIcon,
	RegistryIcon,
	ContainersIcon,
	ImagesIcon,
	NetworksIcon,
	VolumesIcon,
	HashIcon,
	DockIcon,
	JobsIcon,
	LayersIcon,
	EventsIcon,
	SettingsIcon,
	GitBranchIcon,
	ShieldAlertIcon,
	HammerIcon,
	TemplateIcon,
	GlobeIcon,
	UpdateIcon,
	VariableIcon,
	ActivityIcon,
	BackupIcon,
	BoxIcon,
	FileTextIcon,
	DockerBrandIcon,
	SecurityIcon,
	ClockIcon,
	CodeIcon,
	MonitorIcon
} from '#lib/icons/index.js';
import { m } from '#lib/paraglide/messages.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { PermissionsManifest, User } from '#lib/types/auth.js';
import type { FeatureID } from '#lib/types/features.js';
import type { PodmanStatus } from '#lib/types/podman.js';
import { canReachShownAccessSurface } from '#lib/utils/auth.js';
import type { ShortcutKey } from '#lib/utils/navigation.js';

export type NavigationItem = {
	title: string;
	url: ResolvedPathname;
	icon: any;
	shortcut?: ShortcutKey[];
	items?: NavigationItem[];
	accessSurfaceId?: string;
	adminOnly?: boolean;
	requiredFeature?: FeatureID;
};

export type NavigationSections = {
	overviewItems: NavigationItem[];
	deployItems: NavigationItem[];
	resourceItems: NavigationItem[];
	swarmItems: NavigationItem[];
	podmanItems: NavigationItem[];
	adminItems: NavigationItem[];
};

export const navigationItems: NavigationSections = {
	overviewItems: [
		{
			title: m.dashboard_title(),
			url: resolve('/(app)/dashboard'),
			icon: DashboardIcon,
			shortcut: ['mod', '1']
		},
		{
			title: m.updates(),
			url: resolve('/(app)/updates'),
			icon: UpdateIcon,
			shortcut: ['mod', 'u'],
			accessSurfaceId: 'route.updates'
		},
		{
			title: m.security(),
			url: resolve('/(app)/security'),
			requiredFeature: 'vulnerabilityManagement',
			icon: ShieldAlertIcon,
			accessSurfaceId: 'route.images.vulnerabilities'
		},
		{
			title: m.events_title(),
			url: resolve('/(app)/events'),
			icon: EventsIcon,
			shortcut: ['mod', '9'],
			accessSurfaceId: 'route.events'
		}
	],
	deployItems: [
		{
			title: m.projects_title(),
			url: resolve('/(app)/projects'),
			icon: ProjectsIcon,
			shortcut: ['mod', '2'],
			accessSurfaceId: 'route.projects',
			items: [
				{
					title: m.templates_title(),
					url: resolve('/(app)/customize/templates'),
					icon: TemplateIcon,
					accessSurfaceId: 'customize.category.templates'
				},
				{
					title: m.variables_title(),
					url: resolve('/(app)/customize/variables'),
					icon: VariableIcon,
					accessSurfaceId: 'customize.category.variables'
				}
			]
		},
		{
			title: m.git_syncs_title(),
			url: resolve('/(app)/gitops'),
			icon: GitBranchIcon,
			shortcut: ['mod', 'g'],
			accessSurfaceId: 'route.gitops',
			items: [
				{
					title: m.git_repositories_title(),
					url: resolve('/(app)/customize/git-repositories'),
					icon: GitBranchIcon,
					accessSurfaceId: 'customize.category.git-repositories'
				}
			]
		}
	],
	resourceItems: [
		{
			title: m.containers(),
			url: resolve('/(app)/containers'),
			icon: ContainersIcon,
			shortcut: ['mod', '5'],
			accessSurfaceId: 'route.containers'
		},
		{
			title: m.images(),
			url: resolve('/(app)/images'),
			icon: ImagesIcon,
			shortcut: ['mod', '6'],
			accessSurfaceId: 'route.images',
			items: [
				{ title: m.builds(), url: resolve('/(app)/images/builds'), icon: HammerIcon, accessSurfaceId: 'route.images.builds' },
				{
					title: m.registries_title(),
					url: resolve('/(app)/customize/registries'),
					icon: RegistryIcon,
					accessSurfaceId: 'customize.category.registries'
				}
			]
		},
		{
			title: m.resource_networks_cap(),
			url: resolve('/(app)/networks'),
			icon: NetworksIcon,
			shortcut: ['mod', '7'],
			accessSurfaceId: 'route.networks',
			items: [
				{ title: m.common_ports(), url: resolve('/(app)/networks/ports'), icon: HashIcon, accessSurfaceId: 'route.ports' },
				{
					title: m.networks_topology_button(),
					url: resolve('/(app)/networks/topology'),
					icon: GitBranchIcon,
					accessSurfaceId: 'route.networks.topology'
				}
			]
		},
		{
			title: m.resource_volumes_cap(),
			url: resolve('/(app)/volumes'),
			icon: VolumesIcon,
			shortcut: ['mod', '8'],
			accessSurfaceId: 'route.volumes'
		}
	],
	swarmItems: [
		{ title: 'Services', url: resolve('/(app)/swarm/services'), icon: DockIcon, accessSurfaceId: 'route.swarm.services' },
		{ title: 'Nodes', url: resolve('/(app)/swarm/nodes'), icon: UsersIcon, accessSurfaceId: 'route.swarm.nodes' },
		{ title: 'Tasks', url: resolve('/(app)/swarm/tasks'), icon: JobsIcon, accessSurfaceId: 'route.swarm.tasks' },
		{ title: 'Stacks', url: resolve('/(app)/swarm/stacks'), icon: LayersIcon, accessSurfaceId: 'route.swarm.stacks' },
		{ title: 'Cluster', url: resolve('/(app)/swarm/cluster'), icon: SettingsIcon, accessSurfaceId: 'route.swarm.cluster' },
		{ title: 'Configs', url: resolve('/(app)/swarm/configs'), icon: TemplateIcon, accessSurfaceId: 'route.swarm.configs' },
		{ title: 'Secrets', url: resolve('/(app)/swarm/secrets'), icon: LockIcon, accessSurfaceId: 'route.swarm.secrets' }
	],
	podmanItems: [
		{ title: m.podman_pods_title(), url: resolve('/(app)/podman/pods'), icon: BoxIcon, accessSurfaceId: 'route.podman' },
		{
			title: m.podman_quadlets_title(),
			url: resolve('/(app)/podman/quadlets'),
			icon: FileTextIcon,
			accessSurfaceId: 'route.podman.quadlets'
		},
		{ title: m.podman_kube_title(), url: resolve('/(app)/podman/kube'), icon: LayersIcon, accessSurfaceId: 'route.podman.kube' },
		{
			title: m.podman_secrets_title(),
			url: resolve('/(app)/podman/secrets'),
			icon: LockIcon,
			accessSurfaceId: 'route.podman.secrets'
		},
		{
			title: m.auto_update(),
			url: resolve('/(app)/podman/autoupdate'),
			icon: UpdateIcon,
			accessSurfaceId: 'route.podman.autoupdate'
		}
	],
	adminItems: [
		{
			title: m.environments_title(),
			url: resolve('/(app)/environments'),
			icon: EnvironmentsIcon,
			shortcut: ['mod', '3'],
			accessSurfaceId: 'route.environments'
		},
		{
			title: m.settings(),
			url: resolve('/(app)/settings'),
			icon: SettingsIcon,
			shortcut: ['mod', '0'],
			accessSurfaceId: 'landing.settings',
			items: [
				{
					title: m.storage_limits(),
					url: resolve('/(app)/settings/storage'),
					icon: VolumesIcon,
					accessSurfaceId: 'settings.category.storage'
				},
				{
					title: m.docker_title(),
					url: resolve('/(app)/settings/docker'),
					icon: DockerBrandIcon,
					accessSurfaceId: 'settings.category.docker'
				},
				{
					title: m.security(),
					url: resolve('/(app)/settings/security'),
					icon: SecurityIcon,
					accessSurfaceId: 'settings.category.security'
				},
				{
					title: m.timeouts_settings(),
					url: resolve('/(app)/settings/timeouts'),
					icon: ClockIcon,
					accessSurfaceId: 'settings.category.timeouts'
				},
				{
					title: m.activity(),
					url: resolve('/(app)/settings/activity'),
					icon: ActivityIcon,
					accessSurfaceId: 'settings.category.activity'
				},
				{
					title: m.builds(),
					url: resolve('/(app)/settings/builds'),
					icon: CodeIcon,
					accessSurfaceId: 'settings.category.build'
				},
				{
					title: m.automations(),
					url: resolve('/(app)/settings/automations'),
					icon: JobsIcon,
					accessSurfaceId: 'settings.category.jobschedule'
				}
			]
		},
		{
			title: m.system(),
			url: resolve('/(app)/system'),
			icon: MonitorIcon,
			accessSurfaceId: 'landing.system',
			items: [
				{
					title: m.api_key_page_title(),
					url: resolve('/(app)/system/api-keys'),
					icon: ApiKeyIcon,
					shortcut: ['mod', 'shift', '1'],
					accessSurfaceId: 'settings.category.apikeys'
				},
				{
					title: m.webhook_page_title(),
					url: resolve('/(app)/system/webhooks'),
					icon: GlobeIcon,
					accessSurfaceId: 'settings.category.webhooks'
				},
				{
					title: m.authentication(),
					url: resolve('/(app)/system/authentication'),
					icon: LockIcon,
					shortcut: ['mod', 'shift', '3'],
					accessSurfaceId: 'settings.category.authentication'
				},
				{
					title: m.notifications_title(),
					url: resolve('/(app)/system/notifications'),
					icon: NotificationsIcon,
					shortcut: ['mod', 'shift', '4'],
					accessSurfaceId: 'settings.category.notifications'
				},
				{
					title: m.system_backups_title(),
					url: resolve('/(app)/system/backups'),
					icon: BackupIcon,
					accessSurfaceId: 'settings.category.systembackups',
					adminOnly: true
				},
				{
					title: m.users_title(),
					url: resolve('/(app)/system/users'),
					icon: UsersIcon,
					shortcut: ['mod', 'shift', '8'],
					accessSurfaceId: 'settings.category.users'
				},
				{
					title: m.roles_title(),
					url: resolve('/(app)/system/roles'),
					icon: ShieldAlertIcon,
					accessSurfaceId: 'settings.category.roles'
				},
				{
					title: m.diagnostics(),
					url: resolve('/(app)/system/diagnostics'),
					icon: ActivityIcon,
					accessSurfaceId: 'settings.category.diagnostics'
				}
			]
		}
	]
};

for (const parent of navigationItems.adminItems) {
	if (parent.url === resolve('/(app)/settings') || parent.url === resolve('/(app)/system')) {
		parent.items?.sort((a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base' }));
	}
}

export function filterByPermissions(
	items: NavigationItem[] | undefined,
	user: User | null,
	accessManifest?: PermissionsManifest | null
): NavigationItem[] {
	if (!items) return [];
	if (!user) return [];
	const out: NavigationItem[] = [];
	for (const item of items) {
		if (!canSeeItem(item, user, accessManifest)) continue;
		if (item.items && item.items.length > 0) {
			const filteredChildren = filterByPermissions(item.items, user, accessManifest);
			// Drop a parent group only when none of its children survived the
			// filter and it has no access surface of its own to link to.
			if (filteredChildren.length === 0 && !item.accessSurfaceId) continue;
			out.push({ ...item, items: filteredChildren });
		} else {
			out.push(item);
		}
	}
	return out;
}

function hasShownFeature(feature: FeatureID): boolean {
	return environmentStore.shown.some((env) => featureStore.isEnabled(feature, env.id));
}

function canSeeItem(item: NavigationItem, user: User, accessManifest: PermissionsManifest | null | undefined): boolean {
	if (item.requiredFeature && !hasShownFeature(item.requiredFeature)) return false;
	if (item.adminOnly && !user.isGlobalAdmin) return false;
	if (!item.accessSurfaceId) return true;
	if (!accessManifest?.accessSurfaces?.length) return true;
	return canReachShownAccessSurface(accessManifest, item.accessSurfaceId, user);
}

/** Returns the title of the navigation item whose URL is the longest prefix of `pathname`. */
export function getNavigationTitleForPath(pathname: string): string | undefined {
	let title: string | undefined;
	let bestLength = 0;
	const visit = (items: NavigationItem[]) => {
		for (const item of items) {
			const url = item.url.split('?')[0] ?? item.url;
			if (url.length > bestLength && (pathname === url || pathname.startsWith(`${url}/`))) {
				title = item.title;
				bestLength = url.length;
			}
			if (item.items) visit(item.items);
		}
	};
	visit(Object.values(navigationItems).flat());
	return title;
}

export function getSettingsSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.adminItems.find((item) => item.url === resolve('/(app)/settings'));
	return entry?.items?.map((item) => item.url) ?? [];
}

export function getCustomizeSubpageUrlsInNavOrder(): string[] {
	return Object.values(navigationItems)
		.flat()
		.flatMap((item) => item.items ?? [])
		.map((item) => item.url)
		.filter((url) => url.startsWith('/customize/'));
}

const defaultMobilePinnedItems: NavigationItem[] = [
	...navigationItems.overviewItems,
	...navigationItems.deployItems,
	...navigationItems.resourceItems
].filter((item) =>
	[resolve('/(app)/dashboard'), resolve('/(app)/projects'), resolve('/(app)/containers'), resolve('/(app)/images')].includes(
		item.url
	)
);

export function getSwarmNavigationItems(swarmEnabled: boolean | undefined): NavigationItem[] {
	if (swarmEnabled) {
		return navigationItems.swarmItems;
	}
	if (swarmEnabled === false && !environmentStore.shown.some((env) => featureStore.isSwarmEnabled(env.id, false))) {
		return [];
	}

	return navigationItems.swarmItems.filter((item) => item.url === resolve('/(app)/swarm/cluster'));
}

export function getPodmanNavigationItems(statuses: Record<string, PodmanStatus>): NavigationItem[] {
	const shown = environmentStore.shown.flatMap((env) => statuses[env.id] ?? []);
	if (shown.length === 0) return [];
	return navigationItems.podmanItems.filter(
		(item) => item.url !== resolve('/(app)/podman/quadlets') || shown.some((status) => status.quadletsSupported)
	);
}

export const DEFAULT_LANDING_PAGE = resolve('/(app)/dashboard');

export function getLandingPageNavItems(): NavigationItem[] {
	return [
		...navigationItems.overviewItems,
		...navigationItems.deployItems,
		...navigationItems.resourceItems,
		...navigationItems.adminItems.filter((item) => !item.items)
	];
}

export type MobileNavigationSettings = {
	pinnedItems: string[];
	mode: 'floating' | 'docked';
	showLabels: boolean;
	scrollToHide: boolean;
};

export function getAvailableMobileNavItems(options?: {
	swarmEnabled?: boolean;
	podmanStatuses?: Record<string, PodmanStatus>;
	user?: User | null;
	accessManifest?: PermissionsManifest | null;
}): NavigationItem[] {
	const flatItems: NavigationItem[] = [];
	flatItems.push(...navigationItems.overviewItems, ...navigationItems.deployItems, ...navigationItems.resourceItems);

	const swarmItems = getSwarmNavigationItems(options?.swarmEnabled);
	if (swarmItems.length > 0) {
		flatItems.push(...swarmItems);
	}

	flatItems.push(...getPodmanNavigationItems(options?.podmanStatuses ?? {}));

	flatItems.push(...navigationItems.adminItems);

	if (options?.user === null) return [];
	if (!options?.user) return flatItems;
	return filterByPermissions(flatItems, options.user, options.accessManifest);
}

export const defaultMobileNavigationSettings: MobileNavigationSettings = {
	pinnedItems: defaultMobilePinnedItems.map((item) => item.url),
	mode: 'floating',
	showLabels: true,
	scrollToHide: true
};

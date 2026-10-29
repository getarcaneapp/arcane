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
	FileTextIcon
} from '#lib/icons/index.js';
import { m } from '#lib/paraglide/messages.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import { featureStore } from '#lib/stores/features.store.svelte.js';
import type { PermissionsManifest, User } from '#lib/types/auth.js';
import type { FeatureID } from '#lib/types/features.js';
import type { PodmanStatus } from '#lib/types/podman.js';
import { canReachShownAccessSurface } from '#lib/utils/auth.js';
import { stripBase } from '#lib/utils/base-path.js';
import type { ShortcutKey } from '#lib/utils/navigation.js';

export type NavigationItem = {
	title: string;
	url: string;
	icon: any;
	shortcut?: ShortcutKey[];
	items?: NavigationItem[];
	/**
	 * Backend-owned access-surface ID that gates this item. Omit to make the
	 * item visible to every authenticated user.
	 */
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
			url: '/dashboard',
			icon: DashboardIcon,
			shortcut: ['mod', '1'],
			accessSurfaceId: 'route.dashboard'
		},
		{
			title: m.updates(),
			url: '/updates',
			icon: UpdateIcon,
			shortcut: ['mod', 'u'],
			accessSurfaceId: 'route.updates'
		},
		{
			title: m.security(),
			url: '/security',
			requiredFeature: 'vulnerabilityManagement',
			icon: ShieldAlertIcon,
			accessSurfaceId: 'route.images.vulnerabilities'
		},
		{
			title: m.events_title(),
			url: '/events',
			icon: EventsIcon,
			shortcut: ['mod', '9'],
			accessSurfaceId: 'route.events'
		}
	],
	deployItems: [
		{
			title: m.projects_title(),
			url: '/projects',
			icon: ProjectsIcon,
			shortcut: ['mod', '2'],
			accessSurfaceId: 'route.projects',
			items: [
				{
					title: m.templates_title(),
					url: '/customize/templates',
					icon: TemplateIcon,
					accessSurfaceId: 'customize.category.templates'
				},
				{
					title: m.variables_title(),
					url: '/customize/variables',
					icon: VariableIcon,
					accessSurfaceId: 'customize.category.variables'
				}
			]
		},
		{
			title: m.git_syncs_title(),
			url: '/gitops',
			icon: GitBranchIcon,
			shortcut: ['mod', 'g'],
			accessSurfaceId: 'route.gitops',
			items: [
				{
					title: m.git_repositories_title(),
					url: '/customize/git-repositories',
					icon: GitBranchIcon,
					accessSurfaceId: 'customize.category.git-repositories'
				}
			]
		}
	],
	resourceItems: [
		{
			title: m.containers(),
			url: '/containers',
			icon: ContainersIcon,
			shortcut: ['mod', '5'],
			accessSurfaceId: 'route.containers'
		},
		{
			title: m.images(),
			url: '/images',
			icon: ImagesIcon,
			shortcut: ['mod', '6'],
			accessSurfaceId: 'route.images',
			items: [
				{ title: m.builds(), url: '/images/builds', icon: HammerIcon, accessSurfaceId: 'route.images.builds' },
				{
					title: m.registries_title(),
					url: '/customize/registries',
					icon: RegistryIcon,
					accessSurfaceId: 'customize.category.registries'
				}
			]
		},
		{
			title: m.resource_networks_cap(),
			url: '/networks',
			icon: NetworksIcon,
			shortcut: ['mod', '7'],
			accessSurfaceId: 'route.networks',
			items: [
				{ title: m.common_ports(), url: '/networks/ports', icon: HashIcon, accessSurfaceId: 'route.ports' },
				{
					title: m.networks_topology_button(),
					url: '/networks/topology',
					icon: GitBranchIcon,
					accessSurfaceId: 'route.networks.topology'
				}
			]
		},
		{
			title: m.resource_volumes_cap(),
			url: '/volumes',
			icon: VolumesIcon,
			shortcut: ['mod', '8'],
			accessSurfaceId: 'route.volumes'
		}
	],
	swarmItems: [
		{ title: 'Services', url: '/swarm/services', icon: DockIcon, accessSurfaceId: 'route.swarm.services' },
		{ title: 'Nodes', url: '/swarm/nodes', icon: UsersIcon, accessSurfaceId: 'route.swarm.nodes' },
		{ title: 'Tasks', url: '/swarm/tasks', icon: JobsIcon, accessSurfaceId: 'route.swarm.tasks' },
		{ title: 'Stacks', url: '/swarm/stacks', icon: LayersIcon, accessSurfaceId: 'route.swarm.stacks' },
		{ title: 'Cluster', url: '/swarm/cluster', icon: SettingsIcon, accessSurfaceId: 'route.swarm.cluster' },
		{ title: 'Configs', url: '/swarm/configs', icon: TemplateIcon, accessSurfaceId: 'route.swarm.configs' },
		{ title: 'Secrets', url: '/swarm/secrets', icon: LockIcon, accessSurfaceId: 'route.swarm.secrets' }
	],
	podmanItems: [
		{ title: m.podman_pods_title(), url: '/podman/pods', icon: BoxIcon, accessSurfaceId: 'route.podman' },
		{ title: m.podman_quadlets_title(), url: '/podman/quadlets', icon: FileTextIcon, accessSurfaceId: 'route.podman.quadlets' },
		{ title: m.podman_kube_title(), url: '/podman/kube', icon: LayersIcon, accessSurfaceId: 'route.podman.kube' },
		{ title: m.podman_secrets_title(), url: '/podman/secrets', icon: LockIcon, accessSurfaceId: 'route.podman.secrets' },
		{
			title: m.auto_update(),
			url: '/podman/autoupdate',
			icon: UpdateIcon,
			accessSurfaceId: 'route.podman.autoupdate'
		}
	],
	adminItems: [
		{
			title: m.environments_title(),
			url: '/environments',
			icon: EnvironmentsIcon,
			shortcut: ['mod', '3'],
			accessSurfaceId: 'route.environments'
		},
		{
			title: m.settings(),
			url: '/settings',
			icon: SettingsIcon,
			shortcut: ['mod', '0'],
			accessSurfaceId: 'landing.settings',
			items: [
				{
					title: m.api_key_page_title(),
					url: '/settings/api-keys',
					icon: ApiKeyIcon,
					shortcut: ['mod', 'shift', '1'],
					accessSurfaceId: 'settings.category.apikeys'
				},
				{
					title: m.webhook_page_title(),
					url: '/settings/webhooks',
					icon: GlobeIcon,
					accessSurfaceId: 'settings.category.webhooks'
				},
				{
					title: m.authentication(),
					url: '/settings/authentication',
					icon: LockIcon,
					shortcut: ['mod', 'shift', '3'],
					accessSurfaceId: 'settings.category.authentication'
				},
				{
					title: m.notifications_title(),
					url: '/settings/notifications',
					icon: NotificationsIcon,
					shortcut: ['mod', 'shift', '4'],
					accessSurfaceId: 'settings.category.notifications'
				},
				{
					title: m.system_backups_title(),
					url: '/settings/backups',
					icon: BackupIcon,
					accessSurfaceId: 'settings.category.systembackups',
					adminOnly: true
				},
				{
					title: m.users_title(),
					url: '/settings/users',
					icon: UsersIcon,
					shortcut: ['mod', 'shift', '8'],
					accessSurfaceId: 'settings.category.users'
				},
				{
					title: m.roles_title(),
					url: '/settings/roles',
					icon: ShieldAlertIcon,
					accessSurfaceId: 'settings.category.roles'
				},
				{
					title: m.diagnostics(),
					url: '/settings/diagnostics',
					icon: ActivityIcon,
					accessSurfaceId: 'settings.category.diagnostics'
				}
			]
		}
	]
};

// Keep the settings sub-navigation alphabetical regardless of the order
// entries are declared in the literal above. The sidebar and mobile nav render it;
// the settings landing page uses it for card order and for Roles/Diagnostics cards.
{
	const settingsParent = navigationItems.adminItems.find((item) => item.url === '/settings');
	if (settingsParent?.items) {
		settingsParent.items.sort((a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: 'base' }));
	}
}

// ---------- Permission-based filtering ----------

/**
 * Filter a navigation tree to entries the user can reach. Empty parent groups
 * (i.e. those whose children were all filtered out and which themselves have
 * no access surface) are preserved; empty parent groups whose own access
 * surface check fails are removed.
 *
 * @param items navigation items to filter
 * @param user the current user (or null when unauthenticated)
 */
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
export function getNavigationTitleForPath(rawPathname: string): string | undefined {
	const pathname = stripBase(rawPathname);
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
	const entry = navigationItems.adminItems.find((item) => item.url === '/settings');
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
].filter((item) => ['/dashboard', '/projects', '/containers', '/images'].includes(item.url));

export function getSwarmNavigationItems(swarmEnabled: boolean | undefined): NavigationItem[] {
	if (swarmEnabled) {
		return navigationItems.swarmItems;
	}
	if (swarmEnabled === false && !environmentStore.shown.some((env) => featureStore.isSwarmEnabled(env.id, false))) {
		return [];
	}

	return navigationItems.swarmItems.filter((item) => item.url === '/swarm/cluster');
}

export function getPodmanNavigationItems(statuses: Record<string, PodmanStatus>): NavigationItem[] {
	const shown = environmentStore.shown.flatMap((env) => statuses[env.id] ?? []);
	if (shown.length === 0) return [];
	return navigationItems.podmanItems.filter(
		(item) => item.url !== '/podman/quadlets' || shown.some((status) => status.quadletsSupported)
	);
}

export const DEFAULT_LANDING_PAGE = '/dashboard';

/**
 * Top-level pages selectable as the post-login landing page. Derived from the
 * sidebar sections so the option list stays in sync with the nav. /settings
 * is not selectable (category landing, not a resource page).
 */
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

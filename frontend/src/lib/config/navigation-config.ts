import {
	ApiKeyIcon,
	AppearanceIcon,
	UsersIcon,
	LockIcon,
	NotificationsIcon,
	DashboardIcon,
	ProjectsIcon,
	EnvironmentsIcon,
	CustomizeIcon,
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
	ActivityIcon
} from '$lib/icons';
import { m } from '$lib/paraglide/messages';
import type { ShortcutKey } from '$lib/utils/navigation';
import type { PermissionsManifest, User } from '$lib/types/auth';
import { canReachAccessSurface } from '$lib/utils/access-policy';

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
};

export type NavigationSections = {
	managementItems: NavigationItem[];
	resourceItems: NavigationItem[];
	swarmItems: NavigationItem[];
	settingsItems: NavigationItem[];
};

export const navigationItems: NavigationSections = {
	managementItems: [
		{
			title: m.dashboard_title(),
			url: '/dashboard',
			icon: DashboardIcon,
			shortcut: ['mod', '1'],
			accessSurfaceId: 'route.dashboard'
		},
		{
			title: m.projects_title(),
			url: '/projects',
			icon: ProjectsIcon,
			shortcut: ['mod', '2'],
			accessSurfaceId: 'route.projects'
		},
		{
			title: m.environments_title(),
			url: '/environments',
			icon: EnvironmentsIcon,
			shortcut: ['mod', '3'],
			accessSurfaceId: 'route.environments'
		},
		{
			title: m.customize_title(),
			url: '/customize',
			icon: CustomizeIcon,
			shortcut: ['mod', '4'],
			accessSurfaceId: 'landing.customize',
			items: [
				{
					title: m.templates_title(),
					url: '/customize/templates',
					icon: TemplateIcon,
					accessSurfaceId: 'customize.category.templates'
				},
				{
					title: m.registries_title(),
					url: '/customize/registries',
					icon: RegistryIcon,
					accessSurfaceId: 'customize.category.registries'
				},
				{
					title: m.variables_title(),
					url: '/customize/variables',
					icon: VariableIcon,
					accessSurfaceId: 'customize.category.variables'
				},
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
			title: m.containers_title(),
			url: '/containers',
			icon: ContainersIcon,
			shortcut: ['mod', '5'],
			accessSurfaceId: 'route.containers'
		},
		{
			title: m.images_title(),
			url: '/images',
			icon: ImagesIcon,
			shortcut: ['mod', '6'],
			accessSurfaceId: 'route.images',
			items: [
				{ title: m.builds(), url: '/images/builds', icon: HammerIcon, accessSurfaceId: 'route.images.builds' },
				{
					title: m.vuln_title(),
					url: '/images/vulnerabilities',
					icon: ShieldAlertIcon,
					accessSurfaceId: 'route.images.vulnerabilities'
				}
			]
		},
		{
			title: m.images_updates(),
			url: '/updates',
			icon: UpdateIcon,
			shortcut: ['mod', 'u'],
			accessSurfaceId: 'route.updates'
		},
		{
			title: m.networks_title(),
			url: '/networks',
			icon: NetworksIcon,
			shortcut: ['mod', '7'],
			accessSurfaceId: 'route.networks',
			items: [
				{ title: m.ports_title(), url: '/ports', icon: HashIcon, accessSurfaceId: 'route.ports' },
				{
					title: m.networks_topology_button(),
					url: '/networks/topology',
					icon: GitBranchIcon,
					accessSurfaceId: 'route.networks.topology'
				}
			]
		},
		{
			title: m.volumes_title(),
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
	settingsItems: [
		{
			title: m.events_title(),
			url: '/events',
			icon: EventsIcon,
			shortcut: ['mod', '9'],
			accessSurfaceId: 'route.events'
		},
		{
			title: m.settings_title(),
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
					title: m.appearance_title(),
					url: '/settings/appearance',
					icon: AppearanceIcon,
					shortcut: ['mod', 'shift', '2'],
					accessSurfaceId: 'settings.category.appearance'
				},
				{
					title: m.webhook_page_title(),
					url: '/settings/webhooks',
					icon: GlobeIcon,
					accessSurfaceId: 'settings.category.webhooks'
				},
				{
					title: m.authentication_title(),
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
					title: m.activity_settings_title(),
					url: '/settings/activity',
					icon: ActivityIcon,
					accessSurfaceId: 'settings.category.activity'
				},
				{
					title: m.builds(),
					url: '/settings/builds',
					icon: HammerIcon,
					shortcut: ['mod', 'shift', '6'],
					accessSurfaceId: 'settings.category.build'
				},
				{
					title: m.timeouts_settings(),
					url: '/settings/timeouts',
					icon: JobsIcon,
					shortcut: ['mod', 'shift', '7'],
					accessSurfaceId: 'settings.category.timeouts'
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
					title: m.diagnostics_title(),
					url: '/settings/diagnostics',
					icon: ActivityIcon,
					accessSurfaceId: 'settings.category.diagnostics'
				}
			]
		}
	]
};

// Keep the settings sub-navigation alphabetical regardless of the order
// entries are declared in the literal above. Sidebar, mobile nav, and the
// settings landing page all read from navigationItems.settingsItems, so a
// single sort here propagates everywhere.
{
	const settingsParent = navigationItems.settingsItems.find((item) => item.url === '/settings');
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
 * @param currentEnvId the currently-selected env, for env-scoped checks
 */
export function filterByPermissions(
	items: NavigationItem[] | undefined,
	user: User | null,
	currentEnvId: string | undefined,
	accessManifest?: PermissionsManifest | null
): NavigationItem[] {
	if (!items) return [];
	if (!user) return [];
	const out: NavigationItem[] = [];
	for (const item of items) {
		if (!canSeeItem(item, user, currentEnvId, accessManifest)) continue;
		if (item.items && item.items.length > 0) {
			const filteredChildren = filterByPermissions(item.items, user, currentEnvId, accessManifest);
			// Drop a parent group only when it has children declared but none
			// survived the filter. A parent with NO children declared (a leaf
			// link that happens to have an empty items array) stays.
			if (filteredChildren.length === 0 && item.items.length > 0) continue;
			out.push({ ...item, items: filteredChildren });
		} else {
			out.push(item);
		}
	}
	return out;
}

function canSeeItem(
	item: NavigationItem,
	user: User,
	currentEnvId: string | undefined,
	accessManifest: PermissionsManifest | null | undefined
): boolean {
	if (!item.accessSurfaceId) return true;
	if (!accessManifest?.accessSurfaces?.length) return true;
	return canReachAccessSurface(accessManifest, item.accessSurfaceId, user, currentEnvId);
}

export function getSettingsSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.settingsItems.find((item) => item.url === '/settings');
	return entry?.items?.map((item) => item.url) ?? [];
}

export function getCustomizeSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.managementItems.find((item) => item.url === '/customize');
	return entry?.items?.map((item) => item.url) ?? [];
}

const defaultMobilePinnedItems: NavigationItem[] = [
	navigationItems.managementItems[0]!,
	navigationItems.managementItems[1]!,
	navigationItems.resourceItems[0]!,
	navigationItems.resourceItems[1]!
];

export function getSwarmNavigationItems(swarmEnabled: boolean): NavigationItem[] {
	if (swarmEnabled) {
		return navigationItems.swarmItems;
	}

	return navigationItems.swarmItems.filter((item) => item.url === '/swarm/cluster');
}

export type MobileNavigationSettings = {
	pinnedItems: string[];
	mode: 'floating' | 'docked';
	showLabels: boolean;
	scrollToHide: boolean;
};

export function getAvailableMobileNavItems(options?: {
	swarmEnabled?: boolean;
	user?: User | null;
	currentEnvId?: string;
	accessManifest?: PermissionsManifest | null;
}): NavigationItem[] {
	const flatItems: NavigationItem[] = [];
	if (navigationItems.managementItems) {
		flatItems.push(...navigationItems.managementItems);
	}

	if (navigationItems.resourceItems) {
		flatItems.push(...navigationItems.resourceItems);
	}

	const swarmItems = getSwarmNavigationItems(!!options?.swarmEnabled);
	if (swarmItems.length > 0) {
		flatItems.push(...swarmItems);
	}

	if (navigationItems.settingsItems) {
		const settingsTopLevel = navigationItems.settingsItems.filter((item) => !item.items);
		flatItems.push(...settingsTopLevel);

		const settingsMain = navigationItems.settingsItems.find((item) => item.items);
		if (settingsMain) {
			flatItems.push(settingsMain);
		}
	}

	if (options?.user === null) return [];
	if (!options?.user) return flatItems;
	return filterByPermissions(flatItems, options.user, options.currentEnvId, options.accessManifest);
}

export const defaultMobileNavigationSettings: MobileNavigationSettings = {
	pinnedItems: defaultMobilePinnedItems.map((item) => item.url),
	mode: 'floating',
	showLabels: true,
	scrollToHide: true
};

export function getManagementItems(environmentId: string): NavigationItem[] {
	const gitSyncsItem: NavigationItem = {
		title: m.git_syncs_title(),
		url: `/environments/${environmentId}/gitops`,
		icon: GitBranchIcon,
		shortcut: ['mod', 'g'],
		accessSurfaceId: 'route.environments.gitops'
	};

	return navigationItems.managementItems.map((item) => {
		if (item.url !== '/environments') return item;
		return { ...item, items: [...(item.items ?? []), gitSyncsItem] };
	});
}

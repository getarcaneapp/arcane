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
	VariableIcon
} from '$lib/icons';
import { m } from '$lib/paraglide/messages';
import type { ShortcutKey } from '$lib/utils/navigation';
import type { User } from '$lib/types/auth';
import { GLOBAL_SCOPE, SUDO_PERMISSION } from '$lib/types/auth';

export type NavigationItem = {
	title: string;
	url: string;
	icon: any;
	shortcut?: ShortcutKey[];
	items?: NavigationItem[];
	/**
	 * Permission(s) the user must hold to see this item. ANY-of semantics: if
	 * the user has at least one of the listed permissions on the relevant
	 * scope, the item is visible. Omit to make the item visible to every
	 * authenticated user.
	 */
	requiredPermission?: string | string[];
	/**
	 * Scope for the permission check. `'global'` checks the global permission
	 * set; `'env'` (default for resource items) accepts permissions scoped to
	 * the currently-selected environment.
	 */
	scope?: 'global' | 'env';
};

export type RouteAccessRule = {
	prefix: string;
	perms: string[];
	scope: 'global' | 'env';
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
			scope: 'env',
			requiredPermission: 'dashboard:read'
		},
		{
			title: m.projects_title(),
			url: '/projects',
			icon: ProjectsIcon,
			shortcut: ['mod', '2'],
			scope: 'env',
			requiredPermission: ['projects:list', 'projects:read']
		},
		{
			title: m.environments_title(),
			url: '/environments',
			icon: EnvironmentsIcon,
			shortcut: ['mod', '3'],
			scope: 'global',
			requiredPermission: ['environments:list', 'environments:read']
		},
		{
			title: m.customize_title(),
			url: '/customize',
			icon: CustomizeIcon,
			shortcut: ['mod', '4'],
			scope: 'global',
			requiredPermission: 'customize:manage',
			items: [
				{
					title: m.templates_title(),
					url: '/customize/templates',
					icon: TemplateIcon,
					scope: 'global',
					requiredPermission: ['templates:list', 'templates:read']
				},
				{
					title: m.registries_title(),
					url: '/customize/registries',
					icon: RegistryIcon,
					scope: 'global',
					requiredPermission: ['registries:list', 'registries:read']
				},
				{
					title: m.variables_title(),
					url: '/customize/variables',
					icon: VariableIcon,
					scope: 'global',
					requiredPermission: ['templates:read']
				},
				{
					title: m.git_repositories_title(),
					url: '/customize/git-repositories',
					icon: GitBranchIcon,
					scope: 'global',
					requiredPermission: ['git-repositories:list', 'git-repositories:read']
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
			scope: 'env',
			requiredPermission: ['containers:list', 'containers:read']
		},
		{
			title: m.images_title(),
			url: '/images',
			icon: ImagesIcon,
			shortcut: ['mod', '6'],
			scope: 'env',
			requiredPermission: ['images:list', 'images:read'],
			items: [
				{ title: m.builds(), url: '/images/builds', icon: HammerIcon, scope: 'env', requiredPermission: 'images:build' },
				{
					title: m.vuln_title(),
					url: '/images/vulnerabilities',
					icon: ShieldAlertIcon,
					scope: 'env',
					requiredPermission: 'vulnerabilities:read'
				}
			]
		},
		{
			title: m.images_updates(),
			url: '/updates',
			icon: UpdateIcon,
			shortcut: ['mod', 'u'],
			scope: 'env',
			requiredPermission: 'image-updates:read'
		},
		{
			title: m.networks_title(),
			url: '/networks',
			icon: NetworksIcon,
			shortcut: ['mod', '7'],
			scope: 'env',
			requiredPermission: ['networks:list', 'networks:read'],
			items: [
				{ title: m.ports_title(), url: '/ports', icon: HashIcon, scope: 'env', requiredPermission: 'containers:list' },
				{
					title: m.networks_topology_button(),
					url: '/networks/topology',
					icon: GitBranchIcon,
					scope: 'env',
					requiredPermission: 'networks:read'
				}
			]
		},
		{
			title: m.volumes_title(),
			url: '/volumes',
			icon: VolumesIcon,
			shortcut: ['mod', '8'],
			scope: 'env',
			requiredPermission: ['volumes:list', 'volumes:read']
		}
	],
	swarmItems: [
		{ title: 'Services', url: '/swarm/services', icon: DockIcon, scope: 'env', requiredPermission: 'swarm:services' },
		{ title: 'Nodes', url: '/swarm/nodes', icon: UsersIcon, scope: 'env', requiredPermission: 'swarm:nodes' },
		{ title: 'Tasks', url: '/swarm/tasks', icon: JobsIcon, scope: 'env', requiredPermission: 'swarm:read' },
		{ title: 'Stacks', url: '/swarm/stacks', icon: LayersIcon, scope: 'env', requiredPermission: 'swarm:stacks' },
		{ title: 'Cluster', url: '/swarm/cluster', icon: SettingsIcon, scope: 'env', requiredPermission: 'swarm:read' },
		{ title: 'Configs', url: '/swarm/configs', icon: TemplateIcon, scope: 'env', requiredPermission: 'swarm:configs' },
		{ title: 'Secrets', url: '/swarm/secrets', icon: LockIcon, scope: 'env', requiredPermission: 'swarm:secrets' }
	],
	settingsItems: [
		{
			title: m.events_title(),
			url: '/events',
			icon: EventsIcon,
			shortcut: ['mod', '9'],
			scope: 'global',
			requiredPermission: 'events:read'
		},
		{
			title: m.settings_title(),
			url: '/settings',
			icon: SettingsIcon,
			shortcut: ['mod', '0'],
			scope: 'global',
			requiredPermission: 'settings:read',
			items: [
				{
					title: m.api_key_page_title(),
					url: '/settings/api-keys',
					icon: ApiKeyIcon,
					shortcut: ['mod', 'shift', '1'],
					scope: 'global',
					requiredPermission: 'apikeys:list'
				},
				{
					title: m.appearance_title(),
					url: '/settings/appearance',
					icon: AppearanceIcon,
					shortcut: ['mod', 'shift', '2'],
					scope: 'global',
					requiredPermission: 'settings:read'
				},
				{
					title: m.webhook_page_title(),
					url: '/settings/webhooks',
					icon: GlobeIcon,
					scope: 'env',
					requiredPermission: 'webhooks:list'
				},
				{
					title: m.authentication_title(),
					url: '/settings/authentication',
					icon: LockIcon,
					shortcut: ['mod', 'shift', '3'],
					scope: 'global',
					requiredPermission: 'settings:read'
				},
				{
					title: m.notifications_title(),
					url: '/settings/notifications',
					icon: NotificationsIcon,
					shortcut: ['mod', 'shift', '4'],
					scope: 'env',
					requiredPermission: 'notifications:manage'
				},
				{
					title: m.builds(),
					url: '/settings/builds',
					icon: HammerIcon,
					shortcut: ['mod', 'shift', '6'],
					scope: 'global',
					requiredPermission: 'settings:read'
				},
				{
					title: m.timeouts_settings(),
					url: '/settings/timeouts',
					icon: JobsIcon,
					shortcut: ['mod', 'shift', '7'],
					scope: 'global',
					requiredPermission: 'settings:read'
				},
				{
					title: m.users_title(),
					url: '/settings/users',
					icon: UsersIcon,
					shortcut: ['mod', 'shift', '8'],
					scope: 'global',
					requiredPermission: ['users:read', 'users:list']
				},
				{
					title: m.roles_title(),
					url: '/settings/roles',
					icon: ShieldAlertIcon,
					scope: 'global',
					requiredPermission: ['roles:read', 'roles:list']
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
 * no required permission) are preserved; empty parent groups whose own
 * permission check fails are removed.
 *
 * @param items navigation items to filter
 * @param user the current user (or null when unauthenticated)
 * @param currentEnvId the currently-selected env, for env-scoped checks
 */
export function filterByPermissions(
	items: NavigationItem[] | undefined,
	user: User | null,
	currentEnvId: string | undefined
): NavigationItem[] {
	if (!items) return [];
	if (!user) return [];
	const out: NavigationItem[] = [];
	for (const item of items) {
		if (!canSeeItem(item, user, currentEnvId)) continue;
		if (item.items && item.items.length > 0) {
			const filteredChildren = filterByPermissions(item.items, user, currentEnvId);
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

function canSeeItem(item: NavigationItem, user: User, currentEnvId: string | undefined): boolean {
	if (!item.requiredPermission) return true;
	const required = Array.isArray(item.requiredPermission) ? item.requiredPermission : [item.requiredPermission];
	const scope = item.scope ?? 'env';
	const set = effectivePermissions(user, scope === 'env' ? currentEnvId : undefined);
	if (set.has(SUDO_PERMISSION)) return true;
	return required.some((p) => set.has(p));
}

function effectivePermissions(user: User, envId: string | undefined): Set<string> {
	const out = new Set<string>();
	const global = user.permissionsByEnv?.[GLOBAL_SCOPE];
	if (global) for (const p of global) out.add(p);
	if (envId && envId !== GLOBAL_SCOPE) {
		const env = user.permissionsByEnv?.[envId];
		if (env) for (const p of env) out.add(p);
	}
	return out;
}

export function getSettingsSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.settingsItems.find((item) => item.url === '/settings');
	return entry?.items?.map((item) => item.url) ?? [];
}

export function getCustomizeSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.managementItems.find((item) => item.url === '/customize');
	return entry?.items?.map((item) => item.url) ?? [];
}

function routeAccessRulesForItems(items: NavigationItem[], out: RouteAccessRule[]): void {
	for (const item of items) {
		if (item.requiredPermission) {
			out.push({
				prefix: item.url,
				perms: Array.isArray(item.requiredPermission) ? item.requiredPermission : [item.requiredPermission],
				scope: item.scope ?? 'env'
			});
		}
		if (item.items) {
			routeAccessRulesForItems(item.items, out);
		}
	}
}

export function getRouteAccessRules(): RouteAccessRule[] {
	const out: RouteAccessRule[] = [];
	routeAccessRulesForItems(navigationItems.managementItems, out);
	routeAccessRulesForItems(navigationItems.resourceItems, out);
	out.push({ prefix: '/swarm', perms: ['swarm:read'], scope: 'env' });
	routeAccessRulesForItems(navigationItems.swarmItems, out);
	routeAccessRulesForItems(navigationItems.settingsItems, out);
	return out;
}

export function getRouteFallbackRules(): RouteAccessRule[] {
	return getRouteAccessRules().filter((rule) => {
		return (
			rule.prefix === '/dashboard' ||
			rule.prefix === '/containers' ||
			rule.prefix === '/projects' ||
			rule.prefix === '/images' ||
			rule.prefix === '/volumes' ||
			rule.prefix === '/networks' ||
			rule.prefix === '/swarm/services' ||
			rule.prefix === '/swarm/stacks' ||
			rule.prefix === '/swarm/cluster' ||
			rule.prefix === '/events' ||
			rule.prefix === '/settings'
		);
	});
}

export const defaultMobilePinnedItems: NavigationItem[] = [
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

export function getAvailableMobileNavItems(options?: { swarmEnabled?: boolean }): NavigationItem[] {
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

	return flatItems;
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
		shortcut: ['mod', 'g']
	};

	return navigationItems.managementItems.map((item) => {
		if (item.url !== '/environments') return item;
		return { ...item, items: [...(item.items ?? []), gitSyncsItem] };
	});
}

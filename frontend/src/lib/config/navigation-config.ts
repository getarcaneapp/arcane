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
	VariableIcon
} from '$lib/icons';
import { m } from '$lib/paraglide/messages';
import type { ShortcutKey } from '$lib/utils/keyboard-shortcut.utils';

export type NavigationItem = {
	title: string;
	url: string;
	icon: any;
	shortcut?: ShortcutKey[];
	items?: NavigationItem[];
};

export type NavigationSections = {
	managementItems: NavigationItem[];
	resourceItems: NavigationItem[];
	swarmItems: NavigationItem[];
	settingsItems: NavigationItem[];
};

export const navigationItems: NavigationSections = {
	managementItems: [
		{ title: m.dashboard_title(), url: '/dashboard', icon: DashboardIcon, shortcut: ['mod', '1'] },
		{ title: m.projects_title(), url: '/projects', icon: ProjectsIcon, shortcut: ['mod', '2'] },
		{ title: m.environments_title(), url: '/environments', icon: EnvironmentsIcon, shortcut: ['mod', '3'] },
		{
			title: m.customize_title(),
			url: '/customize',
			icon: CustomizeIcon,
			shortcut: ['mod', '4'],
			items: [
				{ title: m.templates_title(), url: '/customize/templates', icon: TemplateIcon },
				{ title: m.registries_title(), url: '/customize/registries', icon: RegistryIcon },
				{ title: m.variables_title(), url: '/customize/variables', icon: VariableIcon },
				{ title: m.git_repositories_title(), url: '/customize/git-repositories', icon: GitBranchIcon }
			]
		}
	],
	resourceItems: [
		{ title: m.containers_title(), url: '/containers', icon: ContainersIcon, shortcut: ['mod', '5'] },
		{
			title: m.images_title(),
			url: '/images',
			icon: ImagesIcon,
			shortcut: ['mod', '6'],
			items: [
				{ title: m.builds(), url: '/images/builds', icon: HammerIcon },
				{ title: m.vuln_title(), url: '/images/vulnerabilities', icon: ShieldAlertIcon }
			]
		},
		{
			title: m.networks_title(),
			url: '/networks',
			icon: NetworksIcon,
			shortcut: ['mod', '7'],
			items: [
				{ title: m.ports_title(), url: '/ports', icon: HashIcon },
				{ title: m.networks_topology_button(), url: '/networks/topology', icon: GitBranchIcon }
			]
		},
		{ title: m.volumes_title(), url: '/volumes', icon: VolumesIcon, shortcut: ['mod', '8'] }
	],
	swarmItems: [
		{ title: 'Services', url: '/swarm/services', icon: DockIcon },
		{ title: 'Nodes', url: '/swarm/nodes', icon: UsersIcon },
		{ title: 'Tasks', url: '/swarm/tasks', icon: JobsIcon },
		{ title: 'Stacks', url: '/swarm/stacks', icon: LayersIcon },
		{ title: 'Cluster', url: '/swarm/cluster', icon: SettingsIcon },
		{ title: 'Configs', url: '/swarm/configs', icon: TemplateIcon },
		{ title: 'Secrets', url: '/swarm/secrets', icon: LockIcon }
	],
	settingsItems: [
		{
			title: m.events_title(),
			url: '/events',
			icon: EventsIcon,
			shortcut: ['mod', '9']
		},
		{
			title: m.settings_title(),
			url: '/settings',
			icon: SettingsIcon,
			shortcut: ['mod', '0'],
			items: [
				{ title: m.api_key_page_title(), url: '/settings/api-keys', icon: ApiKeyIcon, shortcut: ['mod', 'shift', '1'] },
				{ title: m.appearance_title(), url: '/settings/appearance', icon: AppearanceIcon, shortcut: ['mod', 'shift', '2'] },
				{
					title: m.authentication_title(),
					url: '/settings/authentication',
					icon: LockIcon,
					shortcut: ['mod', 'shift', '3']
				},
				{
					title: m.environments_title(),
					url: '/settings/environments',
					icon: EnvironmentsIcon,
					shortcut: ['mod', 'shift', '5']
				},
				{
					title: m.notifications_title(),
					url: '/settings/notifications',
					icon: NotificationsIcon,
					shortcut: ['mod', 'shift', '4']
				},
				{ title: m.builds(), url: '/settings/builds', icon: HammerIcon, shortcut: ['mod', 'shift', '6'] },
				{ title: m.timeouts_settings(), url: '/settings/timeouts', icon: JobsIcon, shortcut: ['mod', 'shift', '7'] },
				{ title: m.users_title(), url: '/settings/users', icon: UsersIcon, shortcut: ['mod', 'shift', '8'] }
			]
		}
	]
};

export function getSettingsSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.settingsItems.find((item) => item.url === '/settings');
	return entry?.items?.map((item) => item.url) ?? [];
}

export function getCustomizeSubpageUrlsInNavOrder(): string[] {
	const entry = navigationItems.managementItems.find((item) => item.url === '/customize');
	return entry?.items?.map((item) => item.url) ?? [];
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

export function getGitOpsItems(environmentId: string): NavigationItem[] {
	return [
		{
			title: m.git_syncs_title(),
			url: `/environments/${environmentId}/gitops`,
			icon: GitBranchIcon,
			shortcut: ['mod', 'g']
		}
	];
}

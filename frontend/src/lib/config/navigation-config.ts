import {
	ApiKeyIcon,
	ApperanceIcon,
	JobsIcon,
	UsersIcon,
	SecurityIcon,
	NotificationsIcon,
	DashboardIcon,
	ProjectsIcon,
	EnvironmentsIcon,
	CustomizeIcon,
	ContainersIcon,
	ImagesIcon,
	NetworksIcon,
	VolumesIcon,
	EventsIcon,
	SettingsIcon,
	GitBranchIcon,
	ShieldAlertIcon,
	HammerIcon
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

export const navigationItems: Record<string, NavigationItem[]> = {
	managementItems: [
		{ title: m.dashboard_title(), url: '/dashboard', icon: DashboardIcon, shortcut: ['mod', '1'] },
		{ title: m.projects_title(), url: '/projects', icon: ProjectsIcon, shortcut: ['mod', '2'] },
		{ title: m.environments_title(), url: '/environments', icon: EnvironmentsIcon, shortcut: ['mod', '3'] },
		{ title: m.customize_title(), url: '/customize', icon: CustomizeIcon, shortcut: ['mod', '4'] }
	],
	resourceItems: [
		{ title: m.containers_title(), url: '/containers', icon: ContainersIcon, shortcut: ['mod', '5'] },
		{ title: m.images_title(), url: '/images', icon: ImagesIcon, shortcut: ['mod', '6'] },
		{ title: m.networks_title(), url: '/networks', icon: NetworksIcon, shortcut: ['mod', '7'] },
		{ title: m.volumes_title(), url: '/volumes', icon: VolumesIcon, shortcut: ['mod', '8'] }
	],
	deploymentItems: [{ title: m.builds(), url: '/images/builds', icon: HammerIcon, shortcut: ['mod', 'b'] }],
	securityItems: [{ title: m.vuln_title(), url: '/security', icon: ShieldAlertIcon, shortcut: ['mod', 's'] }],
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
				{ title: m.appearance_title(), url: '/settings/appearance', icon: ApperanceIcon, shortcut: ['mod', 'shift', '2'] },
				{
					title: m.notifications_title(),
					url: '/settings/notifications',
					icon: NotificationsIcon,
					shortcut: ['mod', 'shift', '3']
				},
				{ title: m.security_title(), url: '/settings/security', icon: SecurityIcon, shortcut: ['mod', 'shift', '4'] },
				{ title: m.builds(), url: '/settings/builds', icon: HammerIcon, shortcut: ['mod', 'shift', '5'] },
				{ title: m.timeouts_settings(), url: '/settings/timeouts', icon: JobsIcon, shortcut: ['mod', 'shift', '6'] },
				{ title: m.users_title(), url: '/settings/users', icon: UsersIcon, shortcut: ['mod', 'shift', '7'] }
			]
		}
	]
};

export const defaultMobilePinnedItems: NavigationItem[] = [
	navigationItems.managementItems[0],
	navigationItems.managementItems[1],
	navigationItems.resourceItems[0],
	navigationItems.resourceItems[1]
];

export type MobileNavigationSettings = {
	pinnedItems: string[];
	mode: 'floating' | 'docked';
	showLabels: boolean;
	scrollToHide: boolean;
};

export function getAvailableMobileNavItems(): NavigationItem[] {
	const flatItems: NavigationItem[] = [];

	if (navigationItems.managementItems) {
		flatItems.push(...navigationItems.managementItems);
	}

	if (navigationItems.resourceItems) {
		flatItems.push(...navigationItems.resourceItems);
	}

	if (navigationItems.deploymentItems) {
		flatItems.push(...navigationItems.deploymentItems);
	}

	if (navigationItems.securityItems) {
		flatItems.push(...navigationItems.securityItems);
	}

	if (navigationItems.settingsItems) {
		const settingsTopLevel = navigationItems.settingsItems.filter((item) => !item.items);
		flatItems.push(...settingsTopLevel);

		// Also add the main settings item itself if it has children, as it's a valid navigation target
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

export function getBuildAndDeploymentItems(environmentId: string): NavigationItem[] {
	return [
		...navigationItems.deploymentItems,
		{
			title: m.git_syncs_title(),
			url: `/environments/${environmentId}/gitops`,
			icon: GitBranchIcon,
			shortcut: ['mod', 'g']
		}
	];
}

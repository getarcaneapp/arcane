import {
	ApiKeyIcon,
	ApperanceIcon,
	DockerBrandIcon,
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
	JobsIcon,
	GitBranchIcon
} from '$lib/icons';
import { m } from '$lib/paraglide/messages';

export type NavigationItem = {
	title: string;
	url: string;
	icon: any;
	items?: NavigationItem[];
};

export const navigationItems: Record<string, NavigationItem[]> = {
	managementItems: [
		{ title: m.dashboard_title(), url: '/dashboard', icon: DashboardIcon },
		{ title: m.projects_title(), url: '/projects', icon: ProjectsIcon },
		{ title: m.environments_title(), url: '/environments', icon: EnvironmentsIcon },
		{ title: m.customize_title(), url: '/customize', icon: CustomizeIcon }
	],
	resourceItems: [
		{ title: m.containers_title(), url: '/containers', icon: ContainersIcon },
		{ title: m.images_title(), url: '/images', icon: ImagesIcon },
		{ title: m.networks_title(), url: '/networks', icon: NetworksIcon },
		{ title: m.volumes_title(), url: '/volumes', icon: VolumesIcon }
	],
	settingsItems: [
		{
			title: m.events_title(),
			url: '/events',
			icon: EventsIcon
		},
		{
			title: m.settings_title(),
			url: '/settings',
			icon: SettingsIcon,
			items: [
				{ title: m.api_key_page_title(), url: '/settings/api-keys', icon: ApiKeyIcon },
				{ title: m.appearance_title(), url: '/settings/appearance', icon: ApperanceIcon },
				{ title: m.jobs_title(), url: '/settings/jobs', icon: JobsIcon },
				{ title: m.notifications_title(), url: '/settings/notifications', icon: NotificationsIcon },
				{ title: m.security_title(), url: '/settings/security', icon: SecurityIcon },
				{ title: m.users_title(), url: '/settings/users', icon: UsersIcon }
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

export function getManagementItems(environmentId: string): NavigationItem[] {
	return [
		...navigationItems.managementItems,
		{
			title: m.git_syncs_title?.() ?? 'Git Sync',
			url: `/environments/${environmentId}/gitops`,
			icon: GitBranchIcon
		}
	];
}

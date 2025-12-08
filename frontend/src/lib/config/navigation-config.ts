import { KeyIcon, type Icon as IconType } from '@lucide/svelte';
import PaletteIcon from '@lucide/svelte/icons/palette';
import FileStackIcon from '@lucide/svelte/icons/file-stack';
import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
import HouseIcon from '@lucide/svelte/icons/home';
import NetworkIcon from '@lucide/svelte/icons/network';
import ContainerIcon from '@lucide/svelte/icons/container';
import ImageIcon from '@lucide/svelte/icons/image';
import SettingsIcon from '@lucide/svelte/icons/settings';
import DatabaseIcon from '@lucide/svelte/icons/database';
import UserIcon from '@lucide/svelte/icons/user';
import ShieldIcon from '@lucide/svelte/icons/shield';
import ComputerIcon from '@lucide/svelte/icons/computer';
import AlarmClockIcon from '@lucide/svelte/icons/alarm-clock';
import BellIcon from '@lucide/svelte/icons/bell';
import { m } from '$lib/paraglide/messages';

export type NavigationItem = {
	title: string;
	url: string;
	icon: typeof IconType;
	items?: NavigationItem[];
};

export const navigationItems: Record<string, NavigationItem[]> = {
	managementItems: [
		{ title: m.dashboard_title(), url: '/dashboard', icon: HouseIcon },
		{ title: m.projects_title(), url: '/projects', icon: FileStackIcon },
		{ title: m.environments_title(), url: '/environments', icon: ComputerIcon },
		{ title: m.customize_title(), url: '/customize', icon: PaletteIcon }
	],
	resourceItems: [
		{ title: m.containers_title(), url: '/containers', icon: ContainerIcon },
		{ title: m.projects_title(), url: '/projects', icon: FileStackIcon },
		{ title: m.images_title(), url: '/images', icon: ImageIcon },
		{ title: m.networks_title(), url: '/networks', icon: NetworkIcon },
		{ title: m.volumes_title(), url: '/volumes', icon: HardDriveIcon }
	],
	settingsItems: [
		{
			title: m.events_title(),
			url: '/events',
			icon: AlarmClockIcon
		},
		{
			title: m.settings_title(),
			url: '/settings',
			icon: SettingsIcon,
			items: [
				{ title: m.general_title(), url: '/settings/general', icon: SettingsIcon },
				{ title: m.appearance_title(), url: '/settings/appearance', icon: PaletteIcon },
				{ title: m.docker_title(), url: '/settings/docker', icon: DatabaseIcon },
				{ title: m.security_title(), url: '/settings/security', icon: ShieldIcon },
				{ title: m.users_title(), url: '/settings/users', icon: UserIcon },
				{ title: m.notifications_title(), url: '/settings/notifications', icon: BellIcon },
				{ title: m.api_key_page_title(), url: '/settings/api-keys', icon: KeyIcon }
			]
		}
	]
};

export const defaultMobilePinnedItems: NavigationItem[] = [
	navigationItems.managementItems[0],
	navigationItems.resourceItems[0],
	navigationItems.resourceItems[2],
	navigationItems.settingsItems[1]
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

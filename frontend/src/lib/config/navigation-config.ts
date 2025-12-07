import KeyIcon from 'phosphor-svelte/lib/Key';
import type { PhosphorIcon } from '$lib/types/icon.type';
import PaletteIcon from 'phosphor-svelte/lib/Palette';
import FileStackIcon from 'phosphor-svelte/lib/Files';
import HardDriveIcon from 'phosphor-svelte/lib/HardDrive';
import HouseIcon from 'phosphor-svelte/lib/House';
import NetworkIcon from 'phosphor-svelte/lib/Network';
import ContainerIcon from 'phosphor-svelte/lib/Package';
import ImageIcon from 'phosphor-svelte/lib/Image';
import SettingsIcon from 'phosphor-svelte/lib/Gear';
import DatabaseIcon from 'phosphor-svelte/lib/Database';
import LayoutTemplateIcon from 'phosphor-svelte/lib/Layout';
import UserIcon from 'phosphor-svelte/lib/User';
import ShieldIcon from 'phosphor-svelte/lib/Shield';
import ComputerIcon from 'phosphor-svelte/lib/Desktop';
import LockKeyholeIcon from 'phosphor-svelte/lib/LockKey';
import AlarmClockIcon from 'phosphor-svelte/lib/Clock';
import NavigationIcon from 'phosphor-svelte/lib/Compass';
import FileTextIcon from 'phosphor-svelte/lib/FileText';
import BellIcon from 'phosphor-svelte/lib/Bell';
import { m } from '$lib/paraglide/messages';

export type NavigationItem = {
	title: string;
	url: string;
	icon: PhosphorIcon;
	items?: NavigationItem[];
};

export const navigationItems: Record<string, NavigationItem[]> = {
	managementItems: [
		{ title: m.dashboard_title(), url: '/dashboard', icon: HouseIcon },
		{ title: m.containers_title(), url: '/containers', icon: ContainerIcon },
		{ title: m.projects_title(), url: '/projects', icon: FileStackIcon },
		{ title: m.images_title(), url: '/images', icon: ImageIcon },
		{ title: m.networks_title(), url: '/networks', icon: NetworkIcon },
		{ title: m.volumes_title(), url: '/volumes', icon: HardDriveIcon }
	],
	customizationItems: [
		{
			title: m.customize_title(),
			url: '/customize',
			icon: PaletteIcon,
			items: [
				{ title: m.templates_title(), url: '/customize/templates', icon: LayoutTemplateIcon },
				{ title: m.registries_title(), url: '/customize/registries', icon: LockKeyholeIcon },
				{ title: m.variables_title(), url: '/customize/variables', icon: FileTextIcon }
			]
		}
	],
	environmentItems: [
		{
			title: m.environments_title(),
			url: '/environments',
			icon: ComputerIcon
		}
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
				{ title: m.docker_title(), url: '/settings/docker', icon: DatabaseIcon },
				{ title: m.security_title(), url: '/settings/security', icon: ShieldIcon },
				{ title: m.navigation_title(), url: '/settings/navigation', icon: NavigationIcon },
				{ title: m.users_title(), url: '/settings/users', icon: UserIcon },
				{ title: m.notifications_title(), url: '/settings/notifications', icon: BellIcon },
				{ title: m.api_key_page_title(), url: '/settings/api-keys', icon: KeyIcon }
			]
		}
	]
};

export const defaultMobilePinnedItems: NavigationItem[] = [
	navigationItems.managementItems[0],
	navigationItems.managementItems[1],
	navigationItems.managementItems[3],
	navigationItems.managementItems[5]
];

export type MobileNavigationSettings = {
	pinnedItems: string[];
	mode: 'floating' | 'docked';
	showLabels: boolean;
	scrollToHide: boolean;
};

export function getAvailableMobileNavItems(): NavigationItem[] {
	const flatItems: NavigationItem[] = [];

	flatItems.push(...navigationItems.managementItems);
	for (const item of navigationItems.customizationItems) {
		if (item.items && item.items.length > 0) {
			flatItems.push(...item.items);
		} else {
			flatItems.push(item);
		}
	}

	if (navigationItems.environmentItems) {
		flatItems.push(...navigationItems.environmentItems);
	}
	if (navigationItems.settingsItems) {
		const settingsTopLevel = navigationItems.settingsItems.filter((item) => !item.items);
		flatItems.push(...settingsTopLevel);
	}

	return flatItems;
}

export const defaultMobileNavigationSettings: MobileNavigationSettings = {
	pinnedItems: defaultMobilePinnedItems.map((item) => item.url),
	mode: 'floating',
	showLabels: true,
	scrollToHide: true
};

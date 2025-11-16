/**
 * Navigation Commands
 * Commands for navigating between routes
 */

import type { Command } from '../types';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import { navigationItems } from '$lib/config/navigation-config';
import type { NavigationItem } from '$lib/config/navigation-config';
import NavigationIcon from '@lucide/svelte/icons/navigation';

/**
 * Flatten navigation items into a flat list
 */
function flattenNavigationItems(items: NavigationItem[]): NavigationItem[] {
	const flat: NavigationItem[] = [];

	for (const item of items) {
		flat.push(item);
		if (item.items) {
			flat.push(...flattenNavigationItems(item.items));
		}
	}

	return flat;
}

/**
 * Get all navigation routes
 */
function getAllRoutes(): NavigationItem[] {
	const allItems = [
		...navigationItems.managementItems,
		...navigationItems.customizationItems,
		...navigationItems.environmentItems,
		...navigationItems.settingsItems
	];

	return flattenNavigationItems(allItems);
}

/**
 * Create navigation commands as a single nested "Go to" command
 */
export function createNavigationCommands(): Command[] {
	const routes = getAllRoutes();

	// Create sub-commands for each route
	const subCommands: Command[] = routes.map((route) => ({
		id: `navigate-to-${route.url}`,
		label: route.title,
		description: route.url,
		icon: route.icon,
		keywords: [route.title.toLowerCase(), route.url],
		// Add keybinds for specific routes
		...(route.url === '/dashboard' && { keybind: 'Meta+D' }),
		...(route.url === '/containers' && { keybind: 'Meta+C' }),
		...(route.url === '/projects' && { keybind: 'Meta+P' }),
		...(route.url === '/images' && { keybind: 'Meta+I' }),
		...(route.url === '/networks' && { keybind: 'Meta+N' }),
		...(route.url === '/volumes' && { keybind: 'Meta+V' }),
		action: async () => {
			const currentPath = page.url.pathname;
			if (currentPath === route.url) {
				return;
			}
			await goto(route.url);
		}
	}));

	// Return a single "Go to" command with all routes as sub-commands
	return [
		{
			id: 'goto',
			label: 'Go to...',
			description: 'Navigate to a page',
			icon: NavigationIcon,
			category: 'Navigation',
			keywords: ['navigate', 'go', 'open', 'page', 'route'],
			keybind: 'Meta+Shift+G',
			subCommands: subCommands
		}
	];
}

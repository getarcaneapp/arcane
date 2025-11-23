import type { Command } from '../types';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import SettingsIcon from '@lucide/svelte/icons/settings';
import TrashIcon from '@lucide/svelte/icons/trash-2';
import { setMode } from 'mode-watcher';
import MonitorIcon from '@lucide/svelte/icons/monitor';
import MoonIcon from '@lucide/svelte/icons/moon';
import SunIcon from '@lucide/svelte/icons/sun';
import PaletteIcon from '@lucide/svelte/icons/palette';

/**
 * Creates system-related commands
 */
export function createSystemCommands(): Command[] {
	return [
		{
			id: 'system',
			label: 'System',
			description: 'System management actions',
			icon: SettingsIcon,
			keywords: ['system', 'docker', 'manage'],
			category: 'System',
			subCommands: [
				{
					id: 'system.prune',
					label: 'Prune System',
					description: 'Remove unused containers, networks, images, and volumes',
					icon: TrashIcon,
					keywords: ['prune', 'clean', 'cleanup', 'remove', 'unused', 'system'],
					action: async () => {
						const currentPath = page.url.pathname;
						if (currentPath !== '/dashboard') {
							await goto('/dashboard');
						}
						window.dispatchEvent(new CustomEvent('command:open-prune-dialog'));
					}
				}
			]
		},
		{
			id: 'theme',
			label: 'Change Theme',
			description: 'Switch between light, dark, and system theme',
			icon: PaletteIcon,
			keywords: ['theme', 'appearance', 'dark', 'light', 'mode'],
			category: 'System',
			subCommands: [
				{
					id: 'theme.light',
					label: 'Light Theme',
					description: 'Switch to light theme',
					icon: SunIcon,
					keywords: ['light', 'bright', 'day'],
					action: () => {
						setMode('light');
					}
				},
				{
					id: 'theme.dark',
					label: 'Dark Theme',
					description: 'Switch to dark theme',
					icon: MoonIcon,
					keywords: ['dark', 'night'],
					action: () => {
						setMode('dark');
					}
				},
				{
					id: 'theme.system',
					label: 'System Theme',
					description: 'Use system theme preference',
					icon: MonitorIcon,
					keywords: ['system', 'auto', 'automatic'],
					action: () => {
						setMode('system');
					}
				}
			]
		}
	];
}

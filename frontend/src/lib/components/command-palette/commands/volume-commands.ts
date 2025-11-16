import type { Command } from '../types';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
import PlusCircleIcon from '@lucide/svelte/icons/plus-circle';

/**
 * Creates volume-related commands
 */
export function createVolumeCommands(): Command[] {
	return [
		{
			id: 'volumes',
			label: 'Volumes',
			description: 'Volume management actions',
			icon: HardDriveIcon,
			keywords: ['volume', 'storage', 'docker'],
			category: 'Resources',
			subCommands: [
				{
					id: 'volumes.create',
					label: 'Create Volume',
					description: 'Create a new Docker volume',
					icon: PlusCircleIcon,
					keywords: ['create', 'add', 'new', 'volume', 'storage', 'docker'],
					keybind: 'Meta+Shift+V',
					action: async () => {
						const currentPath = page.url.pathname;
						if (currentPath !== '/volumes') {
							await goto('/volumes');
						}
						window.dispatchEvent(new CustomEvent('command:create-volume'));
					}
				}
			]
		}
	];
}

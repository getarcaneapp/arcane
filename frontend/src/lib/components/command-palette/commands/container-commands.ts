import type { Command } from '../types';
import { goto } from '$app/navigation';
import { get } from 'svelte/store';
import { page } from '$app/state';
import ContainerIcon from '@lucide/svelte/icons/container';

/**
 * Creates container-related commands
 */
export function createContainerCommands(): Command[] {
	return [
		{
			id: 'containers',
			label: 'Containers',
			description: 'Container management actions',
			icon: ContainerIcon,
			keywords: ['container', 'docker'],
			category: 'Resources',
			subCommands: [
				{
					id: 'containers.create',
					label: 'Create Container',
					description: 'Create a new container',
					icon: ContainerIcon,
					keywords: ['create', 'add', 'new', 'container', 'docker'],
					keybind: 'Meta+Shift+C',
					action: async () => {
						const currentPath = page.url.pathname;
						if (currentPath !== '/containers') {
							await goto('/containers');
						}
						window.dispatchEvent(new CustomEvent('command:create-container'));
					}
				}
			]
		}
	];
}

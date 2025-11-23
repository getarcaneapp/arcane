import type { Command } from '../types';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import NetworkIcon from '@lucide/svelte/icons/network';
import PlusCircleIcon from '@lucide/svelte/icons/plus-circle';

/**
 * Creates network-related commands
 */
export function createNetworkCommands(): Command[] {
	return [
		{
			id: 'networks',
			label: 'Networks',
			description: 'Network management actions',
			icon: NetworkIcon,
			keywords: ['network', 'docker'],
			category: 'Resources',
			subCommands: [
				{
					id: 'networks.create',
					label: 'Create Network',
					description: 'Create a new Docker network',
					icon: PlusCircleIcon,
					keywords: ['create', 'add', 'new', 'network', 'docker'],
					keybind: 'Meta+Shift+N',
					action: async () => {
						const currentPath = page.url.pathname;
						if (currentPath !== '/networks') {
							await goto('/networks');
						}
						window.dispatchEvent(new CustomEvent('command:create-network'));
					}
				}
			]
		}
	];
}

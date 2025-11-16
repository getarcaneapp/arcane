import type { Command } from '../types';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import ImageIcon from '@lucide/svelte/icons/image';
import ImagePlusIcon from '@lucide/svelte/icons/image-plus';

/**
 * Creates image-related commands
 */
export function createImageCommands(): Command[] {
	return [
		{
			id: 'images',
			label: 'Images',
			description: 'Image management actions',
			icon: ImageIcon,
			keywords: ['image', 'docker', 'pull'],
			category: 'Resources',
			subCommands: [
				{
					id: 'images.pull',
					label: 'Pull Image',
					description: 'Pull a Docker image',
					icon: ImagePlusIcon,
					keywords: ['pull', 'download', 'image', 'docker'],
					keybind: 'Meta+Shift+I',
					action: async () => {
						const currentPath = page.url.pathname;
						if (currentPath !== '/images') {
							await goto('/images');
						}
						window.dispatchEvent(new CustomEvent('command:pull-image'));
					}
				}
			]
		}
	];
}

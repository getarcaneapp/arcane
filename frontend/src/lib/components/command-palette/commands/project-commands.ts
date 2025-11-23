import type { Command } from '../types';
import { goto } from '$app/navigation';
import { get } from 'svelte/store';
import { page } from '$app/state';
import FolderPlusIcon from '@lucide/svelte/icons/folder-plus';
import FolderIcon from '@lucide/svelte/icons/folder';

/**
 * Creates project-related commands
 */
export function createProjectCommands(): Command[] {
	return [
		{
			id: 'projects',
			label: 'Projects',
			description: 'Project management actions',
			icon: FolderIcon,
			keywords: ['project', 'compose'],
			category: 'Resources',
			subCommands: [
				{
					id: 'projects.create',
					label: 'Create Project',
					description: 'Create a new project',
					icon: FolderPlusIcon,
					keywords: ['create', 'add', 'new', 'project'],
					keybind: 'Meta+Shift+P',
					action: async () => {
						const currentPath = page.url.pathname;
						if (currentPath !== '/projects/new') {
							await goto('/projects/new');
						}
					}
				}
			]
		}
	];
}

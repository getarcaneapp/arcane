/**
 * Help & Documentation Commands
 * Commands for accessing help resources and documentation
 */

import type { Command } from '../types';
import HelpCircleIcon from '@lucide/svelte/icons/help-circle';
// import KeyboardIcon from '@lucide/svelte/icons/keyboard';
import BookOpenIcon from '@lucide/svelte/icons/book-open';
import BugIcon from '@lucide/svelte/icons/bug';
import ExternalLinkIcon from '@lucide/svelte/icons/external-link';

const GITHUB_URL = 'https://github.com/ofkm/arcane';
const DOCUMENTATION_URL = 'https://getarcane.app/docs';

/**
 * Create help and documentation commands
 */
export function createHelpCommands(): Command[] {
	return [
		{
			id: 'help',
			label: 'Help & Documentation',
			description: 'Access help resources',
			icon: HelpCircleIcon,
			category: 'Help',
			keywords: ['help', 'docs', 'documentation', 'support', 'guide'],
			keybind: 'Meta+Shift+H',
			subCommands: [
				// {
				// 	id: 'view-keyboard-shortcuts',
				// 	label: 'View Keyboard Shortcuts',
				// 	description: 'See all available keyboard shortcuts',
				// 	icon: KeyboardIcon,
				// 	keywords: ['keyboard', 'shortcuts', 'hotkeys', 'keybinds'],
				// 	action: async () => {
				// 		// TODO: Implement keyboard shortcuts dialog
				// 	}
				// },
				{
					id: 'open-documentation',
					label: 'Open Documentation',
					description: 'View the official documentation',
					icon: BookOpenIcon,
					keywords: ['docs', 'documentation', 'manual', 'guide', 'help'],
					action: async () => {
						// Open documentation in new tab
						window.open(DOCUMENTATION_URL, '_blank');
					}
				},
				{
					id: 'report-issue',
					label: 'Report an Issue',
					description: 'Report a bug or request a feature',
					icon: BugIcon,
					keywords: ['bug', 'issue', 'report', 'feedback', 'feature'],
					action: async () => {
						// Open GitHub issues in new tab
						window.open(`${GITHUB_URL}/issues/new`, '_blank');
					}
				},
				{
					id: 'view-changelog',
					label: 'View Changelog',
					description: "See what's new in this version",
					icon: ExternalLinkIcon,
					keywords: ['changelog', 'updates', 'release', 'version', 'new'],
					action: async () => {
						// Navigate to changelog or open in new tab
						window.open(`${GITHUB_URL}/releases`, '_blank');
					}
				}
			]
		}
	];
}

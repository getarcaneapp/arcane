/**
 * Command Palette Store
 * Manages the state of the command palette
 */

import { commandRegistry } from './command-registry.svelte';

interface NavigationStackItem {
	commandId: string;
	selectedIndex: number;
}

class CommandPaletteStore {
	open = $state(false);
	searchQuery = $state('');
	selectedIndex = $state(0);
	navigationStack = $state<NavigationStackItem[]>([]);

	/**
	 * Open the command palette
	 */
	show(): void {
		this.open = true;
		this.searchQuery = '';
		this.selectedIndex = 0;
		this.navigationStack = [];
	}

	/**
	 * Close the command palette
	 */
	hide(): void {
		this.open = false;
		this.searchQuery = '';
		this.selectedIndex = 0;
		this.navigationStack = [];
	}

	/**
	 * Toggle the command palette
	 */
	toggle(): void {
		if (this.open) {
			this.hide();
		} else {
			this.show();
		}
	}

	/**
	 * Set the search query
	 */
	setSearchQuery(query: string): void {
		this.searchQuery = query;
		this.selectedIndex = 0; // Reset selection when search changes
	}

	/**
	 * Set the selected index
	 */
	setSelectedIndex(index: number): void {
		this.selectedIndex = index;
	}

	/**
	 * Move selection up
	 */
	moveUp(maxIndex: number): void {
		this.selectedIndex = this.selectedIndex > 0 ? this.selectedIndex - 1 : maxIndex;
	}

	/**
	 * Move selection down
	 */
	moveDown(maxIndex: number): void {
		this.selectedIndex = this.selectedIndex < maxIndex ? this.selectedIndex + 1 : 0;
	}

	/**
	 * Navigate into a command (show its sub-commands)
	 * Pushes current state onto stack and selects first sub-command
	 */
	navigateInto(commandId: string, currentSelectedIndex: number): void {
		// Push current state onto the stack
		this.navigationStack = [
			...this.navigationStack,
			{
				commandId,
				selectedIndex: currentSelectedIndex
			}
		];
		this.searchQuery = '';
		// Always select the first item in sub-commands
		this.selectedIndex = 0;
	}

	/**
	 * Navigate back to parent commands
	 * Pops from stack and restores the parent command selection
	 */
	navigateBack(): void {
		if (this.navigationStack.length > 0) {
			// Pop the last item from the stack (this is the command we're leaving)
			const poppedState = this.navigationStack[this.navigationStack.length - 1];
			this.navigationStack = this.navigationStack.slice(0, -1);
			this.searchQuery = '';
			// Restore the selected index to the parent command we came from
			this.selectedIndex = poppedState.selectedIndex;
		}
	}

	/**
	 * Check if we're in a nested view
	 */
	isNested(): boolean {
		return this.navigationStack.length > 0;
	}

	/**
	 * Get the current command ID (if nested)
	 */
	getCurrentCommandId(): string | null {
		if (this.navigationStack.length === 0) {
			return null;
		}
		return this.navigationStack[this.navigationStack.length - 1].commandId;
	}

	/**
	 * Open the command palette and navigate directly into a command's sub-commands
	 * Properly injects the navigation hierarchy
	 */
	showAndNavigateInto(commandId: string): void {
		// First, find the command in the registry to ensure it exists
		const command = commandRegistry.get(commandId);
		if (!command || !command.subCommands || command.subCommands.length === 0) {
			// If command doesn't exist or has no subcommands, just open normally
			this.show();
			return;
		}

		// Open the palette
		this.open = true;
		this.searchQuery = '';
		this.selectedIndex = 0;
		this.navigationStack = [];

		// Navigate into the command immediately - this sets up the navigation stack
		this.navigateInto(commandId, 0);
	}
}

// Export singleton instance
export const commandPaletteStore = new CommandPaletteStore();


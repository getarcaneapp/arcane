/**
 * Command Palette System Types
 */

export interface Command {
	/** Unique identifier for the command */
	id: string;
	/** Display name of the command */
	label: string;
	/** Optional description */
	description?: string;
	/** Optional icon component */
	icon?: any;
	/** Keywords for better search matching */
	keywords?: string[];
	/** Category for grouping commands */
	category?: string;
	/** Function to execute when command is triggered (not used if subCommands exist) */
	action?: () => void | Promise<void>;
	/** Optional sub-commands for nested navigation */
	subCommands?: Command[];
	/** Optional condition to determine if command should be shown */
	condition?: () => boolean;
	/** Optional keybind registered directly with the command */
	keybind?: string | { key: string; description?: string };
}

export interface Keybind {
	/** Unique identifier for the keybind */
	id: string;
	/** Command ID to execute */
	commandId: string;
	/** Key combination (e.g., 'Cmd+K', 'Ctrl+Shift+P') */
	key: string;
	/** Optional description override */
	description?: string;
	/** Optional condition to determine if keybind should be active */
	condition?: () => boolean;
}

export interface KeyboardEvent {
	key: string;
	ctrlKey: boolean;
	metaKey: boolean;
	shiftKey: boolean;
	altKey: boolean;
}

export interface CommandPaletteState {
	open: boolean;
	searchQuery: string;
	selectedIndex: number;
	/** Stack of command IDs for nested navigation */
	navigationStack: string[];
}


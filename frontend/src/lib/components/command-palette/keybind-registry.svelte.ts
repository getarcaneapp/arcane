/**
 * Keybind Registry
 * Manages keyboard shortcuts and their associated commands
 */

import type { Keybind } from './types';
import { commandRegistry } from './command-registry.svelte';

/**
 * Keybind part type for rendering
 */
export type KeybindPart =
	| { type: 'command' }
	| { type: 'control' }
	| { type: 'shift' }
	| { type: 'option' }
	| { type: 'text'; text: string };

class KeybindRegistry {
	private keybinds = $state<Map<string, Keybind>>(new Map());
	private isListening = $state(false);

	/**
	 * Register a new keybind
	 */
	register(keybind: Keybind): () => void {
		this.keybinds.set(keybind.id, keybind);

		// Return unregister function
		return () => {
			this.keybinds.delete(keybind.id);
		};
	}

	/**
	 * Register multiple keybinds at once
	 */
	registerMany(keybinds: Keybind[]): () => void {
		const unregisterFns = keybinds.map((kb) => this.register(kb));

		// Return function to unregister all
		return () => {
			unregisterFns.forEach((fn) => fn());
		};
	}

	/**
	 * Unregister a keybind by ID
	 */
	unregister(keybindId: string): void {
		this.keybinds.delete(keybindId);
	}

	/**
	 * Get all registered keybinds
	 */
	getAll(): Keybind[] {
		return Array.from(this.keybinds.values());
	}

	/**
	 * Get keybinds for a specific command
	 */
	getForCommand(commandId: string): Keybind[] {
		return this.getAll().filter((kb) => kb.commandId === commandId);
	}

	/**
	 * Normalize a key combination string
	 */
	private normalizeKey(key: string): string {
		return key.toLowerCase().replace(/\s+/g, '').replace('cmd', 'meta').replace('command', 'meta').replace('ctrl', 'meta');
	}

	/**
	 * Convert keyboard event to key string
	 */
	private eventToKeyString(event: KeyboardEvent): string {
		const parts: string[] = [];

		if (event.ctrlKey || event.metaKey) {
			parts.push('meta');
		}
		if (event.shiftKey) {
			parts.push('shift');
		}
		if (event.altKey) {
			parts.push('alt');
		}

		// Add the actual key
		const key = event.key.toLowerCase();
		if (key !== 'control' && key !== 'meta' && key !== 'shift' && key !== 'alt') {
			parts.push(key);
		}

		return parts.join('+');
	}

	/**
	 * Check if an event matches a keybind
	 */
	private matchesKeybind(event: KeyboardEvent, keybind: Keybind): boolean {
		const eventKey = this.eventToKeyString(event);
		const keybindKey = this.normalizeKey(keybind.key);

		return eventKey === keybindKey;
	}

	/**
	 * Handle keyboard event
	 */
	private handleKeyDown = async (event: KeyboardEvent): Promise<void> => {
		// Find matching keybind
		for (const keybind of this.keybinds.values()) {
			// Check condition
			if (keybind.condition && !keybind.condition()) {
				continue;
			}

			if (this.matchesKeybind(event, keybind)) {
				event.preventDefault();
				event.stopPropagation();

				// Execute the associated command
				await commandRegistry.execute(keybind.commandId);
				break;
			}
		}
	};

	/**
	 * Start listening for keyboard events
	 */
	startListening(): void {
		if (this.isListening) {
			return;
		}

		if (typeof window !== 'undefined') {
			window.addEventListener('keydown', this.handleKeyDown as any);
			this.isListening = true;
		}
	}

	/**
	 * Stop listening for keyboard events
	 */
	stopListening(): void {
		if (!this.isListening) {
			return;
		}

		if (typeof window !== 'undefined') {
			window.removeEventListener('keydown', this.handleKeyDown as any);
			this.isListening = false;
		}
	}

	/**
	 * Format a key combination for display
	 * Returns an array of parts that can be rendered with icons
	 */
	formatKey(key: string): KeybindPart[] {
		const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/.test(navigator.userAgent);

		const parts = key.split('+').map((part) => {
			const normalized = part.trim().toLowerCase();
			switch (normalized) {
				case 'meta':
				case 'cmd':
				case 'command':
					return isMac ? { type: 'command' as const } : { type: 'control' as const };
				case 'ctrl':
				case 'control':
					return { type: 'control' as const };
				case 'shift':
					return { type: 'shift' as const };
				case 'alt':
					return { type: 'option' as const };
				default:
					return { type: 'text' as const, text: part.charAt(0).toUpperCase() + part.slice(1) };
			}
		});

		return parts;
	}

	/**
	 * Clear all keybinds
	 */
	clear(): void {
		this.keybinds.clear();
	}
}

// Export singleton instance
export const keybindRegistry = new KeybindRegistry();

/**
 * Command Registry
 * Central registry for all commands in the application
 */

import type { Command } from './types';
import { keybindRegistry } from './keybind-registry.svelte';
import { commandPaletteStore } from './command-palette-store.svelte';

class CommandRegistry {
	private commands = $state<Map<string, Command>>(new Map());
	private keybindUnregisterFns = new Map<string, () => void>();

	/**
	 * Register a new command
	 */
	register(command: Command): () => void {
		this.commands.set(command.id, command);

		// Register keybind if provided
		if (command.keybind) {
			const keybindKey = typeof command.keybind === 'string' ? command.keybind : command.keybind.key;
			const keybindDescription = typeof command.keybind === 'string' ? undefined : command.keybind.description;

			const unregisterKeybind = keybindRegistry.register({
				id: `${command.id}-keybind`,
				commandId: command.id,
				key: keybindKey,
				description: keybindDescription || command.description
			});

			this.keybindUnregisterFns.set(command.id, unregisterKeybind);
		}

		// Recursively register keybinds for sub-commands
		if (command.subCommands) {
			for (const subCommand of command.subCommands) {
				if (subCommand.keybind) {
					const subKeybindKey = typeof subCommand.keybind === 'string' ? subCommand.keybind : subCommand.keybind.key;
					const subKeybindDescription = typeof subCommand.keybind === 'string' ? undefined : subCommand.keybind.description;

					const unregisterSubKeybind = keybindRegistry.register({
						id: `${subCommand.id}-keybind`,
						commandId: subCommand.id,
						key: subKeybindKey,
						description: subKeybindDescription || subCommand.description
					});

					this.keybindUnregisterFns.set(subCommand.id, unregisterSubKeybind);
				}
			}
		}

		// Return unregister function
		return () => {
			this.commands.delete(command.id);
			
			// Unregister keybinds
			const unregisterKeybind = this.keybindUnregisterFns.get(command.id);
			if (unregisterKeybind) {
				unregisterKeybind();
				this.keybindUnregisterFns.delete(command.id);
			}

			// Unregister sub-command keybinds
			if (command.subCommands) {
				for (const subCommand of command.subCommands) {
					const unregisterSubKeybind = this.keybindUnregisterFns.get(subCommand.id);
					if (unregisterSubKeybind) {
						unregisterSubKeybind();
						this.keybindUnregisterFns.delete(subCommand.id);
					}
				}
			}
		};
	}

	/**
	 * Register multiple commands at once
	 */
	registerMany(commands: Command[]): () => void {
		const unregisterFns = commands.map((cmd) => this.register(cmd));

		// Return function to unregister all
		return () => {
			unregisterFns.forEach((fn) => fn());
		};
	}

	/**
	 * Unregister a command by ID
	 */
	unregister(commandId: string): void {
		this.commands.delete(commandId);
	}

	/**
	 * Get a command by ID
	 */
	get(commandId: string): Command | undefined {
		return this.commands.get(commandId);
	}

	/**
	 * Get all registered commands
	 */
	getAll(): Command[] {
		return Array.from(this.commands.values()).filter((cmd) => {
			// Filter out commands that don't meet their condition
			if (cmd.condition && !cmd.condition()) {
				return false;
			}
			return true;
		});
	}

	/**
	 * Execute a command by ID
	 */
	async execute(commandId: string): Promise<void> {
		let command = this.commands.get(commandId);
		
		// If not found in top-level commands, search in sub-commands
		if (!command) {
			for (const parentCommand of this.commands.values()) {
				if (parentCommand.subCommands) {
					const subCommand = parentCommand.subCommands.find((cmd) => cmd.id === commandId);
					if (subCommand) {
						command = subCommand;
						break;
					}
				}
			}
		}

		if (!command) {
			console.warn(`Command not found: ${commandId}`);
			return;
		}

		// Check condition before executing
		if (command.condition && !command.condition()) {
			console.warn(`Command condition not met: ${commandId}`);
			return;
		}

		// If command has sub-commands, open the palette and navigate into them
		if (command.subCommands && command.subCommands.length > 0) {
			commandPaletteStore.showAndNavigateInto(commandId);
			return;
		}

		// Only execute action if it exists (commands with subCommands don't need actions)
		if (command.action) {
			await command.action();
		}
	}

	/**
	 * Get sub-commands for a command
	 */
	getSubCommands(commandId: string): Command[] {
		const command = this.commands.get(commandId);
		if (!command || !command.subCommands) {
			return [];
		}

		// Filter sub-commands by their conditions
		return command.subCommands.filter((cmd) => {
			if (cmd.condition && !cmd.condition()) {
				return false;
			}
			return true;
		});
	}

	/**
	 * Search commands by query with smart matching
	 * Prioritizes: starts-with > word boundaries > contains
	 */
	search(query: string): Command[] {
		const normalizedQuery = query.toLowerCase().trim();

		if (!normalizedQuery) {
			return this.getAll();
		}

		const results: { command: Command; score: number }[] = [];
		const topLevelCommands = this.getAll();

		// Smart matching function that returns a score (higher = better match)
		// Only matches at word boundaries - no substring matching
		const getMatchScore = (cmd: Command): number => {
			let score = 0;
			const label = cmd.label.toLowerCase();
			const description = cmd.description?.toLowerCase() || '';
			const keywords = cmd.keywords?.map((k) => k.toLowerCase()) || [];
			const category = cmd.category?.toLowerCase() || '';

			// Exact match (highest priority)
			if (label === normalizedQuery) return 1000;

			// Starts with query (very high priority)
			if (label.startsWith(normalizedQuery)) {
				score += 100;
			}

			// Word boundary match in label (high priority)
			// e.g., "ke" matches "Keyboard" or "View Keyboard" but NOT "Docker"
			const labelWords = label.split(/[\s-_/]+/);
			for (const word of labelWords) {
				if (word.startsWith(normalizedQuery)) {
					score += 50;
					break;
				}
			}

			// Description starts with (medium priority)
			if (description.startsWith(normalizedQuery)) {
				score += 15;
			}

			// Word boundary match in description
			const descWords = description.split(/[\s-_/]+/);
			for (const word of descWords) {
				if (word.startsWith(normalizedQuery)) {
					score += 12;
					break;
				}
			}

			// Keyword exact match (medium priority)
			if (keywords.some((k) => k === normalizedQuery)) {
				score += 15;
			}

			// Keyword starts with (lower priority)
			if (keywords.some((k) => k.startsWith(normalizedQuery))) {
				score += 10;
			}

			// Category match (lower priority)
			if (category.startsWith(normalizedQuery)) {
				score += 8;
			}

			return score;
		};

		// Search through all commands including sub-commands
		for (const cmd of topLevelCommands) {
			const score = getMatchScore(cmd);
			if (score > 0) {
				results.push({ command: cmd, score });
			}

			// Search through sub-commands
			if (cmd.subCommands) {
				for (const subCmd of cmd.subCommands) {
					const subScore = getMatchScore(subCmd);
					if (subScore > 0) {
						// Ensure sub-commands inherit parent category if they don't have one
						const subCommandWithCategory = {
							...subCmd,
							category: subCmd.category || cmd.category || 'General'
						};
						results.push({ command: subCommandWithCategory, score: subScore });
					}
				}
			}
		}

		// Sort by score (highest first) and return commands
		return results.sort((a, b) => b.score - a.score).map((r) => r.command);
	}

	/**
	 * Clear all commands
	 */
	clear(): void {
		this.commands.clear();
	}
}

// Export singleton instance
export const commandRegistry = new CommandRegistry();

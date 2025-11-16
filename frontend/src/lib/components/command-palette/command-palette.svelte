<script lang="ts">
	import { Dialog as DialogPrimitive } from 'bits-ui';
	import { commandRegistry } from './command-registry.svelte';
	import { commandPaletteStore } from './command-palette-store.svelte';
	import { keybindRegistry } from './keybind-registry.svelte';
	import { cn } from '$lib/utils';
	import SearchIcon from '@lucide/svelte/icons/search';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import CommandIcon from '@lucide/svelte/icons/command';
	import ShiftIcon from '@lucide/svelte/icons/arrow-big-up';
	import OptionIcon from '@lucide/svelte/icons/option';
	import type { Command } from './types';
	import type { KeybindPart } from './keybind-registry.svelte';
	import { tick } from 'svelte';

	let inputRef: HTMLInputElement | null = $state(null);
	let listRef: HTMLDivElement | null = $state(null);

	// Reactive state
	const open = $derived(commandPaletteStore.open);
	const searchQuery = $derived(commandPaletteStore.searchQuery);
	const selectedIndex = $derived(commandPaletteStore.selectedIndex);
	const isNested = $derived(commandPaletteStore.isNested());
	const currentCommandId = $derived(commandPaletteStore.getCurrentCommandId());
	let isKeyboardMode = $state(true);

	// Get current commands to display
	const currentCommands = $derived.by(() => {
		// Force reactivity by accessing open state
		open;

		if (currentCommandId) {
			// Show sub-commands of the current command
			const subCommands = commandRegistry.getSubCommands(currentCommandId);

			// Filter by search if there's a query
			if (searchQuery) {
				return subCommands.filter((cmd) => {
					const query = searchQuery.toLowerCase();
					return (
						cmd.label.toLowerCase().includes(query) ||
						cmd.description?.toLowerCase().includes(query) ||
						cmd.keywords?.some((k) => k.toLowerCase().includes(query))
					);
				});
			}

			return subCommands;
		} else {
			// Show top-level commands
			return commandRegistry.search(searchQuery);
		}
	});

	// Filtered commands grouped by category
	const filteredCommands = $derived.by(() => {
		const commands = currentCommands;

		// Group by category
		const grouped = new Map<string, Command[]>();

		commands.forEach((cmd) => {
			const category = cmd.category || 'General';
			if (!grouped.has(category)) {
				grouped.set(category, []);
			}
			grouped.get(category)!.push(cmd);
		});

		return grouped;
	});

	// Flat list of commands for keyboard navigation
	const flatCommands = $derived.by(() => {
		const commands: Command[] = [];
		filteredCommands.forEach((cmds) => {
			commands.push(...cmds);
		});
		return commands;
	});

	// Get the current parent command (for breadcrumb)
	const currentParentCommand = $derived.by(() => {
		if (currentCommandId) {
			return commandRegistry.get(currentCommandId);
		}
		return null;
	});

	// Auto-select first item when search results change and current selection is invalid
	// Only track flatCommands changes, not selectedIndex to avoid interference with navigation
	$effect(() => {
		// Only track flatCommands length changes
		const commandsLength = flatCommands.length;
		const currentIndex = commandPaletteStore.selectedIndex;

		// If there are commands and selected index is out of bounds, reset to 0
		if (commandsLength > 0 && currentIndex >= commandsLength) {
			commandPaletteStore.setSelectedIndex(0);
		}
		// If no commands, ensure index is 0
		else if (commandsLength === 0 && currentIndex !== 0) {
			commandPaletteStore.setSelectedIndex(0);
		}
	});

	// Handle keyboard navigation
	function handleKeyDown(event: KeyboardEvent) {
		const maxIndex = flatCommands.length - 1;

		// Enable keyboard mode on any keyboard interaction
		const isNavigationKey = ['ArrowDown', 'ArrowUp', 'ArrowRight', 'ArrowLeft', 'Enter', 'Escape', 'Backspace'].includes(
			event.key
		);
		if (isNavigationKey) {
			isKeyboardMode = true;
		}

		switch (event.key) {
			case 'ArrowDown':
				event.preventDefault();
				commandPaletteStore.moveDown(maxIndex);
				scrollToSelected();
				break;
			case 'ArrowUp':
				event.preventDefault();
				commandPaletteStore.moveUp(maxIndex);
				scrollToSelected();
				break;
			case 'ArrowRight':
				event.preventDefault();
				const selectedCommand = flatCommands[selectedIndex];
				if (selectedCommand && hasSubCommands(selectedCommand)) {
					commandPaletteStore.navigateInto(selectedCommand.id, selectedIndex);
				}
				break;
			case 'ArrowLeft':
				event.preventDefault();
				if (isNested) {
					commandPaletteStore.navigateBack();
				}
				break;
			case 'Enter':
				event.preventDefault();
				executeSelected();
				break;
			case 'Escape':
				event.preventDefault();
				if (isNested) {
					commandPaletteStore.navigateBack();
				} else {
					commandPaletteStore.hide();
				}
				break;
			case 'Backspace':
				// If search is empty and we're nested, go back
				if (!searchQuery && isNested) {
					event.preventDefault();
					commandPaletteStore.navigateBack();
				}
				break;
		}
	}

	// Handle mouse movement to detect when to switch to mouse mode
	function handlePointerMove() {
		// Switch to mouse mode on any pointer movement
		if (isKeyboardMode) {
			isKeyboardMode = false;
		}
	}

	// Handle mouse enter on command items
	function handleCommandMouseEnter(index: number) {
		// Only update selection if in mouse mode
		if (!isKeyboardMode) {
			commandPaletteStore.setSelectedIndex(index);
		}
	}

	// Reset to keyboard mode when dialog opens
	$effect(() => {
		if (open) {
			isKeyboardMode = true;
		}
	});

	// Scroll to selected item
	async function scrollToSelected() {
		await tick();
		const selectedElement = listRef?.querySelector('[data-selected="true"]');
		if (selectedElement) {
			selectedElement.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
		}
	}

	// Execute selected command
	async function executeSelected() {
		const command = flatCommands[selectedIndex];
		if (command) {
			if (command.subCommands && command.subCommands.length > 0) {
				// Navigate into sub-commands
				commandPaletteStore.navigateInto(command.id, selectedIndex);
			} else {
				// Execute the command directly (it might be a sub-command not in registry)
				if (command.action) {
					await command.action();
				} else {
					await commandRegistry.execute(command.id);
				}
				commandPaletteStore.hide();
			}
		}
	}

	// Execute command by clicking
	async function executeCommand(command: Command) {
		const commandIndex = getCommandIndex(command);
		if (command.subCommands && command.subCommands.length > 0) {
			// Navigate into sub-commands
			commandPaletteStore.navigateInto(command.id, commandIndex);
		} else {
			// Execute the command directly (it might be a sub-command not in registry)
			if (command.action) {
				await command.action();
			} else {
				await commandRegistry.execute(command.id);
			}
			commandPaletteStore.hide();
		}
	}

	// Focus input when dialog opens
	$effect(() => {
		if (open && inputRef) {
			setTimeout(() => {
				inputRef?.focus();
			}, 0);
		}
	});

	// Get keybind for command
	function getKeybindForCommand(commandId: string): KeybindPart[] | null {
		const keybinds = keybindRegistry.getForCommand(commandId);
		if (keybinds.length > 0) {
			return keybindRegistry.formatKey(keybinds[0].key);
		}
		return null;
	}

	// Get current index for a command
	function getCommandIndex(command: Command): number {
		return flatCommands.findIndex((cmd) => cmd.id === command.id);
	}

	// Check if command has sub-commands
	function hasSubCommands(command: Command): boolean {
		return !!command.subCommands && command.subCommands.length > 0;
	}
</script>

<DialogPrimitive.Root bind:open={commandPaletteStore.open}>
	<DialogPrimitive.Portal>
		<DialogPrimitive.Overlay
			class="data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 z-98"
			style="background: color-mix(in oklch, var(--background) 50%, transparent); backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);"
		/>

		<DialogPrimitive.Content
			class="data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 bubble bubble-shadow-lg fixed top-[15%] left-[50%] z-99 w-full max-w-2xl translate-x-[-50%] translate-y-[-15%] overflow-hidden duration-200"
		>
			<div
				class="flex flex-col"
				role="dialog"
				aria-label="Command palette"
				tabindex="-1"
				data-keyboard-mode={isKeyboardMode}
				onkeydown={handleKeyDown}
				onpointermove={handlePointerMove}
			>
				<!-- Breadcrumb / Back Button with glass effect -->
				{#if isNested && currentParentCommand}
					<div class="bg-muted/30 ring-border/50 flex items-center gap-2 px-4 py-2.5 text-sm ring-1 backdrop-blur-sm ring-inset">
						<button
							type="button"
							class="hover:bg-accent/60 hover:text-accent-foreground hover-lift flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 font-medium transition-all"
							onclick={() => commandPaletteStore.navigateBack()}
						>
							<ArrowLeftIcon class="size-3.5" />
							<span>Back</span>
						</button>
						<ChevronRightIcon class="size-4 opacity-40" />
						<div class="flex items-center gap-2">
							{#if currentParentCommand.icon}
								{@const Icon = currentParentCommand.icon}
								<Icon class="text-primary size-4" />
							{/if}
							<span class="text-foreground font-semibold">{currentParentCommand.label}</span>
						</div>
					</div>
				{/if}

				<!-- Search Input with enhanced styling -->
				<div
					class="from-background/50 to-muted/20 ring-border/50 flex items-center gap-3 bg-linear-to-br px-4 ring-1 backdrop-blur-sm ring-inset"
				>
					<SearchIcon class="text-primary/70 size-5 shrink-0" />
					<input
						bind:this={inputRef}
						type="text"
						placeholder={isNested ? 'Search options...' : 'Type a command or search...'}
						class="text-foreground placeholder:text-muted-foreground/60 flex h-16 w-full bg-transparent py-4 text-base font-medium outline-none disabled:cursor-not-allowed disabled:opacity-50"
						bind:value={commandPaletteStore.searchQuery}
					/>
				</div>

				<!-- Commands List with custom scrollbar -->
				<div bind:this={listRef} class="command-palette-list max-h-[420px] overflow-y-auto p-2" style="scrollbar-gutter: stable;">
					{#if flatCommands.length === 0}
						<div class="flex flex-col items-center justify-center gap-3 py-12">
							<div class="bg-muted/50 flex size-16 items-center justify-center rounded-2xl">
								<SearchIcon class="text-muted-foreground/50 size-7" />
							</div>
							<div class="text-center">
								<p class="text-foreground/80 text-sm font-medium">No commands found</p>
								<p class="text-muted-foreground text-xs">Try a different search term</p>
							</div>
						</div>
					{:else}
						{#each Array.from(filteredCommands.entries()) as [category, commands]}
							<div class="mb-3">
								<!-- Category Header with glass effect -->
								<div class="text-muted-foreground/70 mb-1.5 px-3 py-1 text-xs font-bold tracking-wider uppercase">
									{category}
								</div>

								<!-- Commands in Category -->
								<div class="space-y-0.5">
									{#each commands as command}
										{@const index = getCommandIndex(command)}
										{@const isSelected = index === selectedIndex}
										{@const keybindParts = getKeybindForCommand(command.id)}
										{@const isParent = hasSubCommands(command)}

										<button
											type="button"
											class={cn(
												'command-item group relative flex w-full items-center gap-3 rounded-xl px-3 py-3 text-left transition-all duration-150',
												isSelected
													? 'bubble-outline from-primary/10 to-primary/5 text-foreground ring-primary/20 bg-linear-to-br shadow-sm ring-1'
													: 'active:scale-[0.98]'
											)}
											data-selected={isSelected}
											onclick={() => executeCommand(command)}
											onmouseenter={() => handleCommandMouseEnter(index)}
										>
											<!-- Icon with glass container -->
											{#if command.icon}
												{@const Icon = command.icon}
												<div
													class={cn(
														'flex size-9 shrink-0 items-center justify-center rounded-lg transition-all',
														isSelected
															? 'bg-primary/15 text-primary ring-primary/30 ring-1'
															: 'bg-muted/50 text-muted-foreground group-hover:bg-muted/70 group-hover:text-foreground'
													)}
												>
													<Icon class="size-4" />
												</div>
											{/if}

											<!-- Label and Description -->
											<div class="flex min-w-0 flex-1 flex-col gap-0.5">
												<span class={cn('leading-tight font-semibold', isSelected ? 'text-foreground' : 'text-foreground/90')}>
													{command.label}
												</span>
												{#if command.description}
													<span class="text-muted-foreground truncate text-xs leading-tight">
														{command.description}
													</span>
												{/if}
											</div>

											<!-- Keybind or Arrow for nested -->
											<div class="ml-auto flex shrink-0 items-center gap-2">
												{#if keybindParts && keybindParts.length > 0 && isParent}
													<!-- Show keybind before chevron for parent commands -->
													<kbd
														class={cn(
															'hidden shrink-0 items-center gap-1 rounded-lg px-2 py-1.5 font-mono text-xs font-semibold shadow-sm ring-1 transition-all sm:inline-flex',
															isSelected
																? 'bg-primary/10 text-primary ring-primary/20'
																: 'bg-muted/50 text-muted-foreground ring-border/50 group-hover:bg-muted/70'
														)}
													>
														{#each keybindParts as part, i}
															{#if i > 0}
																<span class="text-[0.6rem] opacity-50">+</span>
															{/if}
															{#if part.type === 'command' || part.type === 'control'}
																<CommandIcon class="size-3" />
															{:else if part.type === 'shift'}
																<ShiftIcon class="size-3" />
															{:else if part.type === 'option'}
																<OptionIcon class="size-3" />
															{:else if part.type === 'text'}
																<span>{part.text}</span>
															{/if}
														{/each}
													</kbd>
												{/if}
												{#if isParent}
													<div
														class={cn(
															'flex size-7 shrink-0 items-center justify-center rounded-lg transition-all',
															isSelected
																? 'bg-primary/10 text-primary'
																: 'bg-muted/30 text-muted-foreground group-hover:bg-muted/50'
														)}
													>
														<ChevronRightIcon class="size-4" />
													</div>
												{:else if keybindParts && keybindParts.length > 0}
													<kbd
														class={cn(
															'hidden shrink-0 items-center gap-1 rounded-lg px-2 py-1.5 font-mono text-xs font-semibold shadow-sm ring-1 transition-all sm:inline-flex',
															isSelected
																? 'bg-primary/10 text-primary ring-primary/20'
																: 'bg-muted/50 text-muted-foreground ring-border/50 group-hover:bg-muted/70'
														)}
													>
														{#each keybindParts as part, i}
															{#if i > 0}
																<span class="text-[0.6rem] opacity-50">+</span>
															{/if}
															{#if part.type === 'command' || part.type === 'control'}
																<CommandIcon class="size-3" />
															{:else if part.type === 'shift'}
																<ShiftIcon class="size-3" />
															{:else if part.type === 'option'}
																<OptionIcon class="size-3" />
															{:else if part.type === 'text'}
																<span>{part.text}</span>
															{/if}
														{/each}
													</kbd>
												{/if}
											</div>
										</button>
									{/each}
								</div>
							</div>
						{/each}
					{/if}
				</div>

				<div class="from-muted/30 to-background/50 ring-border/50 bg-linear-to-br px-4 py-3 ring-1 backdrop-blur-sm ring-inset">
					<div class="flex items-center justify-between text-xs">
						<div class="flex items-center gap-3">
							<span class="text-muted-foreground flex items-center gap-1.5">
								<kbd
									class="bubble-pill bg-muted/60 text-foreground/80 ring-border/50 px-2 py-1 font-mono text-[10px] font-semibold shadow-sm ring-1"
									>↑↓</kbd
								>
								<span class="font-medium">Navigate</span>
							</span>
							<span class="text-muted-foreground flex items-center gap-1.5">
								<kbd
									class="bubble-pill bg-muted/60 text-foreground/80 ring-border/50 px-2 py-1 font-mono text-[10px] font-semibold shadow-sm ring-1"
									>→</kbd
								>
								<span class="font-medium">Open</span>
							</span>
							<span class="text-muted-foreground flex items-center gap-1.5">
								<kbd
									class="bubble-pill bg-muted/60 text-foreground/80 ring-border/50 px-2 py-1 font-mono text-[10px] font-semibold shadow-sm ring-1"
									>←</kbd
								>
								<span class="font-medium">Back</span>
							</span>
							<span class="text-muted-foreground flex items-center gap-1.5">
								<kbd
									class="bubble-pill bg-muted/60 text-foreground/80 ring-border/50 px-2 py-1 font-mono text-[10px] font-semibold shadow-sm ring-1"
									>↵</kbd
								>
								<span class="font-medium">Select</span>
							</span>
						</div>
					</div>
				</div>
			</div>
		</DialogPrimitive.Content>
	</DialogPrimitive.Portal>
</DialogPrimitive.Root>

<style>
	/* Enhanced scrollbar styling */
	.command-palette-list::-webkit-scrollbar {
		width: 10px;
	}

	.command-palette-list::-webkit-scrollbar-track {
		background: transparent;
		margin: 4px 0;
	}

	.command-palette-list::-webkit-scrollbar-thumb {
		background: color-mix(in oklch, var(--muted) 80%, transparent);
		border-radius: 6px;
		border: 2px solid transparent;
		background-clip: padding-box;
	}

	.command-palette-list::-webkit-scrollbar-thumb:hover {
		background: color-mix(in oklch, var(--muted-foreground) 50%, transparent);
		background-clip: padding-box;
	}

	/* Smooth scroll behavior */
	.command-palette-list {
		scroll-behavior: smooth;
	}

	/* Reduce motion for accessibility */
	@media (prefers-reduced-motion: reduce) {
		.command-palette-list {
			scroll-behavior: auto;
		}
	}

	/* Only enable hover effects when NOT in keyboard mode */
	[data-keyboard-mode='false'] .command-item:hover {
		background-color: color-mix(in oklch, var(--accent) 40%, transparent);
		box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.05);
	}

	/* Disable hover effects in keyboard mode */
	[data-keyboard-mode='true'] .command-item:hover {
		background-color: transparent;
		box-shadow: none;
	}
</style>

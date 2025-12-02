<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import SearchIcon from '@lucide/svelte/icons/search';
	import ServerIcon from '@lucide/svelte/icons/server';
	import RouterIcon from '@lucide/svelte/icons/router';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import CheckIcon from '@lucide/svelte/icons/check';
	import XIcon from '@lucide/svelte/icons/x';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';
	import SettingsIcon from '@lucide/svelte/icons/settings';
	import TagIcon from '@lucide/svelte/icons/tag';
	import type { Environment } from '$lib/types/environment.type';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { envSelectorFilterStore } from '$lib/stores/env-selector-filter.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { goto } from '$app/navigation';
	import settingsStore from '$lib/stores/config-store';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';

	interface Props {
		open?: boolean;
		isAdmin?: boolean;
		onOpenChange?: (open: boolean) => void;
		trigger?: import('svelte').Snippet;
	}

	let { open = $bindable(false), isAdmin = false, onOpenChange, trigger }: Props = $props();

	// State
	let environments = $state<Paginated<Environment> | null>(null);
	let isLoading = $state(false);
	let inputValue = $state('');
	let searchQuery = $state('');
	let filterPopoverOpen = $state(false);
	let searchInputRef = $state<HTMLInputElement | null>(null);
	let requestOptions = $state<SearchPaginationSortRequest>({
		pagination: { page: 1, limit: 100 },
		sort: { column: 'name', direction: 'asc' }
	});

	// Use persisted filter state from store
	const filterStore = envSelectorFilterStore;

	// Derived
	const allTags = $derived.by(() => {
		if (!environments?.data) return [];
		const tags = new Set<string>();
		for (const env of environments.data) {
			if (env.tags) {
				for (const tag of env.tags) {
					tags.add(tag);
				}
			}
		}
		return Array.from(tags).sort();
	});

	// Tag suggestion state
	let selectedSuggestionIndex = $state(-1);

	// Status options for is: syntax
	const statusOptions = ['online', 'offline'] as const;

	function getStatusLabel(status: string): string {
		return status === 'online' ? m.common_online() : m.common_offline();
	}

	// Determine what type of suggestion to show
	const suggestionType = $derived.by(() => {
		if (inputValue.match(/is:(\S*)$/i)) return 'status';
		if (inputValue.match(/-tag:(\S*)$/i)) return 'exclude';
		if (inputValue.match(/tag:(\S*)$/i)) return 'include';
		return null;
	});

	// Status suggestions for is: syntax (matches both key and translated label)
	const statusSuggestions = $derived.by(() => {
		const isMatch = inputValue.match(/is:(\S*)$/i);
		if (!isMatch) return [];
		
		const partial = isMatch[1].toLowerCase();
		const currentStatus = filterStore.statusFilter;
		return statusOptions.filter(
			(status) => (status.includes(partial) || getStatusLabel(status).toLowerCase().includes(partial)) && currentStatus !== status
		);
	});

	// Tag suggestions - exclude tags already in selectedTags OR excludedTags
	const tagSuggestions = $derived.by(() => {
		const excludeMatch = inputValue.match(/-tag:(\S*)$/i);
		const includeMatch = inputValue.match(/tag:(\S*)$/i);
		const tagMatch = excludeMatch || includeMatch;
		if (!tagMatch) return [];
		
		const partial = tagMatch[1].toLowerCase();
		const allUsedTags = [...filterStore.selectedTags, ...filterStore.excludedTags];
		return allTags.filter((tag) => !allUsedTags.includes(tag) && tag.toLowerCase().includes(partial));
	});

	const showSuggestions = $derived(tagSuggestions.length > 0 || statusSuggestions.length > 0);

	// Reset selection index when suggestions change
	$effect(() => {
		const totalSuggestions = tagSuggestions.length + statusSuggestions.length;
		if (totalSuggestions > 0) {
			selectedSuggestionIndex = 0;
		} else {
			selectedSuggestionIndex = -1;
		}
	});

	const filteredEnvironments = $derived.by(() => {
		if (!environments?.data) return [];

		let filtered = environments.data;

		// Filter by search query
		if (searchQuery.trim()) {
			const query = searchQuery.toLowerCase();
			filtered = filtered.filter(
				(env) =>
					env.name.toLowerCase().includes(query) ||
					env.apiUrl.toLowerCase().includes(query) ||
					env.tags?.some((tag) => tag.toLowerCase().includes(query))
			);
		}

		// Filter by status
		if (filterStore.statusFilter !== 'all') {
			filtered = filtered.filter((env) => env.status === filterStore.statusFilter);
		}

		// Filter by included tags (AND/OR mode)
		if (filterStore.selectedTags.length > 0) {
			if (filterStore.tagMode === 'all') {
				// AND mode: environment must have ALL selected tags
				filtered = filtered.filter((env) => filterStore.selectedTags.every((tag) => env.tags?.includes(tag)));
			} else {
				// OR mode: environment must have ANY selected tag
				filtered = filtered.filter((env) => env.tags?.some((tag) => filterStore.selectedTags.includes(tag)));
			}
		}

		// Filter by excluded tags (always exclude if ANY excluded tag matches)
		if (filterStore.excludedTags.length > 0) {
			filtered = filtered.filter((env) => !env.tags?.some((tag) => filterStore.excludedTags.includes(tag)));
		}

		return filtered;
	});

	// Group environments based on groupBy setting
	const groupedEnvironments = $derived.by(() => {
		if (filterStore.groupBy === 'none') return null;

		const groups = new Map<string, Environment[]>();

		for (const env of filteredEnvironments) {
			if (filterStore.groupBy === 'status') {
				const key = env.status;
				const items = groups.get(key) ?? [];
				items.push(env);
				groups.set(key, items);
			} else if (filterStore.groupBy === 'tags') {
				if (env.tags && env.tags.length > 0) {
					for (const tag of env.tags) {
						const items = groups.get(tag) ?? [];
						if (!items.some((e) => e.id === env.id)) {
							items.push(env);
						}
						groups.set(tag, items);
					}
				} else {
					const key = m.env_selector_untagged();
					const items = groups.get(key) ?? [];
					items.push(env);
					groups.set(key, items);
				}
			}
		}

		return Array.from(groups.entries())
			.map(([name, items]) => ({ name, items }))
			.sort((a, b) => {
				// Put 'online' first for status grouping
				if (filterStore.groupBy === 'status') {
					if (a.name === 'online') return -1;
					if (b.name === 'online') return 1;
				}
				return a.name.localeCompare(b.name);
			});
	});

	// Load environments when dialog opens
	$effect(() => {
		if (open) {
			loadEnvironments();
		}
	});

	async function loadEnvironments() {
		isLoading = true;
		try {
			environments = await environmentManagementService.getEnvironments(requestOptions);
		} catch (error) {
			console.error('Failed to load environments:', error);
			toast.error(m.common_refresh_failed({ resource: m.environments_title() }));
		} finally {
			isLoading = false;
		}
	}

	function handleSearchInput(event: Event) {
		const target = event.target as HTMLInputElement;
		inputValue = target.value;
		
		// Extract search query without tag:, -tag:, and is: prefixes
		const withoutFilters = target.value.replace(/-?tag:\S*/gi, '').replace(/is:\S*/gi, '').trim();
		searchQuery = withoutFilters;
	}

	function handleSearchKeydown(event: KeyboardEvent) {
		const totalSuggestions = tagSuggestions.length + statusSuggestions.length;
		
		if (showSuggestions) {
			if (event.key === 'ArrowDown') {
				event.preventDefault();
				selectedSuggestionIndex = Math.min(selectedSuggestionIndex + 1, totalSuggestions - 1);
				scrollSelectedIntoView();
			} else if (event.key === 'ArrowUp') {
				event.preventDefault();
				selectedSuggestionIndex = Math.max(selectedSuggestionIndex - 1, 0);
				scrollSelectedIntoView();
			} else if (event.key === 'Tab' || event.key === 'Enter') {
				event.preventDefault();
				selectSuggestion(selectedSuggestionIndex);
			} else if (event.key === 'Escape') {
				// Clear the filter prefix to close suggestions
				inputValue = inputValue.replace(/-?tag:\S*$/i, '').replace(/is:\S*$/i, '').trim();
				searchQuery = inputValue;
			}
		} else if (event.key === 'Enter') {
			let hasMatches = false;
			
			// Check for is: syntax (status filter)
			const isMatches = inputValue.match(/is:(\S+)/gi);
			if (isMatches) {
				for (const match of isMatches) {
					const status = match.slice(3).toLowerCase(); // Remove 'is:' prefix
					if (status === 'online' || status === 'offline') {
						filterStore.statusFilter = status;
						hasMatches = true;
					}
				}
			}
			
			// Check for -tag: syntax (exclusion)
			const excludeMatches = inputValue.match(/-tag:(\S+)/gi);
			if (excludeMatches) {
				for (const match of excludeMatches) {
					const tagName = match.slice(5); // Remove '-tag:' prefix
					const exactMatch = allTags.find((t) => t.toLowerCase() === tagName.toLowerCase());
					const partialMatch = allTags.find((t) => t.toLowerCase().includes(tagName.toLowerCase()));
					const matchedTag = exactMatch || partialMatch;
					
					if (matchedTag && !filterStore.excludedTags.includes(matchedTag)) {
						filterStore.addExcludedTag(matchedTag);
						hasMatches = true;
					}
				}
			}
			
			// Check for tag: syntax (inclusion) - filter out -tag: matches
			const allTagMatches = inputValue.match(/\btag:(\S+)/gi) || [];
			const includeMatches = allTagMatches.filter((m) => {
				const idx = inputValue.indexOf(m);
				return idx === 0 || inputValue[idx - 1] !== '-';
			});
			if (includeMatches.length > 0) {
				for (const match of includeMatches) {
					const tagName = match.slice(4); // Remove 'tag:' prefix
					const exactMatch = allTags.find((t) => t.toLowerCase() === tagName.toLowerCase());
					const partialMatch = allTags.find((t) => t.toLowerCase().includes(tagName.toLowerCase()));
					const matchedTag = exactMatch || partialMatch;
					
					if (matchedTag && !filterStore.selectedTags.includes(matchedTag)) {
						filterStore.addTag(matchedTag);
						hasMatches = true;
					}
				}
			}
			
			// Clear all filter patterns from input
			if (hasMatches) {
				inputValue = inputValue.replace(/-?tag:\S*/gi, '').replace(/is:\S*/gi, '').trim();
				searchQuery = inputValue;
			}
		}
	}

	function selectSuggestion(index: number) {
		if (suggestionType === 'status' && index < statusSuggestions.length) {
			const status = statusSuggestions[index];
			filterStore.statusFilter = status as 'online' | 'offline';
			inputValue = inputValue.replace(/is:\S*$/i, '').trim();
		} else if (suggestionType === 'exclude') {
			const tag = tagSuggestions[index];
			filterStore.addExcludedTag(tag);
			inputValue = inputValue.replace(/-tag:\S*$/i, '').trim();
		} else if (suggestionType === 'include') {
			const tag = tagSuggestions[index];
			filterStore.addTag(tag);
			inputValue = inputValue.replace(/tag:\S*$/i, '').trim();
		}
		searchQuery = inputValue;
		searchInputRef?.focus();
	}

	function selectTagSuggestion(tag: string) {
		if (suggestionType === 'exclude') {
			filterStore.addExcludedTag(tag);
			inputValue = inputValue.replace(/-tag:\S*$/i, '').trim();
		} else {
			filterStore.addTag(tag);
			inputValue = inputValue.replace(/tag:\S*$/i, '').trim();
		}
		searchQuery = inputValue;
		searchInputRef?.focus();
	}

	function scrollSelectedIntoView() {
        const selected = document.querySelector(`[data-index="${selectedSuggestionIndex}"]`);
		selected?.scrollIntoView({ block: 'nearest' });
	}

	async function handleSelectEnvironment(env: Environment) {
		if (!env.enabled) {
			toast.error(m.environments_cannot_switch_disabled());
			return;
		}

		try {
			await environmentStore.setEnvironment(env);
			toast.success(m.environments_switched_to({ name: env.name }));
			open = false;
			onOpenChange?.(false);
		} catch (error) {
			console.error('Failed to set environment:', error);
			toast.error(m.env_selector_switch_failed());
		}
	}

	function handleManageEnvironments() {
		open = false;
		onOpenChange?.(false);
		goto('/environments');
	}

	function clearFilters() {
		searchQuery = '';
		inputValue = '';
		filterStore.clearFilters();
	}

	function getConnectionString(env: Environment): string {
		if (env.id === '0') {
			return $settingsStore.dockerHost || 'unix:///var/run/docker.sock';
		}
		return env.apiUrl;
	}

	function getStatusColor(status: string): string {
		switch (status) {
			case 'online':
				return 'bg-emerald-500';
			case 'offline':
				return 'bg-red-500';
			case 'error':
				return 'bg-amber-500';
			default:
				return 'bg-gray-400';
		}
	}

	const hasActiveFilters = $derived(filterStore.selectedTags.length > 0 || filterStore.excludedTags.length > 0 || filterStore.statusFilter !== 'all' || filterStore.groupBy !== 'none');
	const activeFilterCount = $derived((filterStore.groupBy !== 'none' ? 1 : 0) + (filterStore.tagMode !== 'any' ? 1 : 0));
	const hasTagFilters = $derived(filterStore.selectedTags.length > 0 || filterStore.excludedTags.length > 0);
</script>

{#snippet environmentItem(env: Environment)}
	{@const isSelected = environmentStore.selected?.id === env.id}
	{@const isDisabled = !env.enabled}
	<button
		class={cn(
			'group relative flex w-full items-center gap-3 rounded-lg p-2.5 text-left transition-all',
			isSelected
				? 'bg-primary/10 ring-primary/40 ring-1'
				: isDisabled
					? 'cursor-not-allowed opacity-50'
					: 'hover:bg-muted/60'
		)}
		onclick={() => handleSelectEnvironment(env)}
		disabled={isDisabled}
	>
		<!-- Status dot + Icon -->
		<div class="relative">
			<div
				class={cn(
					'flex size-9 items-center justify-center rounded-md',
					isSelected ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
				)}
			>
				{#if env.id === '0'}
					<ServerIcon class="size-4" />
				{:else}
					<RouterIcon class="size-4" />
				{/if}
			</div>
			<span
				class={cn('absolute -right-0.5 -top-0.5 size-2.5 rounded-full ring-2 ring-background', getStatusColor(env.status))}
			></span>
		</div>

		<!-- Environment Info -->
		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-2">
				<span class={cn('truncate text-sm font-medium', isSelected && 'text-primary')}>{env.name}</span>
				{#if isSelected}
					<CheckIcon class="text-primary size-4 shrink-0" />
				{/if}
			</div>
			<div class="text-muted-foreground truncate text-xs">{getConnectionString(env)}</div>
		</div>

		<!-- Tags (compact) -->
		{#if env.tags && env.tags.length > 0}
			<div class="hidden shrink-0 sm:flex sm:gap-1">
				{#each env.tags.slice(0, 2) as tag}
					<span class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px]">{tag}</span>
				{/each}
				{#if env.tags.length > 2}
					<span class="text-muted-foreground text-[10px]">+{env.tags.length - 2}</span>
				{/if}
			</div>
		{/if}
	</button>
{/snippet}

{#snippet envGroup(group: { name: string; items: Environment[] })}
	<Collapsible.Root class="w-full" open={false}>
		<Collapsible.Trigger class="hover:bg-muted/40 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors">
			<ChevronDownIcon class="text-muted-foreground size-4 transition-transform in-data-[state=closed]:-rotate-90" />
			<span class="text-sm font-medium">{group.name}</span>
			<span class="text-muted-foreground text-xs">({group.items.length})</span>
		</Collapsible.Trigger>
		<Collapsible.Content>
			<div class="mt-1 space-y-0.5 pl-2">
				{#each group.items as env (env.id)}
					{@render environmentItem(env)}
				{/each}
			</div>
		</Collapsible.Content>
	</Collapsible.Root>
{/snippet}

<ResponsiveDialog
	bind:open
	{onOpenChange}
	{trigger}
	title={m.env_selector_title()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		<div class="flex flex-col gap-3">
			<!-- Search + Filter Row -->
			<div class="flex gap-2">
				<div class="relative flex-1">
					<SearchIcon class="text-muted-foreground absolute left-3 top-1/2 size-4 -translate-y-1/2" />
					<Input
						bind:ref={searchInputRef}
						type="text"
						placeholder={m.common_search()}
						class="h-9 pl-9 pr-8 text-sm"
						value={inputValue}
						oninput={handleSearchInput}
						onkeydown={handleSearchKeydown}
					/>
					
					<!-- Suggestions Dropdown -->
					{#if showSuggestions}
						<div class="bg-popover border-border absolute left-0 right-0 top-full z-50 mt-1 rounded-md border shadow-md">
							<div class="text-muted-foreground px-3 py-1.5 text-xs">{m.env_selector_suggestions()}</div>
							<div class="max-h-[180px] overflow-y-auto p-1 pt-0">
								{#if statusSuggestions.length > 0}
									{#each statusSuggestions as status, index}
										<button
											class={cn(
												'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors',
												index === selectedSuggestionIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
											)}
											onclick={() => selectSuggestion(index)}
											onmouseenter={() => (selectedSuggestionIndex = index)}
											data-index={index}
										>
											<span class={cn('size-2 rounded-full', status === 'online' ? 'bg-emerald-500' : 'bg-red-500')}></span>
											<span>{getStatusLabel(status)}</span>
										</button>
									{/each}
								{/if}
								{#if tagSuggestions.length > 0}
									{#each tagSuggestions as tag, index}
										<button
											class={cn(
												'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors',
												index === selectedSuggestionIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
											)}
											onclick={() => selectSuggestion(index)}
											onmouseenter={() => (selectedSuggestionIndex = index)}
											data-index={index}
										>
											<TagIcon class="size-3.5" />
											<span>{tag}</span>
										</button>
									{/each}
								{/if}
							</div>
						</div>
					{/if}
				</div>

				<!-- Filter Popover (Status & Group only) -->
				<Popover.Root bind:open={filterPopoverOpen}>
					<Popover.Trigger>
						<Button variant="outline" size="sm" class="h-9 gap-1.5">
							<FilterIcon class="size-4" />
							<span class="hidden sm:inline">{m.common_filter()}</span>
							{#if activeFilterCount > 0}
								<Badge variant="default" class="ml-1 size-5 justify-center p-0 text-[10px]">{activeFilterCount}</Badge>
							{/if}
						</Button>
					</Popover.Trigger>
					<Popover.Content class="w-64 p-3" align="end">
						<div class="space-y-4">
							<!-- Group By -->
							<div class="space-y-2">
								<div class="text-sm font-medium">{m.env_selector_group_by()}</div>
								<div class="flex flex-wrap gap-1.5">
									<button
										class={cn(
											'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
											filterStore.groupBy === 'none'
												? 'bg-primary text-primary-foreground'
												: 'bg-muted hover:bg-muted/80'
										)}
										onclick={() => (filterStore.groupBy = 'none')}
									>
										{m.common_none()}
									</button>
									<button
										class={cn(
											'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
											filterStore.groupBy === 'status'
												? 'bg-primary text-primary-foreground'
												: 'bg-muted hover:bg-muted/80'
										)}
										onclick={() => (filterStore.groupBy = 'status')}
									>
										{m.common_status()}
									</button>
									{#if allTags.length > 0}
										<button
											class={cn(
												'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
												filterStore.groupBy === 'tags'
													? 'bg-primary text-primary-foreground'
													: 'bg-muted hover:bg-muted/80'
											)}
											onclick={() => (filterStore.groupBy = 'tags')}
										>
											{m.env_selector_tags()}
										</button>
									{/if}
							</div>
						</div>

						<!-- Tag Match Mode (only show if tags are selected) -->
							{#if filterStore.selectedTags.length > 1}
								<div class="space-y-2">
									<div class="text-sm font-medium">{m.env_selector_tag_mode()}</div>
									<div class="flex gap-1.5">
										<button
											class={cn(
												'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
												filterStore.tagMode === 'any'
													? 'bg-primary text-primary-foreground'
													: 'bg-muted hover:bg-muted/80'
											)}
											onclick={() => (filterStore.tagMode = 'any')}
										>
											{m.env_selector_tag_mode_any()}
										</button>
										<button
											class={cn(
												'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
												filterStore.tagMode === 'all'
													? 'bg-primary text-primary-foreground'
													: 'bg-muted hover:bg-muted/80'
											)}
											onclick={() => (filterStore.tagMode = 'all')}
										>
											{m.env_selector_tag_mode_all()}
										</button>
									</div>
								</div>
							{/if}

							<!-- Clear Filters -->
							{#if hasActiveFilters}
								<Button variant="ghost" size="sm" class="w-full" onclick={clearFilters}>
									<XIcon class="mr-1.5 size-3" />
									{m.common_clear_filters()}
								</Button>
							{/if}
						</div>
					</Popover.Content>
				</Popover.Root>
			</div>

			<!-- Active Filter Pills -->
			{#if hasTagFilters || filterStore.statusFilter !== 'all'}
				<div class="flex flex-wrap items-center gap-1.5">
					{#if filterStore.statusFilter !== 'all'}
						<Badge variant="secondary" class="gap-1 pr-1">
							<span class={cn('size-1.5 rounded-full', filterStore.statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500')}></span>
							{filterStore.statusFilter === 'online' ? m.common_online() : m.common_offline()}
							<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => (filterStore.statusFilter = 'all')}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/if}
					{#if filterStore.selectedTags.length > 1}
						<Badge variant="secondary" class="text-[10px]">
							{filterStore.tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}
						</Badge>
					{/if}
					{#each filterStore.selectedTags as tag}
						<Badge variant="outline" class="gap-1 pr-1">
							<TagIcon class="size-3" />
							{tag}
							<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => filterStore.removeTag(tag)}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/each}
					{#each filterStore.excludedTags as tag}
						<Badge variant="outline" class="gap-1 border-red-500/50 pr-1 text-red-600 dark:text-red-400">
							<XIcon class="size-3" />
							{tag}
							<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => filterStore.removeExcludedTag(tag)}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/each}
					{#if hasTagFilters}
						<button class="text-muted-foreground hover:text-foreground text-xs" onclick={() => filterStore.clearTags()}>
							{m.common_clear_tags()}
						</button>
					{/if}
				</div>
			{/if}

			<!-- Results count -->
			<div class="text-muted-foreground flex items-center justify-between text-xs">
				<span>
					{m.env_selector_showing_count({ count: filteredEnvironments.length, total: environments?.data?.length ?? 0 })}
				</span>
			</div>

			<!-- Environment List -->
			<ScrollArea class="max-h-[45vh] min-h-[180px]">
				{#if isLoading}
					<div class="flex h-40 items-center justify-center">
						<Spinner class="size-6" />
					</div>
				{:else if filteredEnvironments.length === 0}
					<div class="text-muted-foreground flex h-40 flex-col items-center justify-center text-center">
						<ServerIcon class="mb-2 size-10 opacity-30" />
						<p class="text-sm font-medium">{m.env_selector_no_environments()}</p>
						{#if hasActiveFilters || searchQuery}
							<p class="mt-1 text-xs">{m.env_selector_try_different_filters()}</p>
							<Button variant="ghost" size="sm" class="mt-2" onclick={clearFilters}>
								{m.common_clear_filters()}
							</Button>
						{/if}
					</div>
				{:else if groupedEnvironments && groupedEnvironments.length > 0}
					<div class="space-y-2 p-1">
						{#each groupedEnvironments as group (group.name)}
							{@render envGroup(group)}
						{/each}
					</div>
				{:else}
					<div class="space-y-1 p-1">
						{#each filteredEnvironments as env (env.id)}
							{@render environmentItem(env)}
						{/each}
					</div>
				{/if}
			</ScrollArea>
		</div>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full items-center justify-between">
			{#if isAdmin}
				<Button variant="ghost" size="sm" onclick={handleManageEnvironments}>
					<SettingsIcon class="mr-1.5 size-4" />
					{m.sidebar_manage_environments()}
				</Button>
			{:else}
				<div></div>
			{/if}
			<Button variant="ghost" size="sm" onclick={() => (open = false)}>{m.common_close()}</Button>
		</div>
	{/snippet}
</ResponsiveDialog>

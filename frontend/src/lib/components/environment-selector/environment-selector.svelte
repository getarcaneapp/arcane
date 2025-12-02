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
	import {
		envSelectorFilterStore,
		addTag,
		removeTag,
		addExcludedTag,
		removeExcludedTag,
		clearTags,
		clearFilters
	} from '$lib/stores/env-selector-filter.store.svelte';
	import { environmentManagementService } from '$lib/services/env-mgmt-service';
	import { toast } from 'svelte-sonner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { goto } from '$app/navigation';
	import settingsStore from '$lib/stores/config-store';
	import type { Paginated } from '$lib/types/pagination.type';

	interface Props {
		open?: boolean;
		isAdmin?: boolean;
		onOpenChange?: (open: boolean) => void;
		trigger?: import('svelte').Snippet;
	}

	let { open = $bindable(false), isAdmin = false, onOpenChange, trigger }: Props = $props();

	let environments = $state<Paginated<Environment> | null>(null);
	let isLoading = $state(false);
	let inputValue = $state('');
	let filterPopoverOpen = $state(false);
	let searchInputRef = $state<HTMLInputElement | null>(null);
	let selectedSuggestionIndex = $state(0);

	const filters = $derived(envSelectorFilterStore.current);
	const allTags = $derived(
		[...new Set(environments?.data?.flatMap((env) => env.tags ?? []) ?? [])].sort()
	);

	const inputMatch = $derived.by(() => {
		const isMatch = inputValue.match(/is:(\S*)$/i);
		if (isMatch) return { type: 'status' as const, partial: isMatch[1].toLowerCase() };

		const excludeMatch = inputValue.match(/-tag:(\S*)$/i);
		if (excludeMatch) return { type: 'exclude' as const, partial: excludeMatch[1].toLowerCase() };

		const includeMatch = inputValue.match(/tag:(\S*)$/i);
		if (includeMatch) return { type: 'include' as const, partial: includeMatch[1].toLowerCase() };

		return null;
	});

	const suggestions = $derived.by(() => {
		if (!inputMatch) return [];

		if (inputMatch.type === 'status') {
			const statusOptions = [
				{ value: 'online', label: m.common_online() },
				{ value: 'offline', label: m.common_offline() }
			];
			return statusOptions.filter(
				(s) =>
					(s.value.includes(inputMatch.partial) || s.label.toLowerCase().includes(inputMatch.partial)) &&
					filters.statusFilter !== s.value
			);
		}

		// Tag suggestions - exclude already used tags
		const usedTags = new Set([...filters.selectedTags, ...filters.excludedTags]);
		return allTags
			.filter((tag) => !usedTags.has(tag) && tag.toLowerCase().includes(inputMatch.partial))
			.map((tag) => ({ value: tag, label: tag }));
	});

	// Reset suggestion index when suggestions change
	$effect(() => {
		selectedSuggestionIndex = suggestions.length > 0 ? 0 : -1;
	});

	const searchQuery = $derived(inputValue.replace(/-?tag:\S*/gi, '').replace(/is:\S*/gi, '').trim());

	const filteredEnvironments = $derived.by(() => {
		if (!environments?.data) return [];

		return environments.data.filter((env) => {
			// Search filter
			if (searchQuery) {
				const q = searchQuery.toLowerCase();
				const matches =
					env.name.toLowerCase().includes(q) ||
					env.apiUrl.toLowerCase().includes(q) ||
					env.tags?.some((t) => t.toLowerCase().includes(q));
				if (!matches) return false;
			}

			// Status filter
			if (filters.statusFilter !== 'all' && env.status !== filters.statusFilter) return false;

			// Include tags (AND/OR)
			if (filters.selectedTags.length > 0) {
				const hasTag = filters.tagMode === 'all'
					? filters.selectedTags.every((t) => env.tags?.includes(t))
					: filters.selectedTags.some((t) => env.tags?.includes(t));
				if (!hasTag) return false;
			}

			// Exclude tags
			if (filters.excludedTags.some((t) => env.tags?.includes(t))) return false;

			return true;
		});
	});

	const groupedEnvironments = $derived.by(() => {
		if (filters.groupBy === 'none') return null;

		const groups = new Map<string, Environment[]>();

		for (const env of filteredEnvironments) {
			const keys =
				filters.groupBy === 'status'
					? [env.status]
					: env.tags?.length
						? env.tags
						: [m.env_selector_untagged()];

			for (const key of keys) {
				const items = groups.get(key) ?? [];
				if (!items.some((e) => e.id === env.id)) items.push(env);
				groups.set(key, items);
			}
		}

		return [...groups.entries()]
			.map(([name, items]) => ({ name, items }))
			.sort((a, b) => (a.name === 'online' ? -1 : b.name === 'online' ? 1 : a.name.localeCompare(b.name)));
	});

	const hasActiveFilters = $derived(
		filters.selectedTags.length > 0 ||
			filters.excludedTags.length > 0 ||
			filters.statusFilter !== 'all' ||
			filters.groupBy !== 'none'
	);
	const activeFilterCount = $derived((filters.groupBy !== 'none' ? 1 : 0) + (filters.tagMode !== 'any' ? 1 : 0));
	const hasTagFilters = $derived(filters.selectedTags.length > 0 || filters.excludedTags.length > 0);

	// Load environments when dialog opens
	$effect(() => {
		if (open) loadEnvironments();
	});

	async function loadEnvironments() {
		isLoading = true;
		try {
			environments = await environmentManagementService.getEnvironments({
				pagination: { page: 1, limit: 100 },
				sort: { column: 'name', direction: 'asc' }
			});
		} catch (error) {
			console.error('Failed to load environments:', error);
			toast.error(m.common_refresh_failed({ resource: m.environments_title() }));
		} finally {
			isLoading = false;
		}
	}

	function handleKeydown(event: KeyboardEvent) {
		if (suggestions.length > 0) {
			if (event.key === 'ArrowDown') {
				event.preventDefault();
				selectedSuggestionIndex = Math.min(selectedSuggestionIndex + 1, suggestions.length - 1);
			} else if (event.key === 'ArrowUp') {
				event.preventDefault();
				selectedSuggestionIndex = Math.max(selectedSuggestionIndex - 1, 0);
			} else if (event.key === 'Tab' || event.key === 'Enter') {
				event.preventDefault();
				selectSuggestion(selectedSuggestionIndex);
			} else if (event.key === 'Escape') {
				inputValue = searchQuery;
			}
		}
	}

	function selectSuggestion(index: number) {
		const suggestion = suggestions[index];
		if (!suggestion || !inputMatch) return;

		if (inputMatch.type === 'status') {
			envSelectorFilterStore.current = { ...filters, statusFilter: suggestion.value as 'online' | 'offline' };
		} else if (inputMatch.type === 'exclude') {
			addExcludedTag(suggestion.value);
		} else {
			addTag(suggestion.value);
		}

		inputValue = searchQuery;
		searchInputRef?.focus();
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

	function handleClearFilters() {
		inputValue = '';
		clearFilters();
	}

	function getConnectionString(env: Environment): string {
		return env.id === '0' ? ($settingsStore.dockerHost || 'unix:///var/run/docker.sock') : env.apiUrl;
	}

	function getStatusColor(status: string): string {
		return status === 'online' ? 'bg-emerald-500' : status === 'offline' ? 'bg-red-500' : 'bg-gray-400';
	}
</script>

{#snippet environmentItem(env: Environment)}
	{@const isSelected = environmentStore.selected?.id === env.id}
	{@const isDisabled = !env.enabled}
	<button
		class={cn(
			'group relative flex w-full items-center gap-3 rounded-lg p-2.5 text-left transition-all',
			isSelected ? 'bg-primary/10 ring-primary/40 ring-1' : isDisabled ? 'cursor-not-allowed opacity-50' : 'hover:bg-muted/60'
		)}
		onclick={() => handleSelectEnvironment(env)}
		disabled={isDisabled}
	>
		<div class="relative">
			<div class={cn('flex size-9 items-center justify-center rounded-md', isSelected ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground')}>
				{#if env.id === '0'}<ServerIcon class="size-4" />{:else}<RouterIcon class="size-4" />{/if}
			</div>
			<span class={cn('absolute -right-0.5 -top-0.5 size-2.5 rounded-full ring-2 ring-background', getStatusColor(env.status))}></span>
		</div>

		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-2">
				<span class={cn('truncate text-sm font-medium', isSelected && 'text-primary')}>{env.name}</span>
				{#if isSelected}<CheckIcon class="text-primary size-4 shrink-0" />{/if}
			</div>
			<div class="text-muted-foreground truncate text-xs">{getConnectionString(env)}</div>
		</div>

		{#if env.tags?.length}
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

<ResponsiveDialog bind:open {onOpenChange} {trigger} title={m.env_selector_title()} contentClass="sm:max-w-2xl">
	{#snippet children()}
		<div class="flex flex-col gap-3">
			<!-- Search + Filter -->
			<div class="flex gap-2 pt-2">
				<div class="relative flex-1">
					<SearchIcon class="text-muted-foreground absolute left-3 top-1/2 size-4 -translate-y-1/2" />
					<Input
						bind:ref={searchInputRef}
						type="text"
						placeholder={m.common_search()}
						class="h-9 pl-9 pr-8 text-sm"
						bind:value={inputValue}
						onkeydown={handleKeydown}
					/>

					<!-- Suggestions Dropdown -->
					{#if suggestions.length > 0}
						<div class="bg-popover border-border absolute left-0 right-0 top-full z-50 mt-1 rounded-md border shadow-md">
							<div class="text-muted-foreground px-3 py-1.5 text-xs">{m.env_selector_suggestions()}</div>
							<div class="max-h-[180px] overflow-y-auto p-1 pt-0">
								{#each suggestions as suggestion, index}
									<button
										class={cn(
											'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors',
											index === selectedSuggestionIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-muted'
										)}
										onclick={() => selectSuggestion(index)}
										onmouseenter={() => (selectedSuggestionIndex = index)}
									>
										{#if inputMatch?.type === 'status'}
											<span class={cn('size-2 rounded-full', suggestion.value === 'online' ? 'bg-emerald-500' : 'bg-red-500')}></span>
										{:else}
											<TagIcon class="size-3.5" />
										{/if}
										<span>{suggestion.label}</span>
									</button>
								{/each}
							</div>
						</div>
					{/if}
				</div>

				<!-- Filter Popover -->
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
									{#each [{ value: 'none', label: m.common_none() }, { value: 'status', label: m.common_status() }, ...(allTags.length ? [{ value: 'tags', label: m.env_selector_tags() }] : [])] as option}
										<button
											class={cn(
												'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
												filters.groupBy === option.value ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
											)}
											onclick={() => (envSelectorFilterStore.current = { ...filters, groupBy: option.value as any })}
										>
											{option.label}
										</button>
									{/each}
								</div>
							</div>

							<!-- Tag Mode -->
							{#if filters.selectedTags.length > 1}
								<div class="space-y-2">
									<div class="text-sm font-medium">{m.env_selector_tag_mode()}</div>
									<div class="flex gap-1.5">
										{#each [{ value: 'any', label: m.env_selector_tag_mode_any() }, { value: 'all', label: m.env_selector_tag_mode_all() }] as option}
											<button
												class={cn(
													'rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
													filters.tagMode === option.value ? 'bg-primary text-primary-foreground' : 'bg-muted hover:bg-muted/80'
												)}
												onclick={() => (envSelectorFilterStore.current = { ...filters, tagMode: option.value as any })}
											>
												{option.label}
											</button>
										{/each}
									</div>
								</div>
							{/if}

							{#if hasActiveFilters}
								<Button variant="ghost" size="sm" class="w-full" onclick={handleClearFilters}>
									<XIcon class="mr-1.5 size-3" />
									{m.common_clear_filters()}
								</Button>
							{/if}
						</div>
					</Popover.Content>
				</Popover.Root>
			</div>

			<!-- Active Filter Pills -->
			{#if hasTagFilters || filters.statusFilter !== 'all'}
				<div class="flex flex-wrap items-center gap-1.5">
					{#if filters.statusFilter !== 'all'}
						<Badge variant="secondary" class="gap-1 pr-1">
							<span class={cn('size-1.5 rounded-full', getStatusColor(filters.statusFilter))}></span>
							{filters.statusFilter === 'online' ? m.common_online() : m.common_offline()}
							<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => (envSelectorFilterStore.current = { ...filters, statusFilter: 'all' })}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/if}
					{#if filters.selectedTags.length > 1}
						<Badge variant="secondary" class="text-[10px]">
							{filters.tagMode === 'all' ? m.env_selector_tag_mode_all() : m.env_selector_tag_mode_any()}
						</Badge>
					{/if}
					{#each filters.selectedTags as tag}
						<Badge variant="outline" class="gap-1 pr-1">
							<TagIcon class="size-3" />
							{tag}
							<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => removeTag(tag)}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/each}
					{#each filters.excludedTags as tag}
						<Badge variant="outline" class="gap-1 border-red-500/50 pr-1 text-red-600 dark:text-red-400">
							<TagIcon class="size-3" />
							{tag}
							<button class="hover:bg-muted ml-0.5 rounded p-0.5" onclick={() => removeExcludedTag(tag)}>
								<XIcon class="size-3" />
							</button>
						</Badge>
					{/each}
					{#if hasTagFilters}
						<button class="text-muted-foreground hover:text-foreground text-xs" onclick={clearTags}>
							{m.common_clear_tags()}
						</button>
					{/if}
				</div>
			{/if}

			<!-- Results count -->
			<div class="text-muted-foreground text-xs">
				{m.env_selector_showing_count({ count: filteredEnvironments.length, total: environments?.data?.length ?? 0 })}
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
							<Button variant="ghost" size="sm" class="mt-2" onclick={handleClearFilters}>
								{m.common_clear_filters()}
							</Button>
						{/if}
					</div>
				{:else if groupedEnvironments}
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
				<Button variant="ghost" size="sm" onclick={() => { open = false; onOpenChange?.(false); goto('/environments'); }}>
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

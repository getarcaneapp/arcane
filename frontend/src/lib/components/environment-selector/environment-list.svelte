<script lang="ts">
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import ServerIcon from '@lucide/svelte/icons/server';
	import { m } from '$lib/paraglide/messages';
	import EnvironmentItem from './environment-item.svelte';
	import EnvironmentGroup from './environment-group.svelte';
	import type { Environment, EnvironmentGroup as EnvironmentGroupType } from './types';

	interface Props {
		environments: Environment[];
		groupedEnvironments: EnvironmentGroupType[] | null;
		selectedEnvId?: string;
		isLoading?: boolean;
		hasActiveFilters?: boolean;
		searchQuery?: string;
		hasMorePages?: boolean;
		onSelect?: (env: Environment) => void;
		onClearFilters?: () => void;
		onLoadMore?: () => void;
	}

	let {
		environments,
		groupedEnvironments,
		selectedEnvId,
		isLoading = false,
		hasActiveFilters = false,
		searchQuery = '',
		hasMorePages = false,
		onSelect,
		onClearFilters,
		onLoadMore
	}: Props = $props();
</script>

<ScrollArea class="max-h-[45vh] min-h-[180px]">
	{#if isLoading && environments.length === 0}
		<div class="flex h-40 items-center justify-center">
			<Spinner class="size-6" />
		</div>
	{:else if environments.length === 0}
		<div class="text-muted-foreground flex h-40 flex-col items-center justify-center text-center">
			<ServerIcon class="mb-2 size-10 opacity-30" />
			<p class="text-sm font-medium">{m.common_no_results_found()}</p>
			{#if hasActiveFilters || searchQuery}
				<p class="mt-1 text-xs">{m.env_selector_try_different_filters()}</p>
				<Button variant="ghost" size="sm" class="mt-2" onclick={onClearFilters}>
					{m.common_clear_filters()}
				</Button>
			{/if}
		</div>
	{:else if groupedEnvironments}
		<div class="space-y-2 p-1">
			{#each groupedEnvironments as group (group.name)}
				<EnvironmentGroup {group} {selectedEnvId} {onSelect} />
			{/each}
		</div>
	{:else}
		<div class="space-y-1 p-1">
			{#each environments as env (env.id)}
				<EnvironmentItem {env} isSelected={selectedEnvId === env.id} {onSelect} />
			{/each}
		</div>
	{/if}

	{#if hasMorePages}
		<div class="flex justify-center py-2">
			<Button variant="ghost" size="sm" onclick={onLoadMore} disabled={isLoading}>
				{#if isLoading}
					<Spinner class="mr-2 size-4" />
				{/if}
				{m.common_load_more()}
			</Button>
		</div>
	{/if}
</ScrollArea>

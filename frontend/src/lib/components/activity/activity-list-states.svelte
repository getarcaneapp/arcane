<script lang="ts">
	import type { Snippet } from 'svelte';

	import { ActivityIcon, AlertTriangleIcon, SearchIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';

	let {
		filtered = true,
		children
	}: {
		/** Whether the content honours search and filters; unfiltered views never show "no results". */
		filtered?: boolean;
		children: Snippet;
	} = $props();

	const hasNoResults = $derived(
		filtered &&
			activityStore.activities.length > 0 &&
			activityStore.hasActiveFilters &&
			activityStore.runningGroups.length === 0 &&
			activityStore.historyGroups.length === 0
	);
</script>

{#if activityStore.environmentFailures.length > 0}
	<div class="mx-3 mt-2 flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs">
		<AlertTriangleIcon class="mt-px size-3.5 shrink-0 text-warning" aria-hidden="true" />
		<div class="min-w-0 flex-1 space-y-0.5 text-muted-foreground">
			{#each activityStore.environmentFailures as failure (failure.environmentId)}
				<p>{m.activity_environment_load_failed({ environment: failure.environmentName })}</p>
			{/each}
		</div>
		<button
			type="button"
			onclick={() => activityStore.retryStream()}
			class="shrink-0 cursor-pointer font-medium text-primary hover:underline"
		>
			{m.common_retry()}
		</button>
	</div>
{/if}

{#if activityStore.loading && activityStore.activities.length === 0}
	<div class="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-12 text-center">
		<ActivityIcon class="size-6 animate-pulse text-muted-foreground" aria-hidden="true" />
		<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
	</div>
{:else if activityStore.activities.length === 0}
	<div class="flex flex-1 flex-col items-center justify-center gap-1.5 px-6 py-12 text-center">
		<ActivityIcon class="mb-1 size-7 text-muted-foreground/50" aria-hidden="true" />
		<h3 class="text-sm font-semibold">{m.activity_empty_title()}</h3>
		<p class="max-w-60 text-xs text-muted-foreground">{m.activity_empty_description()}</p>
	</div>
{:else if hasNoResults}
	<div class="flex flex-1 flex-col items-center justify-center gap-1.5 px-6 py-12 text-center">
		<SearchIcon class="mb-1 size-7 text-muted-foreground/50" aria-hidden="true" />
		<h3 class="text-sm font-semibold">{m.activity_no_results_title()}</h3>
		<p class="max-w-60 text-xs text-muted-foreground">{m.activity_no_results_description()}</p>
		<button
			type="button"
			onclick={() => activityStore.clearFilters()}
			class="mt-1 cursor-pointer text-xs font-medium text-primary hover:underline"
		>
			{m.common_clear_filters()}
		</button>
	</div>
{:else}
	{@render children()}
{/if}

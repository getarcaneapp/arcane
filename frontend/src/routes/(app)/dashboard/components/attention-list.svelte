<script lang="ts">
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import SegmentToggle from '#lib/components/segment-toggle.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { IsTablet } from '#lib/hooks/is-tablet.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import { sortAttentionItems, type AttentionItem, type AttentionSeverity, type AttentionSort } from '../overview.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	let {
		items,
		loading,
		failed,
		onAction
	}: {
		items: AttentionItem[];
		loading: boolean;
		failed: boolean;
		onAction: (item: AttentionItem) => void;
	} = $props();

	const isTablet = new IsTablet();
	let sort = $state<AttentionSort>('impact');
	let expanded = $state(false);

	const sortedItems = $derived(sortAttentionItems(items, sort));
	const previewLimit = $derived(isTablet.current ? 2 : 5);
	const visibleItems = $derived(expanded ? sortedItems : sortedItems.slice(0, previewLimit));

	const severityDotClass: Record<AttentionSeverity, string> = {
		critical: 'bg-destructive',
		warning: 'bg-warning',
		info: 'bg-info'
	};
</script>

<DashboardPanel title={m.needs_attention()}>
	{#snippet actions()}
		<SegmentToggle
			label={m.needs_attention()}
			options={[
				{ value: 'impact', label: m.impact() },
				{ value: 'newest', label: m.newest() }
			]}
			current={sort}
			onSelect={(value) => (sort = value === 'newest' ? 'newest' : 'impact')}
		/>
	{/snippet}
	{#if loading && items.length === 0}
		<div class="space-y-3 py-2">
			<Skeleton class="h-4 w-3/4" />
			<Skeleton class="h-4 w-1/2" />
		</div>
	{:else if visibleItems.length === 0}
		<p class="py-2 text-sm text-muted-foreground">{failed ? m.common_unavailable() : m.no_issues()}</p>
	{:else}
		<ul class="divide-y divide-border/40">
			{#each visibleItems as item (item.key)}
				<li class="-mx-2 grid grid-cols-icon-content-action items-start gap-3 rounded-md hover-glow px-2 py-2.5">
					<span class={cn('mt-1.5 size-2 shrink-0 rounded-full', severityDotClass[item.severity])}></span>
					<span class="min-w-0 space-y-1">
						<span class="flex min-w-0 items-center gap-1.5">
							<span class="line-clamp-2 text-sm font-medium wrap-break-word sm:line-clamp-1" title={item.fullId}
								>{item.title}</span
							>
							{#if item.count && item.count > 1}
								<Badge variant="gray" size="xs">{m.times_count({ count: item.count })}</Badge>
							{/if}
						</span>
						<EnvironmentCell value={item.environmentId} />
						{#if item.detail}
							<span class="block text-xs wrap-break-word whitespace-normal text-muted-foreground sm:truncate">{item.detail}</span>
						{/if}
					</span>
					{#if item.target.kind === 'link'}
						<a href={item.target.href} class="touch-hit relative text-sm font-medium text-primary-text hover:underline"
							>{item.actionLabel}</a
						>
					{:else}
						<button
							type="button"
							class="touch-hit relative text-sm font-medium text-primary-text hover:underline"
							onclick={() => onAction(item)}
						>
							{item.actionLabel}
						</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
	{#snippet footer()}
		{#if sortedItems.length > previewLimit}
			<button
				type="button"
				class="text-xs font-medium text-primary-text hover:underline"
				aria-expanded={expanded}
				onclick={() => (expanded = !expanded)}
			>
				{expanded ? m.show_less() : m.show_all_attention_items({ count: sortedItems.length })}
			</button>
		{/if}
	{/snippet}
</DashboardPanel>

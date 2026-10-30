<script lang="ts">
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import SegmentToggle from '#lib/components/segment-toggle.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import { sortAttentionItems, type AttentionItem, type AttentionSeverity, type AttentionSort } from '../overview.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	const ATTENTION_LIMIT = 5;

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

	let sort = $state<AttentionSort>('impact');

	const sortedItems = $derived(sortAttentionItems(items, sort).slice(0, ATTENTION_LIMIT));

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
	{:else if sortedItems.length === 0}
		<p class="py-2 text-sm text-muted-foreground">{failed ? m.common_unavailable() : m.no_issues()}</p>
	{:else}
		<ul class="divide-y divide-border/40">
			{#each sortedItems as item (item.key)}
				<li
					class="-mx-2 grid grid-cols-icon-content-action items-start gap-3 rounded-md px-2 py-2.5 transition-colors hover:bg-muted/40"
				>
					<span class={cn('mt-1.5 size-2 shrink-0 rounded-full', severityDotClass[item.severity])}></span>
					<span class="min-w-0 space-y-1">
						<span class="flex min-w-0 items-center gap-1.5">
							<span class="truncate text-sm font-medium">{item.title}</span>
							{#if item.count && item.count > 1}
								<Badge variant="gray" size="xs">{m.times_count({ count: item.count })}</Badge>
							{/if}
						</span>
						<EnvironmentCell value={item.environmentId} />
						{#if item.detail}
							<span class="block truncate text-xs text-muted-foreground">{item.detail}</span>
						{/if}
					</span>
					{#if item.target.kind === 'link'}
						<a href={item.target.href} class="text-sm font-medium text-primary hover:underline">{item.actionLabel}</a>
					{:else}
						<button type="button" class="text-sm font-medium text-primary hover:underline" onclick={() => onAction(item)}>
							{item.actionLabel}
						</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</DashboardPanel>

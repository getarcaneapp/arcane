<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import type { ArcaneRow, ArcaneSvelteTable } from './table-features';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import Skeleton from '$lib/components/ui/skeleton/skeleton.svelte';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import { FolderXIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import type { Snippet, Component } from 'svelte';
	import { getTableRowsForItems, shouldIgnoreTableRowClick, type GroupedData } from './arcane-table.types.svelte';
	import { slide } from 'svelte/transition';

	void slide;

	let {
		table,
		mobileCard,
		mobileFieldVisibility,
		groupedRows = null,
		groupIcon,
		unstyled = false,
		expandedRowContent,
		expandedRows,
		onToggleRowExpanded,
		loading = false
	}: {
		table: ArcaneSvelteTable<TData>;
		mobileCard: Snippet<[{ row: ArcaneRow<TData>; item: TData; mobileFieldVisibility: Record<string, boolean> }]>;
		mobileFieldVisibility: Record<string, boolean>;
		groupedRows?: GroupedData<TData>[] | null;
		groupIcon?: (groupName: string) => Component;
		unstyled?: boolean;
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
		/** First-load flag — when set and there's no data, render skeleton cards. */
		loading?: boolean;
	} = $props();

	const hasExpand = $derived(!!expandedRowContent);

	function handleRowClick(event: MouseEvent, rowId: string) {
		if (shouldIgnoreTableRowClick(event)) return;
		if (hasExpand) onToggleRowExpanded?.(rowId);
	}

	// Check if we should render grouped view
	const isGrouped = $derived(groupedRows !== null && groupedRows.length > 0);
</script>

{#snippet mobileSkeleton()}
	{#each Array.from({ length: 6 }, (_, i) => i) as r (r)}
		<div class="px-3 py-2.5">
			<div class="flex items-center gap-3">
				<Skeleton class="size-9 rounded-md" />
				<div class="flex-1 space-y-1.5">
					<Skeleton class="h-4 w-1/2" />
					<Skeleton class="h-3 w-1/3" />
				</div>
			</div>
		</div>
	{/each}
{/snippet}

<div class="divide-border/30 divide-y">
	{#if isGrouped && groupedRows}
		<div class="space-y-4 py-2">
			{#each groupedRows as group (group.groupName)}
				{@const groupRows = getTableRowsForItems(table, group.items)}
				{@const IconComponent = groupIcon?.(group.groupName)}

				<DropdownCard
					id={`mobile-group-${group.groupName}`}
					title={group.groupName}
					description={`${group.items.length} ${group.items.length === 1 ? 'item' : 'items'}`}
					icon={IconComponent}
				>
					<div class="divide-border/30 divide-y">
						{#each groupRows as row (row.id)}
							{@const rowId = row.original.id}
							{@const isExpanded = expandedRows?.has(rowId) ?? false}
							<!-- svelte-ignore a11y_click_events_have_key_events -->
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<div class={cn(hasExpand && 'cursor-pointer')} onclick={(e) => handleRowClick(e, rowId)}>
								{@render mobileCard({ row, item: row.original, mobileFieldVisibility })}
							</div>
							{#if hasExpand && isExpanded && expandedRowContent}
								<div class="bg-muted/30 px-4 py-3" transition:slide={{ duration: 200 }}>
									{@render expandedRowContent({ row, item: row.original })}
								</div>
							{/if}
						{:else}
							<div class="text-muted-foreground flex h-24 items-center justify-center text-center">
								{m.common_no_results_found()}
							</div>
						{/each}
					</div>
				</DropdownCard>
			{/each}
		</div>

		{#if groupedRows.length === 0}
			<div class="p-4">
				<Empty.Root
					class={cn('min-h-48 rounded-xl py-12', unstyled ? 'border-transparent bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
					role="status"
					aria-live="polite"
				>
					<Empty.Header>
						<Empty.Media variant="icon">
							<FolderXIcon class="text-muted-foreground/60 size-10" />
						</Empty.Media>
						<Empty.Title class="text-lg font-semibold">{m.common_no_results_found()}</Empty.Title>
						<Empty.Description class="text-muted-foreground text-sm">{m.common_no_results_hint()}</Empty.Description>
					</Empty.Header>
				</Empty.Root>
			</div>
		{/if}
	{:else if loading && table.getRowModel().rows.length === 0}
		{@render mobileSkeleton()}
	{:else}
		<!-- Non-grouped view (original behavior) -->
		{#each table.getRowModel().rows as row (row.id)}
			{@const rowId = (row.original as any).id}
			{@const isExpanded = expandedRows?.has(rowId) ?? false}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div class={cn(hasExpand && 'cursor-pointer')} onclick={(e) => handleRowClick(e, rowId)}>
				{@render mobileCard({ row, item: row.original, mobileFieldVisibility })}
			</div>
			{#if hasExpand && isExpanded && expandedRowContent}
				<div class="bg-muted/30 px-4 py-3" transition:slide={{ duration: 200 }}>
					{@render expandedRowContent({ row, item: row.original })}
				</div>
			{/if}
		{:else}
			<div class="p-4">
				<Empty.Root
					class={cn('min-h-48 rounded-xl py-12', unstyled ? 'border-transparent bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
					role="status"
					aria-live="polite"
				>
					<Empty.Header>
						<Empty.Media variant="icon">
							<FolderXIcon class="text-muted-foreground/60 size-10" />
						</Empty.Media>
						<Empty.Title class="text-lg font-semibold">{m.common_no_results_found()}</Empty.Title>
						<Empty.Description class="text-muted-foreground text-sm">{m.common_no_results_hint()}</Empty.Description>
					</Empty.Header>
				</Empty.Root>
			</div>
		{/each}
	{/if}
</div>

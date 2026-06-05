<script lang="ts">
	import { type Table as TableType, type Row } from '@tanstack/table-core';
	import * as Empty from '$lib/components/ui/empty/index.js';
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
		onToggleRowExpanded
	}: {
		table: TableType<any>;
		mobileCard: Snippet<[{ row: any; item: any; mobileFieldVisibility: Record<string, boolean> }]>;
		mobileFieldVisibility: Record<string, boolean>;
		groupedRows?: GroupedData<any>[] | null;
		groupIcon?: (groupName: string) => Component;
		unstyled?: boolean;
		expandedRowContent?: Snippet<[{ row: Row<any>; item: any }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
	} = $props();

	const hasExpand = $derived(!!expandedRowContent);

	function handleRowClick(event: MouseEvent, rowId: string) {
		if (shouldIgnoreTableRowClick(event)) return;
		if (hasExpand) onToggleRowExpanded?.(rowId);
	}

	// Check if we should render grouped view
	const isGrouped = $derived(groupedRows !== null && groupedRows.length > 0);
</script>

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
							{@const rowId = (row.original as any).id}
							{@const isExpanded = expandedRows?.has(rowId) ?? false}
							<!-- svelte-ignore a11y_click_events_have_key_events -->
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<div class={cn(hasExpand && 'cursor-pointer')} onclick={(e) => handleRowClick(e, rowId)}>
								{@render mobileCard({ row, item: row.original as any, mobileFieldVisibility })}
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
					class={cn('min-h-48 rounded-xl border-0 py-12', unstyled ? 'bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
					role="status"
					aria-live="polite"
				>
					<Empty.Header>
						<Empty.Media variant="icon">
							<FolderXIcon class="text-muted-foreground/40 size-10" />
						</Empty.Media>
						<Empty.Title class="text-base font-medium">{m.common_no_results_found()}</Empty.Title>
					</Empty.Header>
				</Empty.Root>
			</div>
		{/if}
	{:else}
		<!-- Non-grouped view (original behavior) -->
		{#each table.getRowModel().rows as row (row.id)}
			{@const rowId = (row.original as any).id}
			{@const isExpanded = expandedRows?.has(rowId) ?? false}
			<!-- svelte-ignore a11y_click_events_have_key_events -->
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div class={cn(hasExpand && 'cursor-pointer')} onclick={(e) => handleRowClick(e, rowId)}>
				{@render mobileCard({ row, item: row.original as any, mobileFieldVisibility })}
			</div>
			{#if hasExpand && isExpanded && expandedRowContent}
				<div class="bg-muted/30 px-4 py-3" transition:slide={{ duration: 200 }}>
					{@render expandedRowContent({ row, item: row.original })}
				</div>
			{/if}
		{:else}
			<div class="p-4">
				<Empty.Root
					class={cn('min-h-48 rounded-xl border-0 py-12', unstyled ? 'bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
					role="status"
					aria-live="polite"
				>
					<Empty.Header>
						<Empty.Media variant="icon">
							<FolderXIcon class="text-muted-foreground/40 size-10" />
						</Empty.Media>
						<Empty.Title class="text-base font-medium">{m.common_no_results_found()}</Empty.Title>
					</Empty.Header>
				</Empty.Root>
			</div>
		{/each}
	{/if}
</div>

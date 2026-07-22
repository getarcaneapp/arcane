<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import type { ArcaneCell, ArcaneHeader, ArcaneRow, ArcaneSvelteTable } from './table-features';
	import { FlexRender as FlexRenderBase } from '@tanstack/svelte-table';
	import { createVirtualizer } from './virtualizer.svelte';
	import Skeleton from '$lib/components/ui/skeleton/skeleton.svelte';
	import * as Table from '$lib/components/ui/table/index.js';
	import { ArrowRightIcon, ArrowDownIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import {
		getTableRowsForItems,
		shouldIgnoreTableRowClick,
		type ColumnWidth,
		type ColumnAlign,
		type GroupedData,
		type GroupSelectionState
	} from './arcane-table.types.svelte';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import TableEmpty from './table-empty.svelte';
	import type { Component, Snippet } from 'svelte';
	import type { Attachment } from 'svelte/attachments';
	import { slide } from 'svelte/transition';

	void slide;

	let {
		table,
		selectedIds,
		columnsCount,
		groupedRows = null,
		groupIcon,
		groupCollapsedState = {},
		selectionDisabled = false,
		onGroupToggle,
		getGroupSelectionState,
		onToggleGroupSelection,
		onToggleRowSelection,
		unstyled = false,
		expandedRowContent,
		expandedRows,
		onToggleRowExpanded,
		scrollElement,
		loading = false
	}: {
		table: ArcaneSvelteTable<TData>;
		selectedIds: string[];
		columnsCount: number;
		groupedRows?: GroupedData<TData>[] | null;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		selectionDisabled?: boolean;
		onGroupToggle?: (groupName: string) => void;
		getGroupSelectionState?: (groupItems: TData[]) => GroupSelectionState;
		onToggleGroupSelection?: (groupItems: TData[]) => void;
		onToggleRowSelection?: (id: string, selected: boolean) => void;
		unstyled?: boolean;
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
		/** The scrollable ancestor, supplied by the wrapper, used to virtualize long flat lists. */
		scrollElement?: HTMLElement;
		/** First-load flag — when set and there's no data, render skeleton rows. */
		loading?: boolean;
	} = $props();

	const hasExpand = $derived(!!expandedRowContent);

	// FlexRender's generics can't be inferred from its union-shaped props, so unaided it
	// resolves to the broad `Cell<TableFeatures, …>` (which includes feature APIs our cells
	// don't carry). Pin it to the Arcane feature set instead.
	const FlexRender = FlexRenderBase as unknown as Component<{ cell: ArcaneCell<TData> } | { header: ArcaneHeader<TData> }>;

	// Get column width class from meta
	function getWidthClass(width?: ColumnWidth): string {
		if (!width || width === 'auto') return '';
		if (width === 'min') return 'w-0';
		if (width === 'max') return 'w-full';
		if (typeof width === 'number') return `w-[${width}px]`;
		return '';
	}

	// Get column alignment class from meta
	function getAlignClass(align?: ColumnAlign): string {
		if (!align || align === 'left') return '';
		if (align === 'center') return 'text-center';
		if (align === 'right') return 'text-right';
		return '';
	}

	// Narrow, transparent select cell so the row's hover/selected highlight shows through it
	// uniformly (it carried an opaque background before, which broke the highlight at the edge).
	const selectCellClasses = 'w-0 pr-4!';

	function handleRowClick(event: MouseEvent, rowId: string) {
		if (shouldIgnoreTableRowClick(event)) return;
		if (hasExpand) {
			onToggleRowExpanded?.(rowId);
			return;
		}
		if (selectionDisabled) return;
		const isSelected = (selectedIds ?? []).includes(rowId);
		onToggleRowSelection?.(rowId, !isSelected);
	}

	// Get cell classes based on column metadata
	function getCellClasses(cell: ArcaneCell<TData>, isGrouped: boolean, isFirstCell: boolean): string {
		const meta = cell.column.columnDef.meta;
		return cn(
			cell.column.id === 'select' && selectCellClasses,
			cell.column.id === 'actions' && actionsCellClasses,
			getWidthClass(meta?.width),
			getAlignClass(meta?.align),
			meta?.truncate && 'max-w-0 truncate',
			isGrouped && isFirstCell && cell.column.id !== 'select' && 'pl-10'
		);
	}

	// Get rows for a specific group from the table model
	const isGrouped = $derived(groupedRows !== null && groupedRows.length > 0);

	// --- Row virtualization ---------------------------------------------------------------------
	// Only the flat (non-grouped, non-expandable) path is virtualized, and only past a threshold —
	// the case that matters is the "All" page size (TABLE_PAGE_SIZE_ALL), where the server returns
	// the full unpaginated set. Normal paginated pages (<= 100 rows) render plainly. Grouped and
	// expandable layouts keep their existing, proven rendering.
	const VIRTUALIZE_THRESHOLD = 100;
	// Rows are strictly single-line (Table.Cell is `whitespace-nowrap`) so they all share one height.
	// We virtualize with a fixed row height instead of measuring every row: dynamic per-row measurement
	// made the "All" view flicker — with an estimate that differed from the real height, each row
	// re-measured as it scrolled in and nudged the rows already on screen. Calibrate once from the first
	// rendered row, after which every offset is exact and stable.
	const ROW_ESTIMATE_PX = 44;
	let measuredRowHeight = $state<number | null>(null);
	const flatRows = $derived(table.getRowModel().rows);
	const shouldVirtualize = $derived(!isGrouped && !hasExpand && !!scrollElement && flatRows.length > VIRTUALIZE_THRESHOLD);

	function calibrateRowHeight(node: HTMLTableRowElement) {
		if (measuredRowHeight !== null) return;
		const h = node.getBoundingClientRect().height;
		if (h > 0) measuredRowHeight = h;
	}

	// Row actions are a real pinned column: sticky to the row's right edge with its own reserved
	// width, so the floating button never overlaps data columns and survives horizontal scroll.
	// The gutter must stay opaque to mask content scrolling beneath it, so it can't bg-inherit the
	// row's translucent tints (they'd stack with the row's own paint into a darker block). Instead it
	// mirrors the row states from ui/table table-row.svelte as pre-composited opaque colors. Under
	// the virtualized table-fixed layout an auto-sized (w-0 + nowrap) column would collapse, so it
	// gets an explicit width there instead.
	const actionsCellClasses = $derived(
		cn(
			'sticky right-0 z-[var(--arcane-z-sticky)] p-0 whitespace-nowrap',
			shouldVirtualize ? 'w-24' : 'w-0',
			'bg-background',
			'group-hover/row:bg-[color-mix(in_oklab,var(--color-primary)_6%,var(--color-background))]',
			'group-data-[state=selected]/row:bg-[color-mix(in_oklab,var(--color-primary)_12%,var(--color-background))]',
			'group-data-[expanded]/row:bg-[color-mix(in_oklab,var(--color-primary)_15%,var(--color-background))]'
		)
	);

	// Runes can't be created conditionally, so the virtualizer always exists but is `enabled` only
	// when we actually virtualize; disabled, it stays cheap and reports an empty window.
	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLTableRowElement>(() => {
		const rowSize = measuredRowHeight ?? ROW_ESTIMATE_PX;
		return {
			count: flatRows.length,
			getScrollElement: () => scrollElement ?? null,
			estimateSize: () => rowSize,
			overscan: 10,
			getItemKey: (index) => flatRows[index]?.id ?? index,
			enabled: shouldVirtualize
		};
	});
</script>

{#snippet cellContent(cell: ArcaneCell<TData>)}
	{#if cell.column.id === 'actions'}
		<!-- Pinned row actions: a floating chip at the row's end, always present in its own gutter. -->
		<div class="flex items-center justify-end py-1 pr-3 pl-2" data-row-select-ignore>
			<div
				class="flex items-center gap-0.5 rounded-full border border-border/50 bg-card/90 p-0.5 shadow-sm backdrop-blur-sm transition-all duration-150 group-hover/row:border-border group-hover/row:shadow-md"
			>
				<FlexRender {cell} />
			</div>
		</div>
	{:else}
		<FlexRender {cell} />
	{/if}
{/snippet}

{#snippet dataRow(row: ArcaneRow<TData>, isGroupedRow: boolean, measureRow?: Attachment<HTMLTableRowElement>)}
	{@const rowId = row.original.id}
	{@const isExpanded = expandedRows?.has(rowId) ?? false}
	<Table.Row
		{@attach measureRow}
		data-state={(selectedIds ?? []).includes(rowId) && 'selected'}
		data-expanded={isExpanded ? true : undefined}
		onclick={(event) => handleRowClick(event, rowId)}
		class={cn(hasExpand && 'cursor-pointer', isExpanded && 'bg-primary/15')}
	>
		{#if hasExpand}
			<Table.Cell class="w-8 px-2" data-row-select-ignore>
				<button
					class="flex items-center justify-center text-muted-foreground transition-transform duration-200 hover:text-foreground"
					class:rotate-90={isExpanded}
					onclick={(e) => {
						e.stopPropagation();
						onToggleRowExpanded?.(rowId);
					}}
					aria-label={isExpanded ? 'Collapse row' : 'Expand row'}
				>
					<ArrowRightIcon class="size-4" />
				</button>
			</Table.Cell>
		{/if}
		{#each row.getVisibleCells() as cell, cellIndex (cell.id)}
			{@const isFirstDataCell = !selectionDisabled ? cellIndex === 1 : cellIndex === 0}
			<Table.Cell class={getCellClasses(cell, isGroupedRow, isFirstDataCell)}>
				{@render cellContent(cell)}
			</Table.Cell>
		{/each}
	</Table.Row>

	{#if hasExpand && isExpanded && expandedRowContent}
		<Table.Row class="bg-primary/10 hover:bg-primary/10">
			<Table.Cell colspan={columnsCount} class="p-0">
				<div transition:slide={{ duration: 200 }}>
					<div class="px-6 py-4">
						{@render expandedRowContent({ row, item: row.original })}
					</div>
				</div>
			</Table.Cell>
		</Table.Row>
	{/if}
{/snippet}

{#snippet emptyState()}
	<Table.Row>
		<Table.Cell colspan={columnsCount} class="h-48">
			<TableEmpty class={cn('rounded-lg py-12', unstyled ? 'bg-transparent' : 'bg-card/30 backdrop-blur-sm')} />
		</Table.Cell>
	</Table.Row>
{/snippet}

{#snippet skeletonRows()}
	{#each Array.from({ length: 8 }, (_, i) => i) as r (r)}
		<Table.Row class="hover:bg-transparent">
			{#each Array.from({ length: columnsCount }, (_, i) => i) as c (c)}
				<Table.Cell>
					<Skeleton class="h-4 w-full max-w-[140px]" />
				</Table.Cell>
			{/each}
		</Table.Row>
	{/each}
{/snippet}

<div
	class={cn(
		'h-full w-full',
		unstyled &&
			'[&_td]:bg-transparent! [&_thead]:bg-transparent! [&_thead]:backdrop-blur-none [&_tr]:border-border/40! [&_tr]:bg-transparent! [&_tr]:hover:bg-transparent! [&_tr:hover_td]:bg-transparent! [&_tr[data-state=selected]]:bg-transparent! [&_tr[data-state=selected]_td]:bg-transparent!'
	)}
>
	<Table.Root class={shouldVirtualize ? 'table-fixed' : undefined}>
		<Table.Header>
			{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
				<Table.Row>
					{#if hasExpand}
						<Table.Head class="w-8 px-2"></Table.Head>
					{/if}
					{#each headerGroup.headers as header (header.id)}
						<Table.Head
							colspan={header.colSpan}
							class={cn(
								header.column.id === 'select' && selectCellClasses,
								header.column.id === 'actions' && cn(actionsCellClasses, 'z-[var(--arcane-z-page-floating)] bg-background')
							)}
						>
							{#if !header.isPlaceholder}
								<FlexRender {header} />
							{/if}
						</Table.Head>
					{/each}
				</Table.Row>
			{/each}
		</Table.Header>
		<Table.Body>
			{#if isGrouped && groupedRows}
				{#each groupedRows as group (group.groupName)}
					{@const isCollapsed = groupCollapsedState[group.groupName] ?? true}
					{@const groupRows = getTableRowsForItems(table, group.items)}
					{@const selectionState = getGroupSelectionState?.(group.items) ?? 'none'}
					{@const hasSelection = selectionState !== 'none'}
					{@const IconComponent = groupIcon?.(group.groupName)}

					<Table.Row
						class={cn(
							'cursor-pointer transition-colors',
							!unstyled && (hasSelection ? 'bg-primary/10 hover:bg-primary/15' : 'bg-background hover:bg-primary/15')
						)}
						onclick={() => onGroupToggle?.(group.groupName)}
					>
						{#if !selectionDisabled}
							<Table.Cell class={selectCellClasses}>
								<TableCheckbox
									checked={selectionState === 'all'}
									indeterminate={selectionState === 'some'}
									onCheckedChange={() => onToggleGroupSelection?.(group.items)}
									onclick={(e: MouseEvent) => e.stopPropagation()}
									aria-label={m.common_select_all()}
								/>
							</Table.Cell>
						{/if}
						<Table.Cell colspan={columnsCount - (selectionDisabled ? 0 : 1)} class="py-3 font-medium">
							<div class="flex items-center gap-2">
								{#if isCollapsed}
									<ArrowRightIcon class="size-4 text-muted-foreground" />
								{:else}
									<ArrowDownIcon class="size-4 text-muted-foreground" />
								{/if}
								{#if IconComponent}
									<IconComponent class="size-4 text-muted-foreground" />
								{/if}
								<span>{group.groupName}</span>
								<span class="text-xs font-normal text-muted-foreground">({group.items.length})</span>
							</div>
						</Table.Cell>
					</Table.Row>

					<!-- Group Items (if not collapsed) -->
					{#if !isCollapsed}
						{#each groupRows as row (row.id)}
							{@render dataRow(row, true)}
						{/each}
					{/if}
				{/each}

				{#if groupedRows.length === 0}
					{@render emptyState()}
				{/if}
			{:else}
				{#if loading && flatRows.length === 0}
					{@render skeletonRows()}
				{:else if flatRows.length === 0}
					{@render emptyState()}
				{:else if shouldVirtualize}
					{@const vItems = rowVirtualizer.virtualItems}
					{@const first = vItems[0]}
					{@const last = vItems[vItems.length - 1]}
					{@const padTop = first ? first.start : 0}
					{@const padBottom = last ? rowVirtualizer.totalSize - last.end : 0}
					{#if padTop > 0}
						<tr aria-hidden="true"><td colspan={columnsCount} class="border-0 p-0" style="height: {padTop}px"></td></tr>
					{/if}
					{#each vItems as vItem (vItem.key)}
						{@const row = flatRows[vItem.index]}
						{#if row}
							{@render dataRow(row, false, calibrateRowHeight)}
						{/if}
					{/each}
					{#if padBottom > 0}
						<tr aria-hidden="true"><td colspan={columnsCount} class="border-0 p-0" style="height: {padBottom}px"></td></tr>
					{/if}
				{:else}
					{#each flatRows as row (row.id)}
						{@render dataRow(row, false)}
					{/each}
				{/if}
			{/if}
		</Table.Body>
	</Table.Root>
</div>

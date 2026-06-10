<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import type { ArcaneCell, ArcaneRow, ArcaneSvelteTable } from './table-features';
	import { FlexRender } from '@tanstack/svelte-table';
	import { createVirtualizer } from './virtualizer.svelte';
	import Skeleton from '$lib/components/ui/skeleton/skeleton.svelte';
	import * as Table from '$lib/components/ui/table/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { FolderXIcon, ArrowRightIcon, ArrowDownIcon } from '$lib/icons';
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
	const ROW_ESTIMATE_PX = 52;
	const flatRows = $derived(table.getRowModel().rows);
	const shouldVirtualize = $derived(!isGrouped && !hasExpand && !!scrollElement && flatRows.length > VIRTUALIZE_THRESHOLD);

	// Row actions live in a host cell that absorbs the table's leftover width, so data columns
	// size to their content and the floating actions button (see cellContent) gets free room at
	// the row's end instead of overlapping the last data column. Under the virtualized layout
	// the table is table-fixed, where a 100%-width column would squash the auto columns to
	// nothing — there the cell stays zero-width and the button floats over the row as before.
	const actionsCellClasses = $derived(shouldVirtualize ? 'relative w-0 p-0 last:pr-0' : 'relative w-full p-0 last:pr-0');

	// Runes can't be created conditionally, so the virtualizer always exists but is `enabled` only
	// when we actually virtualize; disabled, it stays cheap and reports an empty window.
	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLTableRowElement>(() => ({
		count: flatRows.length,
		getScrollElement: () => scrollElement ?? null,
		estimateSize: () => ROW_ESTIMATE_PX,
		overscan: 10,
		getItemKey: (index) => flatRows[index]?.id ?? index,
		enabled: shouldVirtualize
	}));
</script>

{#snippet cellContent(cell: ArcaneCell<TData>)}
	{#if cell.column.id === 'actions'}
		<!-- Row actions float over the row's right edge, revealed on hover/focus (or while the menu is
		     open). pointer-events are off until revealed so the hidden control can't swallow row clicks. -->
		<div
			class="pointer-events-none absolute top-1/2 right-2 z-10 -translate-y-1/2 opacity-0 transition-opacity group-hover/row:pointer-events-auto group-hover/row:opacity-100 group-focus-within/row:pointer-events-auto group-focus-within/row:opacity-100 has-[[data-state=open]]:pointer-events-auto has-[[data-state=open]]:opacity-100"
			data-row-select-ignore
		>
			<FlexRender {cell} />
		</div>
	{:else}
		<FlexRender {cell} />
	{/if}
{/snippet}

{#snippet flatRow(row: ArcaneRow<TData>, measureRow?: Attachment<HTMLTableRowElement>)}
	{@const rowId = row.original.id}
	{@const isExpanded = expandedRows?.has(rowId) ?? false}
	<Table.Row
		{@attach measureRow}
		data-state={(selectedIds ?? []).includes(rowId) && 'selected'}
		onclick={(event) => handleRowClick(event, rowId)}
		class={cn(hasExpand && 'cursor-pointer', isExpanded && 'bg-primary/15')}
	>
		{#if hasExpand}
			<Table.Cell class="w-8 px-2" data-row-select-ignore>
				<button
					class="text-muted-foreground hover:text-foreground flex items-center justify-center transition-transform duration-200"
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
		{#each row.getVisibleCells() as cell (cell.id)}
			<Table.Cell class={getCellClasses(cell, false, false)}>
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
			'[&_tr]:border-border/40! [&_thead]:bg-transparent! [&_thead]:backdrop-blur-none [&_tr]:bg-transparent! [&_tr]:hover:bg-transparent! [&_tr:hover_td]:bg-transparent! [&_tr[data-state=selected]]:bg-transparent! [&_tr[data-state=selected]_td]:bg-transparent!'
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
							class={cn(header.column.id === 'select' && selectCellClasses, header.column.id === 'actions' && actionsCellClasses)}
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
									<ArrowRightIcon class="text-muted-foreground size-4" />
								{:else}
									<ArrowDownIcon class="text-muted-foreground size-4" />
								{/if}
								{#if IconComponent}
									<IconComponent class="text-muted-foreground size-4" />
								{/if}
								<span>{group.groupName}</span>
								<span class="text-muted-foreground text-xs font-normal">({group.items.length})</span>
							</div>
						</Table.Cell>
					</Table.Row>

					<!-- Group Items (if not collapsed) -->
					{#if !isCollapsed}
						{#each groupRows as row (row.id)}
							{@const rowId = row.original.id}
							{@const isExpanded = expandedRows?.has(rowId) ?? false}
							<Table.Row
								data-state={(selectedIds ?? []).includes(rowId) && 'selected'}
								onclick={(event) => handleRowClick(event, rowId)}
								class={cn(hasExpand && 'cursor-pointer', isExpanded && 'bg-primary/15')}
							>
								{#if hasExpand}
									<Table.Cell class="w-8 px-2" data-row-select-ignore>
										<button
											class="text-muted-foreground hover:text-foreground flex items-center justify-center transition-transform duration-200"
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
									<Table.Cell class={getCellClasses(cell, true, isFirstDataCell)}>
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
						{/each}
					{/if}
				{/each}

				{#if groupedRows.length === 0}
					<Table.Row>
						<Table.Cell colspan={columnsCount} class="h-48">
							<Empty.Root
								class={cn('rounded-lg py-12', unstyled ? 'bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
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
						</Table.Cell>
					</Table.Row>
				{/if}
			{:else}
				{#if loading && flatRows.length === 0}
					{@render skeletonRows()}
				{:else if flatRows.length === 0}
					<Table.Row>
						<Table.Cell colspan={columnsCount} class="h-48">
							<Empty.Root
								class={cn('rounded-lg py-12', unstyled ? 'bg-transparent' : 'backdrop-blur-sm bg-card/30')}
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
						</Table.Cell>
					</Table.Row>
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
							{@render flatRow(row, rowVirtualizer.measureElement)}
						{/if}
					{/each}
					{#if padBottom > 0}
						<tr aria-hidden="true"><td colspan={columnsCount} class="border-0 p-0" style="height: {padBottom}px"></td></tr>
					{/if}
				{:else}
					{#each flatRows as row (row.id)}
						{@render flatRow(row)}
					{/each}
				{/if}
			{/if}
		</Table.Body>
	</Table.Root>
</div>

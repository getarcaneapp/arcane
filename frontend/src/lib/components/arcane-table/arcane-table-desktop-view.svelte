<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import { FlexRender as FlexRenderBase } from '@tanstack/svelte-table';
	import { untrack, type Component, type Snippet } from 'svelte';
	import type { Attachment } from 'svelte/attachments';

	import Skeleton from '#lib/components/ui/skeleton/skeleton.svelte';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ArrowRightIcon, ArrowDownIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { TableDisplayEntry } from '#lib/types/table-display.js';
	import { cn } from '#lib/utils.js';

	import { createVirtualizer } from '../ui/virtualizer.svelte';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import {
		shouldIgnoreTableRowClick,
		type GroupedData,
		type GroupSelectionState,
		type SelectionModifiers
	} from './arcane-table.types.svelte';
	import { getTableDisplayEntries } from './arcane-table.utils';
	import type { ArcaneCell, ArcaneFeatures, ArcaneRow, ArcaneSvelteTable } from './table-features';

	let {
		table,
		rowIndex,
		selectedIdSet,
		initialScrollTop = 0,
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
		loading = false,
		empty,
		wrapText = false
	}: {
		table: ArcaneSvelteTable<TData>;
		rowIndex: ReadonlyMap<string, { row: ArcaneRow<TData>; index: number }>;
		selectedIdSet: ReadonlySet<string>;
		initialScrollTop?: number;
		columnsCount: number;
		groupedRows?: GroupedData<TData>[] | null;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		selectionDisabled?: boolean;
		onGroupToggle?: (groupName: string) => void;
		getGroupSelectionState?: (groupItems: TData[]) => GroupSelectionState;
		onToggleGroupSelection?: (groupItems: TData[]) => void;
		onToggleRowSelection?: (id: string, selected: boolean, modifiers?: SelectionModifiers) => void;
		unstyled?: boolean;
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
		/** The scrollable ancestor, supplied by the wrapper, used to virtualize displayed entries. */
		scrollElement?: HTMLElement;
		/** First-load flag — when set and there's no data, render skeleton rows. */
		loading?: boolean;
		/** Renders the empty state; receives an optional wrapper class. */
		empty: Snippet;
		/** Wrap cell content instead of truncating. */
		wrapText?: boolean;
	} = $props();

	const hasExpand = $derived(!!expandedRowContent);

	// FlexRender's generics can't be inferred from its union-shaped props, so unaided it
	// resolves to the broad `Cell<TableFeatures, RowData, …>` and fails invariance against our
	// concrete cells. Pin them with an instantiation expression — no cast involved.
	const FlexRender = FlexRenderBase<ArcaneFeatures, TData, unknown>;

	function handleRowClick(event: MouseEvent, rowId: string) {
		if (shouldIgnoreTableRowClick(event)) return;
		if (hasExpand) {
			onToggleRowExpanded?.(rowId);
			return;
		}
		if (selectionDisabled) return;
		const isSelected = selectedIdSet.has(rowId);
		onToggleRowSelection?.(rowId, !isSelected, { shiftKey: event.shiftKey });
	}

	function handleRowMouseDown(event: MouseEvent) {
		if (event.shiftKey && !hasExpand && !selectionDisabled && !shouldIgnoreTableRowClick(event)) event.preventDefault();
	}

	// Keep small views on the regular table path.
	const VIRTUALIZE_THRESHOLD = 100;
	const ROW_ESTIMATE_PX = 44;
	let measuredRowHeight = $state<number | null>(null);
	let tableElement = $state<HTMLTableElement | null>(null);
	let bodyElement = $state<HTMLTableSectionElement | null>(null);
	let scrollMargin = $state(0);
	const flatRows = $derived(table.getRowModel().rows);
	const entries = $derived(
		getTableDisplayEntries(flatRows, rowIndex, groupedRows, groupCollapsedState, hasExpand ? expandedRows : undefined)
	);
	const shouldVirtualize = $derived(entries.length > VIRTUALIZE_THRESHOLD);
	// Natural column widths (% of the table) measured from an auto-layout pass, then locked for table-fixed.
	let lockedColumns = $state<{ key: string; widths: number[] } | null>(null);
	const columnKey = $derived(
		table
			.getVisibleLeafColumns()
			.map((column) => column.id)
			.join('|') + `:${hasExpand}:${wrapText}`
	);
	const columnWidths = $derived(shouldVirtualize && lockedColumns?.key === columnKey ? lockedColumns.widths : null);
	const getItemKey = $derived.by(() => {
		const displayed = entries;
		return (index: number) => displayed[index]?.key ?? index;
	});

	function measureRow(node: HTMLTableRowElement) {
		return untrack(() => {
			if (node.dataset['entryKind'] === 'row' && measuredRowHeight === null) {
				const height = node.getBoundingClientRect().height;
				if (height > 0) measuredRowHeight = height;
			}
			return rowVirtualizer.measureElement(node);
		});
	}

	$effect(() => {
		const container = scrollElement;
		const tableNode = tableElement;
		const body = bodyElement;
		if (!shouldVirtualize || !container || !tableNode || !body) return;

		const layoutKey = columnKey;
		let width = 0;
		const remeasure = () => {
			const row = body.querySelector<HTMLTableRowElement>('tr[data-entry-kind="row"]');
			const height = row?.getBoundingClientRect().height;
			if (height) measuredRowHeight = height;
			rowVirtualizer.measure();
		};
		const updateLayout = () => {
			const nextWidth = tableNode.getBoundingClientRect().width;
			scrollMargin =
				body.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop - container.clientTop;
			if (nextWidth !== width) {
				width = nextWidth;
				remeasure();
			}
		};
		const measureColumns = () => {
			if (columnWidths || !body.querySelector('tr[data-index]')) return;
			const cells = tableNode.tHead?.rows[0]?.cells;
			const tableWidth = tableNode.getBoundingClientRect().width;
			if (!cells || !tableWidth) return;
			lockedColumns = {
				key: layoutKey,
				widths: Array.from(cells, (cell) => (cell.getBoundingClientRect().width / tableWidth) * 100)
			};
		};
		const fontsChanged = () => {
			remeasure();
			updateLayout();
		};
		const observer = new ResizeObserver(() => {
			measureColumns();
			updateLayout();
		});
		observer.observe(tableNode);
		observer.observe(container);
		if (tableNode.tHead) observer.observe(tableNode.tHead);
		document.fonts.addEventListener('loadingdone', fontsChanged);
		untrack(updateLayout);
		return () => {
			observer.disconnect();
			document.fonts.removeEventListener('loadingdone', fontsChanged);
		};
	});

	// Runes can't be created conditionally, so the virtualizer always exists but is `enabled` only
	// when we actually virtualize; disabled, it stays cheap and reports an empty window.
	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLTableRowElement>(() => {
		return {
			count: entries.length,
			getScrollElement: () => scrollElement ?? null,
			estimateSize: (index) => (entries[index]?.kind === 'detail' ? 160 : (measuredRowHeight ?? ROW_ESTIMATE_PX)),
			overscan: 10,
			getItemKey,
			initialOffset: initialScrollTop,
			scrollMargin,
			enabled: shouldVirtualize && !!scrollElement
		};
	});
	$effect(() => {
		const container = scrollElement;
		const size = rowVirtualizer.totalSize;
		const margin = scrollMargin;
		if (!container || !shouldVirtualize) return;
		const frame = requestAnimationFrame(() => {
			const max = Math.max(0, size + margin - container.clientHeight);
			if (container.scrollTop > max) container.scrollTop = max;
		});
		return () => cancelAnimationFrame(frame);
	});
</script>

{#snippet cellContent(cell: ArcaneCell<TData>)}
	{#if cell.column.id === 'actions'}
		<!-- Pinned row actions: a floating chip at the row's end, always present in its own gutter. -->
		<div class="flex items-center justify-end py-1 pr-3 pl-2" data-row-select-ignore>
			<div
				class="flex items-center gap-0.5 rounded-full border border-border/50 bg-card/90 p-0.5 shadow-sm transition-all duration-150 group-hover/row:border-border group-hover/row:shadow-md"
			>
				<FlexRender {cell} />
			</div>
		</div>
	{:else}
		<FlexRender {cell} />
	{/if}
{/snippet}

{#snippet dataRow(
	row: ArcaneRow<TData>,
	isGroupedRow: boolean,
	rowMeasurement?: Attachment<HTMLTableRowElement>,
	virtualIndex?: number
)}
	{@const rowId = row.original.id}
	{@const isExpanded = expandedRows?.has(rowId) ?? false}
	<Table.Row
		{@attach rowMeasurement}
		data-index={virtualIndex}
		data-entry-kind="row"
		data-state={selectedIdSet.has(rowId) && 'selected'}
		data-expanded={isExpanded ? true : undefined}
		onclick={(event) => handleRowClick(event, rowId)}
		onmousedown={handleRowMouseDown}
		class={cn('isolate', hasExpand && 'cursor-pointer')}
	>
		{#if hasExpand}
			<Table.Cell variant="expander" data-row-select-ignore>
				<button
					class={cn(
						'flex items-center justify-center text-muted-foreground transition-transform duration-200 hover:text-foreground',
						isExpanded && 'rotate-90'
					)}
					onclick={(e) => {
						e.stopPropagation();
						onToggleRowExpanded?.(rowId);
					}}
					aria-label={isExpanded ? m.table_collapse_row() : m.table_expand_row()}
				>
					<ArrowRightIcon class="size-4" />
				</button>
			</Table.Cell>
		{/if}
		{#each row.getVisibleCells() as cell, cellIndex (cell.id)}
			{@const isFirstDataCell = !selectionDisabled ? cellIndex === 1 : cellIndex === 0}
			{@const meta = cell.column.columnDef.meta}
			{@const clip =
				!wrapText && (meta?.truncate || (!!columnWidths && cell.column.id !== 'select' && cell.column.id !== 'actions'))}
			<Table.Cell
				pinned={cell.column.id === 'actions'}
				variant={cell.column.id === 'select' ? 'checkbox' : 'default'}
				indent={isGroupedRow && isFirstDataCell && cell.column.id !== 'select'}
				style={typeof meta?.width === 'number' ? `--col-width: ${meta.width}px` : undefined}
				class={cn(
					cell.column.id === 'actions' && 'w-0',
					meta?.width === 'min' && 'w-0',
					meta?.width === 'max' && 'w-full',
					meta?.align === 'center' && 'text-center',
					meta?.align === 'right' && 'text-right',
					clip && 'max-w-0',
					wrapText && cell.column.id !== 'select' && cell.column.id !== 'actions' && 'break-words whitespace-normal',
					typeof meta?.width === 'number' && 'w-(--col-width)'
				)}
			>
				{#if clip}
					<span class="block min-w-0 truncate">{@render cellContent(cell)}</span>
				{:else}
					{@render cellContent(cell)}
				{/if}
			</Table.Cell>
		{/each}
	</Table.Row>
{/snippet}

{#snippet detailRow(row: ArcaneRow<TData>, rowMeasurement?: Attachment<HTMLTableRowElement>, virtualIndex?: number)}
	{#if expandedRowContent}
		<Table.Row variant="detail" {@attach rowMeasurement} data-index={virtualIndex}>
			<Table.Cell colspan={columnsCount} variant="flush">
				<div class="px-6 py-4">
					{@render expandedRowContent({ row, item: row.original })}
				</div>
			</Table.Cell>
		</Table.Row>
	{/if}
{/snippet}

{#snippet emptyState()}
	<Table.Row>
		<Table.Cell colspan={columnsCount} class="h-48">
			{@render empty()}
		</Table.Cell>
	</Table.Row>
{/snippet}

{#snippet skeletonRows()}
	{#each Array.from({ length: 8 }, (_, i) => i) as r (r)}
		<Table.Row variant="static">
			{#each Array.from({ length: columnsCount }, (_, i) => i) as c (c)}
				<Table.Cell>
					<Skeleton class="h-4 w-full max-w-35" />
				</Table.Cell>
			{/each}
		</Table.Row>
	{/each}
{/snippet}

{#snippet tableHeader()}
	<Table.Header>
		{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
			<Table.Row>
				{#if hasExpand}
					<Table.Head
						variant="expander"
						style={columnWidths ? `--col-width: ${columnWidths[0]}%` : undefined}
						class={columnWidths ? 'w-(--col-width)' : undefined}
					></Table.Head>
				{/if}
				{#each headerGroup.headers as header, i (header.id)}
					{@const meta = header.column.columnDef.meta}
					{@const pxWidth = typeof meta?.width === 'number' ? `${meta.width}px` : undefined}
					{@const colWidth = columnWidths ? `${columnWidths[i + (hasExpand ? 1 : 0)]}%` : pxWidth}
					<Table.Head
						colspan={header.colSpan}
						pinned={header.column.id === 'actions'}
						variant={header.column.id === 'select' ? 'checkbox' : 'default'}
						style={colWidth ? `--col-width: ${colWidth}` : undefined}
						class={cn(
							meta?.width === 'min' && 'w-0',
							meta?.width === 'max' && 'w-full',
							header.column.id === 'actions' && 'w-0',
							colWidth && 'w-(--col-width)'
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
{/snippet}

{#snippet tableGroup(group: GroupedData<TData>, rowMeasurement?: Attachment<HTMLTableRowElement>, virtualIndex?: number)}
	{@const isCollapsed = groupCollapsedState[group.groupName] ?? true}
	{@const selectionState = getGroupSelectionState?.(group.items) ?? 'none'}
	{@const hasSelection = selectionState !== 'none'}
	{@const IconComponent = groupIcon?.(group.groupName)}

	<Table.Row
		{@attach rowMeasurement}
		data-index={virtualIndex}
		variant={unstyled ? 'static' : 'default'}
		data-state={hasSelection ? 'selected' : undefined}
		onclick={() => onGroupToggle?.(group.groupName)}
	>
		{#if !selectionDisabled}
			<Table.Cell variant="checkbox">
				<TableCheckbox
					checked={selectionState === 'all'}
					indeterminate={selectionState === 'some'}
					onCheckedChange={() => onToggleGroupSelection?.(group.items)}
					onclick={(e: MouseEvent) => e.stopPropagation()}
					aria-label={m.common_select_all()}
				/>
			</Table.Cell>
		{/if}
		<Table.Cell colspan={columnsCount - (selectionDisabled ? 0 : 1)}>
			<div class="flex items-center gap-2 font-medium">
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
{/snippet}

{#snippet displayEntry(entry: TableDisplayEntry<TData>, virtualIndex?: number)}
	{@const measurement = virtualIndex !== undefined ? measureRow : undefined}
	{#if entry.kind === 'group'}
		{@render tableGroup(entry.group, measurement, virtualIndex)}
	{:else if entry.kind === 'detail'}
		{@render detailRow(entry.row, measurement, virtualIndex)}
	{:else}
		{@render dataRow(entry.row, entry.grouped, measurement, virtualIndex)}
	{/if}
{/snippet}

{#snippet virtualRows()}
	{@const vItems = rowVirtualizer.virtualItems}
	{@const first = vItems[0]}
	{@const last = vItems[vItems.length - 1]}
	{@const padTop = first ? Math.max(0, first.start - scrollMargin) : 0}
	{@const padBottom = last ? Math.max(0, rowVirtualizer.totalSize - (last.end - scrollMargin)) : rowVirtualizer.totalSize}
	{#if padTop > 0}
		<tr aria-hidden="true"><td colspan={columnsCount} class="h-(--pad) border-0 p-0" style="--pad: {padTop}px"></td></tr>
	{/if}
	{#each vItems as vItem (vItem.key)}
		{@const entry = entries[vItem.index]}
		{#if entry}
			{@render displayEntry(entry, vItem.index)}
		{/if}
	{/each}
	{#if padBottom > 0}
		<tr aria-hidden="true"><td colspan={columnsCount} class="h-(--pad) border-0 p-0" style="--pad: {padBottom}px"></td></tr>
	{/if}
{/snippet}

<div
	class={cn(
		'h-full w-full',
		unstyled &&
			'[&_td]:bg-transparent! [&_thead]:bg-transparent! [&_tr]:border-border/40! [&_tr]:bg-transparent! [&_tr]:hover:bg-transparent! [&_tr:hover_td]:bg-transparent! [&_tr[data-state=selected]]:bg-transparent! [&_tr[data-state=selected]_td]:bg-transparent!'
	)}
>
	<Table.Root bind:ref={tableElement} class={columnWidths ? 'table-fixed' : undefined}>
		{@render tableHeader()}
		<Table.Body bind:ref={bodyElement}>
			{#if loading && entries.length === 0}
				{@render skeletonRows()}
			{:else if entries.length === 0}
				{@render emptyState()}
			{:else if shouldVirtualize}
				{@render virtualRows()}
			{:else}
				{#each entries as entry (entry.key)}
					{@render displayEntry(entry)}
				{/each}
			{/if}
		</Table.Body>
	</Table.Root>
</div>

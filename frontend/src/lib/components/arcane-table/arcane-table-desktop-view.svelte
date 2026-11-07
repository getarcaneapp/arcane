<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import { FlexRender as FlexRenderBase } from '@tanstack/svelte-table';
	import { untrack, type Component, type Snippet } from 'svelte';
	import type { Attachment } from 'svelte/attachments';

	import Skeleton from '#lib/components/ui/skeleton/skeleton.svelte';
	import * as Table from '#lib/components/ui/table/index.js';
	import { ArrowRightIcon, ArrowDownIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { TableDisplayEntry } from '#lib/types/ui/tabledisplay.js';
	import { cn } from '#lib/utils.js';
	import { rowKey } from '#lib/utils/environments.js';

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
		scrollElement?: HTMLElement;
		loading?: boolean;
		empty: Snippet;
		wrapText?: boolean;
	} = $props();

	const tableId = $props.id();
	const hasExpand = $derived(!!expandedRowContent);
	const expanderIndex = $derived(selectionDisabled ? 0 : 1);

	// Pin FlexRender's generics; they can't be inferred from its union-shaped props.
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
	// Widths in px from an auto-layout pass, locked so overflowing tables scroll instead of squashing.
	let lockedColumns = $state<{ key: string; widths: number[] } | null>(null);
	const columnKey = $derived(
		table
			.getVisibleLeafColumns()
			.map((column) => column.id)
			.join('|') + `:${hasExpand}:${wrapText}`
	);
	const columnWidths = $derived(shouldVirtualize && lockedColumns?.key === columnKey ? lockedColumns.widths : null);
	let settledWidths = $state<{ key: string; widths: Record<string, number> }>({ key: '', widths: {} });
	let layoutWidth = $state(0);
	const settleKey = $derived(`${columnKey}:${layoutWidth}`);
	const minWidths = $derived(settledWidths.key === settleKey && flatRows.length > 0 ? settledWidths.widths : {});

	$effect(() => {
		const container = scrollElement;
		if (!container) return;
		const observer = new ResizeObserver(() => {
			layoutWidth = Math.round(container.clientWidth);
		});
		observer.observe(container);
		return () => observer.disconnect();
	});

	$effect(() => {
		const tableNode = tableElement;
		const key = settleKey;
		if (!tableNode || columnWidths || flatRows.length === 0) return;
		const frame = requestAnimationFrame(() => {
			const previous = settledWidths.key === key ? settledWidths.widths : {};
			const next = { ...previous };
			let grew = false;
			for (const cell of tableNode.querySelectorAll<HTMLTableCellElement>('thead th[data-column-id]')) {
				const id = cell.dataset['columnId'] ?? '';
				const width = Math.floor(cell.getBoundingClientRect().width);
				if (width > (next[id] ?? 0)) {
					next[id] = width;
					grew = true;
				}
			}
			if (grew || settledWidths.key !== key) settledWidths = { key, widths: next };
		});
		return () => cancelAnimationFrame(frame);
	});
	const lockedTableWidth = $derived(columnWidths?.reduce((total, width) => total + width, 0));
	const ariaSort = { asc: 'ascending', desc: 'descending' } as const;
	const cellPropsByColumn = $derived(
		new Map(
			table.getVisibleLeafColumns().map((column, index) => {
				const meta = column.columnDef.meta;
				const isControl = column.id === 'select' || column.id === 'actions';
				return [
					column.id,
					{
						pinned: column.id === 'actions',
						variant: column.id === 'select' ? ('checkbox' as const) : ('default' as const),
						firstData: column.id !== 'select' && index === (selectionDisabled ? 0 : 1),
						shrink: column.id === 'actions' || meta?.width === 'min',
						grow: meta?.width === 'max',
						align: meta?.align,
						clip: !wrapText && (!!meta?.truncate || (!!columnWidths && !isControl)),
						wrap: wrapText && !isControl,
						width: typeof meta?.width === 'number' ? meta.width : undefined
					}
				];
			})
		)
	);
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
		let forceRemeasure = false;
		let columnsPending = false;
		let frame = 0;
		const measureColumns = () => {
			if (columnWidths || !body.querySelector('tr[data-index]')) return;
			const cells = tableNode.tHead?.rows[0]?.cells;
			if (!cells?.length) return;
			lockedColumns = {
				key: layoutKey,
				widths: Array.from(cells, (cell) => cell.getBoundingClientRect().width)
			};
		};
		const scheduleLayout = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				if (columnsPending) measureColumns();
				columnsPending = false;
				const nextWidth = tableNode.getBoundingClientRect().width;
				scrollMargin =
					body.getBoundingClientRect().top - container.getBoundingClientRect().top + container.scrollTop - container.clientTop;
				if (nextWidth === width && !forceRemeasure) return;
				width = nextWidth;
				forceRemeasure = false;
				const height = body.querySelector<HTMLTableRowElement>('tr[data-entry-kind="row"]')?.getBoundingClientRect().height;
				if (height) measuredRowHeight = height;
				rowVirtualizer.measure();
			});
		};
		const fontsChanged = () => {
			forceRemeasure = true;
			scheduleLayout();
		};
		const observer = new ResizeObserver(() => {
			columnsPending = true;
			scheduleLayout();
		});
		observer.observe(tableNode);
		observer.observe(container);
		if (tableNode.tHead) observer.observe(tableNode.tHead);
		document.fonts.addEventListener('loadingdone', fontsChanged);
		scheduleLayout();
		return () => {
			cancelAnimationFrame(frame);
			observer.disconnect();
			document.fonts.removeEventListener('loadingdone', fontsChanged);
		};
	});

	// Runes can't be conditional, so the virtualizer always exists and is toggled via `enabled`.
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
		<div class="flex items-center justify-end gap-0.5 py-1 pr-3 pl-2" data-row-select-ignore>
			<FlexRender {cell} />
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
	{@const rowId = row.id}
	{@const isExpanded = expandedRows?.has(rowId) ?? false}
	<Table.Row
		{@attach rowMeasurement}
		data-index={virtualIndex}
		data-entry-kind="row"
		id={isGroupedRow ? `${tableId}-row-${rowId}` : undefined}
		data-state={selectedIdSet.has(rowId) && 'selected'}
		data-expanded={isExpanded ? true : undefined}
		onclick={(event) => handleRowClick(event, rowId)}
		onmousedown={handleRowMouseDown}
		class={hasExpand ? 'cursor-pointer' : undefined}
	>
		{#each row.getVisibleCells() as cell, i (cell.id)}
			{@const props = cellPropsByColumn.get(cell.column.id)}
			{#if hasExpand && i === expanderIndex}
				<Table.Cell variant="expander" data-row-select-ignore>
					<button
						type="button"
						class={cn(
							'flex items-center justify-center text-muted-foreground transition-transform duration-200 hover:text-foreground',
							isExpanded && 'rotate-90'
						)}
						onclick={(e) => {
							e.stopPropagation();
							onToggleRowExpanded?.(rowId);
						}}
						aria-label={isExpanded ? m.table_collapse_row() : m.table_expand_row()}
						aria-expanded={isExpanded}
						aria-controls={isExpanded ? `${tableId}-detail-${rowId}` : undefined}
					>
						<ArrowRightIcon class="size-4" />
					</button>
				</Table.Cell>
			{/if}
			<Table.Cell
				pinned={props?.pinned}
				variant={props?.variant}
				indent={isGroupedRow && !!props?.firstData}
				style={props?.width !== undefined ? `--col-width: ${props.width}px` : undefined}
				class={cn(
					props?.shrink && 'w-0',
					props?.grow && 'w-full',
					props?.align === 'center' && 'text-center',
					props?.align === 'right' && 'text-right',
					props?.clip && 'max-w-0',
					props?.wrap && 'break-words whitespace-normal',
					props?.width !== undefined && 'w-(--col-width)'
				)}
			>
				{#if props?.clip}
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
				<div id="{tableId}-detail-{row.id}" class="px-6 py-4">
					{@render expandedRowContent({ row, item: row.original })}
				</div>
			</Table.Cell>
		</Table.Row>
	{/if}
{/snippet}

{#snippet emptyState()}
	<Table.Row variant="static">
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
				{#each headerGroup.headers as header, i (header.id)}
					{@const meta = header.column.columnDef.meta}
					{@const pxWidth = typeof meta?.width === 'number' ? `${meta.width}px` : undefined}
					{@const colWidth = columnWidths ? `${columnWidths[i + (hasExpand && i >= expanderIndex ? 1 : 0)]}px` : pxWidth}
					{#if hasExpand && i === expanderIndex}
						<Table.Head
							variant="expander"
							style={columnWidths ? `--col-width: ${columnWidths[expanderIndex]}px` : undefined}
							class={columnWidths ? 'w-(--col-width)' : undefined}
						></Table.Head>
					{/if}
					{@const sorted = header.column.getIsSorted()}
					<Table.Head
						colspan={header.colSpan}
						aria-sort={sorted ? ariaSort[sorted] : undefined}
						pinned={header.column.id === 'actions'}
						variant={header.column.id === 'select' ? 'checkbox' : 'default'}
						data-column-id={meta?.width === 'max' || header.column.id === 'select' || header.column.id === 'actions'
							? undefined
							: header.column.id}
						style={colWidth
							? `--col-width: ${colWidth}`
							: minWidths[header.column.id]
								? `--col-min: ${minWidths[header.column.id]}px`
								: undefined}
						class={cn(
							meta?.width === 'min' && 'w-0',
							meta?.width === 'max' && 'w-full',
							header.column.id === 'actions' && 'w-0',
							colWidth && 'w-(--col-width)',
							!colWidth && minWidths[header.column.id] && 'min-w-(--col-min)'
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
	>
		{#if !selectionDisabled}
			<Table.Cell variant="checkbox">
				<TableCheckbox
					checked={selectionState === 'all'}
					indeterminate={selectionState === 'some'}
					onCheckedChange={() => onToggleGroupSelection?.(group.items)}
					aria-label={m.common_select_all()}
				/>
			</Table.Cell>
		{/if}
		<Table.Cell colspan={columnsCount - (selectionDisabled ? 0 : 1)}>
			<button
				type="button"
				class="flex w-full cursor-pointer items-center gap-2 text-left font-medium"
				aria-expanded={!isCollapsed}
				aria-controls={isCollapsed ? undefined : group.items.map((item) => `${tableId}-row-${rowKey(item)}`).join(' ')}
				onclick={() => onGroupToggle?.(group.groupName)}
			>
				{#if isCollapsed}
					<ArrowRightIcon class="size-4 text-muted-foreground" aria-hidden="true" />
				{:else}
					<ArrowDownIcon class="size-4 text-muted-foreground" aria-hidden="true" />
				{/if}
				{#if IconComponent}
					<IconComponent class="size-4 text-muted-foreground" aria-hidden="true" />
				{/if}
				<span>{group.groupName}</span>
				<span class="text-xs font-normal text-muted-foreground" aria-hidden="true">({group.items.length})</span>
			</button>
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

<div class={cn('h-full w-full', unstyled && '[&_td]:bg-transparent!')}>
	<Table.Root
		bind:ref={tableElement}
		class={cn(columnWidths && 'table-fixed', lockedTableWidth && 'min-w-(--table-width)')}
		style={lockedTableWidth ? `--table-width: ${lockedTableWidth}px` : undefined}
	>
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

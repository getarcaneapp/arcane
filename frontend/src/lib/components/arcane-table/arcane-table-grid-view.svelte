<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import { FlexRender as FlexRenderBase } from '@tanstack/svelte-table';
	import type { Snippet, Component } from 'svelte';

	import Skeleton from '#lib/components/ui/skeleton/skeleton.svelte';
	import { createVirtualizer } from '#lib/components/ui/virtualizer.svelte.js';
	import VirtualRows from '#lib/components/virtual-rows.svelte';
	import { ArrowDownIcon, ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import TableCheckbox from './arcane-table-checkbox.svelte';
	import {
		shouldIgnoreTableRowClick,
		type GroupedData,
		type GroupSelectionState,
		type SelectionModifiers
	} from './arcane-table.types.svelte';
	import { getCardSlot, getTableDisplayEntries } from './arcane-table.utils';
	import ResourceCardField from './cards/resource-card-field.svelte';
	import ResourceCardMeta from './cards/resource-card-meta.svelte';
	import ResourceCard from './cards/resource-card.svelte';
	import type { ArcaneCell, ArcaneFeatures, ArcaneRow, ArcaneSvelteTable } from './table-features';

	type GridEntry =
		| { kind: 'group'; key: string; group: GroupedData<TData> }
		| { kind: 'cards'; key: string; rows: ArcaneRow<TData>[] };

	const MIN_CARD_WIDTH = 320;

	let {
		table,
		rowIndex,
		selectedIdSet,
		selectionDisabled = false,
		onToggleRowSelection,
		groupedRows = null,
		groupIcon,
		groupCollapsedState = {},
		onGroupToggle,
		getGroupSelectionState,
		onToggleGroupSelection,
		expandedRowContent,
		expandedRows,
		onToggleRowExpanded,
		scrollElement,
		initialScrollTop = 0,
		loading = false,
		empty
	}: {
		table: ArcaneSvelteTable<TData>;
		rowIndex: ReadonlyMap<string, { row: ArcaneRow<TData>; index: number }>;
		selectedIdSet: ReadonlySet<string>;
		selectionDisabled?: boolean;
		onToggleRowSelection: (id: string, selected: boolean, modifiers?: SelectionModifiers) => void;
		groupedRows?: GroupedData<TData>[] | null;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		onGroupToggle?: (groupName: string) => void;
		getGroupSelectionState?: (groupItems: TData[]) => GroupSelectionState;
		onToggleGroupSelection?: (groupItems: TData[]) => void;
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
		scrollElement?: HTMLElement;
		initialScrollTop?: number;
		/** First-load flag — when set and there's no data, render skeleton cards. */
		loading?: boolean;
		empty: Snippet;
	} = $props();

	const hasExpand = $derived(!!expandedRowContent);
	const hasSelection = $derived(selectedIdSet.size > 0);
	// Pin FlexRender's generics, as in the desktop view.
	const FlexRender = FlexRenderBase<ArcaneFeatures, TData, unknown>;
	const CONTROL_COLUMNS = new Set(['select', 'actions']);

	// Cards render the list's own cells: the first visible data column is the title, the rest go to their card slot.
	function splitCells(row: ArcaneRow<TData>) {
		const visible = row.getVisibleCells();
		const cells = visible.filter((cell) => !CONTROL_COLUMNS.has(cell.column.id));
		const slots = {
			title: cells[0] as ArcaneCell<TData> | undefined,
			actions: visible.find((cell) => cell.column.id === 'actions'),
			badges: [] as ArcaneCell<TData>[],
			fields: [] as { cell: ArcaneCell<TData>; wide: boolean }[],
			footer: [] as ArcaneCell<TData>[]
		};
		for (const cell of cells.slice(1)) {
			const slot = getCardSlot(cell.column.id, cell.column.columnDef.meta?.card);
			if (slot === 'badge') slots.badges.push(cell);
			else if (slot === 'footer') slots.footer.push(cell);
			else if (slot !== 'hidden') slots.fields.push({ cell, wide: slot === 'wide' });
		}
		return slots;
	}

	function cellLabel(cell: ArcaneCell<TData>): string {
		return cell.column.columnDef.meta?.title ?? cell.column.id;
	}

	let containerWidth = $state(0);
	const fieldCount = $derived(Math.max(0, table.getVisibleLeafColumns().filter((c) => !CONTROL_COLUMNS.has(c.id)).length - 1));
	const columnCount = $derived(Math.max(1, Math.floor(containerWidth / MIN_CARD_WIDTH)));
	const gridColumnsStyle = $derived(`--grid-cols: repeat(${columnCount}, minmax(0, 1fr))`);

	$effect(() => {
		const container = scrollElement;
		if (!container) return;
		containerWidth = container.clientWidth;
		const observer = new ResizeObserver(() => {
			containerWidth = container.clientWidth;
		});
		observer.observe(container);
		return () => observer.disconnect();
	});

	const displayEntries = $derived(getTableDisplayEntries(table.getRowModel().rows, rowIndex, groupedRows, groupCollapsedState));

	// Pack consecutive rows into fixed-width grid lines so each line can be virtualized as one item.
	const entries = $derived.by((): GridEntry[] => {
		const out: GridEntry[] = [];
		let pending: ArcaneRow<TData>[] = [];
		let pendingKey = '';
		const flush = () => {
			if (pending.length === 0) return;
			out.push({ kind: 'cards', key: `cards:${pendingKey}`, rows: pending });
			pending = [];
		};
		for (const entry of displayEntries) {
			if (entry.kind === 'group') {
				flush();
				out.push(entry);
			} else if (entry.kind === 'row') {
				if (pending.length === 0) pendingKey = entry.key;
				pending.push(entry.row);
				if (pending.length === columnCount) flush();
			}
		}
		flush();
		return out;
	});

	const shouldVirtualize = $derived(entries.length > 30);
	const getItemKey = $derived.by(() => {
		const displayed = entries;
		return (index: number) => displayed[index]?.key ?? index;
	});
	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLDivElement>(() => ({
		count: entries.length,
		getScrollElement: () => scrollElement ?? null,
		estimateSize: (index) => (entries[index]?.kind === 'group' ? 48 : 140 + Math.ceil(fieldCount / 2) * 52),
		overscan: 4,
		getItemKey,
		initialOffset: initialScrollTop,
		enabled: shouldVirtualize && !!scrollElement
	}));

	$effect(() => {
		if (!shouldVirtualize) return;
		// Column count and field changes reflow every line, invalidating cached heights.
		void columnCount;
		void fieldCount;
		rowVirtualizer.measure();
	});

	function handleCardClick(event: MouseEvent, rowId: string) {
		if (shouldIgnoreTableRowClick(event)) return;
		if (hasExpand) onToggleRowExpanded?.(rowId);
	}
</script>

{#snippet gridSkeleton()}
	<div class="grid grid-cols-(--grid-cols) gap-3 p-3" style={gridColumnsStyle}>
		{#each Array.from({ length: columnCount * 2 }, (_, i) => i) as r (r)}
			<div class="space-y-3 rounded-xl border border-border/40 p-4">
				<div class="flex items-center gap-3">
					<Skeleton class="size-10" />
					<div class="flex-1 space-y-1.5">
						<Skeleton class="h-4 w-1/2" />
						<Skeleton class="h-3 w-1/3" />
					</div>
				</div>
				<Skeleton class="h-3 w-3/4" />
				<Skeleton class="h-3 w-2/3" />
			</div>
		{/each}
	</div>
{/snippet}

{#snippet groupHeader(group: GroupedData<TData>)}
	{@const isCollapsed = groupCollapsedState[group.groupName] ?? true}
	{@const selectionState = getGroupSelectionState?.(group.items) ?? 'none'}
	{@const IconComponent = groupIcon?.(group.groupName)}
	<div
		class="mx-3 mt-3 mb-1.5 flex cursor-pointer items-center gap-2 rounded-lg border border-border/40 bg-muted/30 px-3 py-2 text-sm font-medium select-none hover:bg-muted/50"
		role="button"
		tabindex={0}
		aria-expanded={!isCollapsed}
		onclick={() => onGroupToggle?.(group.groupName)}
		onkeydown={(event) => {
			if (event.target !== event.currentTarget) return;
			if (event.key === 'Enter' || event.key === ' ') {
				event.preventDefault();
				onGroupToggle?.(group.groupName);
			}
		}}
	>
		{#if !selectionDisabled}
			<TableCheckbox
				checked={selectionState === 'all'}
				indeterminate={selectionState === 'some'}
				onCheckedChange={() => onToggleGroupSelection?.(group.items)}
				onclick={(e: MouseEvent) => e.stopPropagation()}
				aria-label={m.common_select_all()}
			/>
		{/if}
		{#if isCollapsed}
			<ArrowRightIcon class="size-4 text-muted-foreground" />
		{:else}
			<ArrowDownIcon class="size-4 text-muted-foreground" />
		{/if}
		{#if IconComponent}
			<IconComponent class="size-4 text-muted-foreground" />
		{/if}
		<span>{group.groupName}</span>
		<span class="text-xs font-normal text-muted-foreground">{m.table_group_items({ count: group.items.length })}</span>
	</div>
{/snippet}

{#snippet gridCard(row: ArcaneRow<TData>)}
	{@const rowId = row.id}
	{@const isSelected = selectedIdSet.has(rowId)}
	{@const isExpanded = expandedRows?.has(rowId) ?? false}
	{@const cells = splitCells(row)}
	{#snippet cardLeading()}
		<div
			class={cn(
				'flex shrink-0 transition-opacity duration-150 group-hover/resource-card:opacity-100 focus-within:opacity-100',
				isSelected || hasSelection ? 'opacity-100' : 'opacity-0'
			)}
		>
			<TableCheckbox
				checked={isSelected}
				onCheckedChange={(value, modifiers) => onToggleRowSelection(rowId, value, modifiers)}
				aria-label={m.common_select_row()}
			/>
		</div>
	{/snippet}
	{#snippet cardTitle()}
		{#if cells.title}
			<FlexRender cell={cells.title} />
		{/if}
	{/snippet}
	{#snippet cardActions()}
		{#if cells.actions}
			<FlexRender cell={cells.actions} />
		{/if}
	{/snippet}
	{#snippet cardBadges()}
		{#each cells.badges as cell (cell.id)}
			<FlexRender {cell} />
		{/each}
	{/snippet}
	{#snippet cardFields()}
		{#each cells.fields as field (field.cell.id)}
			<ResourceCardField label={cellLabel(field.cell)} wide={field.wide}>
				<FlexRender cell={field.cell} />
			</ResourceCardField>
		{/each}
	{/snippet}
	{#snippet cardFooter()}
		{#each cells.footer as cell (cell.id)}
			<ResourceCardMeta label={cellLabel(cell)}>
				<FlexRender {cell} />
			</ResourceCardMeta>
		{/each}
	{/snippet}
	<ResourceCard
		selected={isSelected}
		onclick={hasExpand ? (e) => handleCardClick(e, rowId) : undefined}
		leading={selectionDisabled ? undefined : cardLeading}
		title={cardTitle}
		actions={cells.actions ? cardActions : undefined}
		badges={cells.badges.length > 0 ? cardBadges : undefined}
		fields={cells.fields.length > 0 ? cardFields : undefined}
		footer={cells.footer.length > 0 ? cardFooter : undefined}
	>
		{#if hasExpand && isExpanded && expandedRowContent}
			<div class="border-t border-border/50 bg-muted/30 px-4 py-3">
				{@render expandedRowContent({ row, item: row.original })}
			</div>
		{/if}
	</ResourceCard>
{/snippet}

{#snippet gridEntry(entry: GridEntry)}
	{#if entry.kind === 'group'}
		{@render groupHeader(entry.group)}
	{:else}
		<div class="grid grid-cols-(--grid-cols) items-stretch gap-3 px-3 py-1.5" style={gridColumnsStyle}>
			{#each entry.rows as row (row.id)}
				{@render gridCard(row)}
			{/each}
		</div>
	{/if}
{/snippet}

{#if loading && displayEntries.length === 0}
	{@render gridSkeleton()}
{:else if displayEntries.length === 0}
	<div class="min-h-48 p-4">
		{@render empty()}
	</div>
{:else if shouldVirtualize}
	<VirtualRows virtualizer={rowVirtualizer} rows={entries} row={gridEntry} />
{:else}
	<div class="py-1">
		{#each entries as entry (entry.key)}
			{@render gridEntry(entry)}
		{/each}
	</div>
{/if}

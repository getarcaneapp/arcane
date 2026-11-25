<script lang="ts" generics="TData extends Record<string, any> & { id: string }">
	import { PersistedState } from 'runed';
	import { untrack, type Snippet, type Component } from 'svelte';

	import * as Card from '#lib/components/ui/card/index.js';
	import Skeleton from '#lib/components/ui/skeleton/skeleton.svelte';
	import { createVirtualizer } from '#lib/components/ui/virtualizer.svelte.js';
	import VirtualRows from '#lib/components/virtual-rows.svelte';
	import { ArrowDownIcon, ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { TableDisplayEntry } from '#lib/types/ui/tabledisplay.js';
	import { cn } from '#lib/utils.js';

	import { setMobileRowSelection, type GroupedData, type SelectionModifiers } from './arcane-table.types.svelte';
	import { getTableDisplayEntries } from './arcane-table.utils';
	import type { ArcaneRow, ArcaneSvelteTable } from './table-features';

	let {
		table,
		rowIndex,
		mobileCard,
		mobileFieldVisibility,
		groupedRows = null,
		groupIcon,
		expandedRowContent,
		expandedRows,
		onToggleRowExpanded,
		scrollElement,
		initialScrollTop = 0,
		loading = false,
		virtualize = false,
		selectedIdSet,
		selectionDisabled = false,
		onToggleRowSelection,
		empty
	}: {
		table: ArcaneSvelteTable<TData>;
		rowIndex: ReadonlyMap<string, { row: ArcaneRow<TData>; index: number }>;
		mobileCard: Snippet<
			[{ row: ArcaneRow<TData>; item: TData; mobileFieldVisibility: Record<string, boolean>; expander?: Snippet }]
		>;
		mobileFieldVisibility: Record<string, boolean>;
		groupedRows?: GroupedData<TData>[] | null;
		groupIcon?: (groupName: string) => Component;
		expandedRowContent?: Snippet<[{ row: ArcaneRow<TData>; item: TData }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
		loading?: boolean;
		scrollElement?: HTMLElement;
		initialScrollTop?: number;
		virtualize?: boolean;
		selectedIdSet: ReadonlySet<string>;
		selectionDisabled?: boolean;
		onToggleRowSelection: (id: string, selected: boolean, modifiers?: SelectionModifiers) => void;
		empty: Snippet;
	} = $props();

	setMobileRowSelection({
		get enabled() {
			return !selectionDisabled;
		},
		isSelected: (id) => selectedIdSet.has(id),
		toggle: (id, selected, modifiers) => onToggleRowSelection(id, selected, modifiers)
	});

	const tableId = $props.id();
	const hasExpand = $derived(!!expandedRowContent);

	const groupExpansion = new PersistedState<Record<string, boolean>>('collapsible-cards-expanded', {}, { syncTabs: false });
	const collapsedGroups = $derived.by(() => {
		const collapsed: Record<string, boolean> = {};
		for (const group of groupedRows ?? []) {
			collapsed[group.groupName] = !(groupExpansion.current[`mobile-group-${group.groupName}`] ?? false);
		}
		return collapsed;
	});
	const entries = $derived(getTableDisplayEntries(table.getRowModel().rows, rowIndex, groupedRows, collapsedGroups));
	const getItemKey = $derived.by(() => {
		const displayed = entries;
		return (index: number) => displayed[index]?.key ?? index;
	});
	const rowVirtualizer = createVirtualizer<HTMLElement, HTMLDivElement>(() => ({
		count: entries.length,
		getScrollElement: () => scrollElement ?? null,
		estimateSize: (index) => (entries[index]?.kind === 'group' ? 90 : 160),
		overscan: 10,
		getItemKey,
		initialOffset: initialScrollTop,
		enabled: virtualize && !!scrollElement
	}));

	function toggleGroup(groupName: string) {
		groupExpansion.current = {
			...groupExpansion.current,
			[`mobile-group-${groupName}`]: collapsedGroups[groupName] ?? true
		};
	}

	$effect(() => {
		const container = scrollElement;
		if (!container || !virtualize) return;
		let width = container.clientWidth;
		const observer = new ResizeObserver(() => {
			if (container.clientWidth !== width) {
				width = container.clientWidth;
				rowVirtualizer.measure();
			}
		});
		observer.observe(container);
		const remeasure = () => rowVirtualizer.measure();
		document.fonts.addEventListener('loadingdone', remeasure);
		return () => {
			observer.disconnect();
			document.fonts.removeEventListener('loadingdone', remeasure);
		};
	});

	$effect(() => {
		if (!scrollElement || !virtualize || !mobileFieldVisibility) return;
		untrack(() => rowVirtualizer.measure());
	});

	$effect(() => {
		const container = scrollElement;
		const size = rowVirtualizer.totalSize;
		if (!container || !virtualize) return;
		const frame = requestAnimationFrame(() => {
			const max = Math.max(0, size - container.clientHeight);
			if (container.scrollTop > max) container.scrollTop = max;
		});
		return () => cancelAnimationFrame(frame);
	});
</script>

{#snippet mobileSkeleton()}
	{#each Array.from({ length: 6 }, (_, i) => i) as r (r)}
		<div class="px-3 py-2.5">
			<div class="flex items-center gap-3">
				<Skeleton class="size-9" />
				<div class="flex-1 space-y-1.5">
					<Skeleton class="h-4 w-1/2" />
					<Skeleton class="h-3 w-1/3" />
				</div>
			</div>
		</div>
	{/each}
{/snippet}

{#snippet mobileRow(row: ArcaneRow<TData>)}
	{@const rowId = row.id}
	{@const isExpanded = expandedRows?.has(rowId) ?? false}
	{#snippet expander()}
		<button
			type="button"
			class={cn(
				'flex size-8 items-center justify-center rounded-xl text-muted-foreground transition-transform duration-200 hover:text-foreground',
				isExpanded && 'rotate-90'
			)}
			onclick={() => onToggleRowExpanded?.(rowId)}
			aria-label={isExpanded ? m.table_collapse_row() : m.table_expand_row()}
			aria-expanded={isExpanded}
			aria-controls={isExpanded ? `${tableId}-detail-${rowId}` : undefined}
		>
			<ArrowRightIcon class="size-4" />
		</button>
	{/snippet}
	{@render mobileCard({ row, item: row.original, mobileFieldVisibility, expander: hasExpand ? expander : undefined })}
	{#if hasExpand && isExpanded && expandedRowContent}
		<div id="{tableId}-detail-{rowId}" class="bg-muted/30 px-4 py-3">
			{@render expandedRowContent({ row, item: row.original })}
		</div>
	{/if}
{/snippet}

{#snippet emptyState()}
	<div class="min-h-48 p-4">
		{@render empty()}
	</div>
{/snippet}

{#snippet displayEntry(entry: TableDisplayEntry<TData>)}
	{#if entry.kind === 'group'}
		{@const group = entry.group}
		{@const expanded = !collapsedGroups[group.groupName]}
		<Card.Root>
			<Card.Header
				icon={groupIcon?.(group.groupName)}
				enableHover
				class="cursor-pointer select-none"
				role="button"
				tabindex={0}
				aria-expanded={expanded}
				onclick={() => toggleGroup(group.groupName)}
				onkeydown={(event) => {
					if (event.key === 'Enter' || event.key === ' ') {
						event.preventDefault();
						toggleGroup(group.groupName);
					}
				}}
			>
				<div>
					<Card.Title>{group.groupName}</Card.Title>
					<Card.Description class="mt-1">{m.table_group_items({ count: group.items.length })}</Card.Description>
				</div>
				<Card.Action class="ml-auto">
					<ArrowDownIcon class={cn('size-5', expanded && 'rotate-180')} />
				</Card.Action>
			</Card.Header>
		</Card.Root>
	{:else}
		{@render mobileRow(entry.row)}
	{/if}
{/snippet}

{#if loading && entries.length === 0}
	{@render mobileSkeleton()}
{:else if entries.length === 0}
	{@render emptyState()}
{:else if virtualize}
	<VirtualRows virtualizer={rowVirtualizer} rows={entries} row={displayEntry} />
{:else}
	{#each entries as entry (entry.key)}
		{@render displayEntry(entry)}
	{/each}
{/if}

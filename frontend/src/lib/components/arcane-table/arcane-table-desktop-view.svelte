<script lang="ts">
	import { type Table as TableType, type Header, type Cell, type Row } from '@tanstack/table-core';
	import * as Table from '$lib/components/ui/table/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import { FolderXIcon, ArrowRightIcon, ArrowDownIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import type { ColumnWidth, ColumnAlign, GroupedData, GroupSelectionState } from './arcane-table.types.svelte.ts';
	import TableCheckbox from './arcane-table-checkbox.svelte';
	import type { Component, Snippet } from 'svelte';
	import { slide } from 'svelte/transition';

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
		onToggleRowExpanded
	}: {
		table: TableType<any>;
		selectedIds: string[];
		columnsCount: number;
		groupedRows?: GroupedData<any>[] | null;
		groupIcon?: (groupName: string) => Component;
		groupCollapsedState?: Record<string, boolean>;
		selectionDisabled?: boolean;
		onGroupToggle?: (groupName: string) => void;
		getGroupSelectionState?: (groupItems: any[]) => GroupSelectionState;
		onToggleGroupSelection?: (groupItems: any[]) => void;
		onToggleRowSelection?: (id: string, selected: boolean) => void;
		unstyled?: boolean;
		expandedRowContent?: Snippet<[{ row: Row<any>; item: any }]>;
		expandedRows?: Set<string>;
		onToggleRowExpanded?: (rowId: string) => void;
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

	const stickyActionsClasses = 'sticky right-0 z-10 transition-colors';
	const stickySelectClasses = 'w-0 pr-6!';

	function shouldIgnoreRowClick(event: MouseEvent): boolean {
		const target = event.target as HTMLElement | null;
		return !!target?.closest('a, button, input, [role="checkbox"], [data-slot="checkbox"], [data-row-select-ignore]');
	}

	function handleRowClick(event: MouseEvent, rowId: string) {
		if (shouldIgnoreRowClick(event)) return;
		if (hasExpand) {
			onToggleRowExpanded?.(rowId);
			return;
		}
		if (selectionDisabled) return;
		const isSelected = (selectedIds ?? []).includes(rowId);
		onToggleRowSelection?.(rowId, !isSelected);
	}

	// Get cell classes based on column metadata
	function getCellClasses(cell: Cell<any, unknown>, isGrouped: boolean, isFirstCell: boolean): string {
		const meta = cell.column.columnDef.meta as { width?: ColumnWidth; align?: ColumnAlign; truncate?: boolean } | undefined;
		return cn(
			cell.column.id === 'actions' && 'text-right whitespace-nowrap',
			cell.column.id === 'select' && stickySelectClasses,
			cell.column.id === 'actions' && stickyActionsClasses,
			getWidthClass(meta?.width),
			getAlignClass(meta?.align),
			meta?.truncate && 'max-w-0 truncate',
			isGrouped && isFirstCell && cell.column.id !== 'select' && 'pl-10'
		);
	}

	// Get rows for a specific group from the table model
	function getRowsForGroup(groupItems: any[]) {
		const groupIds = new Set(groupItems.map((item) => item.id));
		return table.getRowModel().rows.filter((row) => groupIds.has((row.original as any).id));
	}

	const isGrouped = $derived(groupedRows !== null && groupedRows.length > 0);
</script>

<div
	class={cn(
		'h-full w-full',
		unstyled &&
			'[&_tr]:border-border/40! [&_thead]:bg-transparent! [&_thead]:backdrop-blur-none [&_tr]:bg-transparent! [&_tr]:hover:bg-transparent! [&_tr:hover_td]:bg-transparent! [&_tr[data-state=selected]]:bg-transparent! [&_tr[data-state=selected]_td]:bg-transparent!'
	)}
>
	<Table.Root>
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
								header.column.id === 'select' && stickySelectClasses,
								header.column.id === 'actions' && stickyActionsClasses
							)}
						>
							{#if !header.isPlaceholder}
								<FlexRender content={header.column.columnDef.header} context={header.getContext()} />
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
					{@const groupRows = getRowsForGroup(group.items)}
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
							<Table.Cell class={stickySelectClasses}>
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
							{@const rowId = (row.original as any).id}
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
										{#if cell.column.id === 'actions'}
											<div class="flex items-center justify-end" data-row-select-ignore>
												<div
													class={cn(
														'border-border/40 bg-background pointer-events-auto flex items-center gap-1 rounded-full border px-2 py-1 shadow-sm',
														unstyled && 'border-transparent bg-transparent shadow-none'
													)}
												>
													<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
												</div>
											</div>
										{:else}
											<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
										{/if}
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
										<FolderXIcon class="text-muted-foreground/40 size-10" />
									</Empty.Media>
									<Empty.Title class="text-base font-medium">{m.common_no_results_found()}</Empty.Title>
								</Empty.Header>
							</Empty.Root>
						</Table.Cell>
					</Table.Row>
				{/if}
			{:else}
				{#each table.getRowModel().rows as row (row.id)}
					{@const rowId = (row.original as any).id}
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
						{#each row.getVisibleCells() as cell (cell.id)}
							<Table.Cell class={getCellClasses(cell, false, false)}>
								{#if cell.column.id === 'actions'}
									<div class="flex items-center justify-end" data-row-select-ignore>
										<div
											class={cn(
												'border-border/40 bg-background pointer-events-auto flex items-center gap-1 rounded-full border px-2 py-1 shadow-sm',
												unstyled && 'border-transparent bg-transparent shadow-none'
											)}
										>
											<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
										</div>
									</div>
								{:else}
									<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
								{/if}
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
				{:else}
					<Table.Row>
						<Table.Cell colspan={columnsCount} class="h-48">
							<Empty.Root
								class={cn('rounded-lg py-12', unstyled ? 'bg-transparent' : 'backdrop-blur-sm bg-card/30')}
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
						</Table.Cell>
					</Table.Row>
				{/each}
			{/if}
		</Table.Body>
	</Table.Root>
</div>

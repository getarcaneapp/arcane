<script lang="ts">
	import { type Table as TableType, type Header, type Cell } from '@tanstack/table-core';
	import * as Table from '$lib/components/ui/table/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import { FolderXIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import type { ColumnWidth, ColumnAlign } from './arcane-table.types.svelte';

	let {
		table,
		selectedIds,
		columnsCount
	}: {
		table: TableType<any>;
		selectedIds: string[];
		columnsCount: number;
	} = $props();

	// Check if the last column is the actions column
	const isActionsColumn = (columnId: string) => columnId === 'actions';
	const stickyActionsClass =
		'sticky right-0 w-fit bg-background/90 backdrop-blur-lg border-l-2 border-border/50 group-hover/row:bg-muted/90 group-data-[state=selected]/row:bg-primary/10';

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

	// Get header classes based on column metadata
	function getHeaderClasses(header: Header<any, unknown>): string {
		const meta = header.column.columnDef.meta as { width?: ColumnWidth; align?: ColumnAlign } | undefined;
		return cn(isActionsColumn(header.id) && stickyActionsClass, getWidthClass(meta?.width), getAlignClass(meta?.align));
	}

	// Get cell classes based on column metadata
	function getCellClasses(cell: Cell<any, unknown>): string {
		const meta = cell.column.columnDef.meta as { width?: ColumnWidth; align?: ColumnAlign; truncate?: boolean } | undefined;
		return cn(
			isActionsColumn(cell.column.id) && stickyActionsClass,
			getWidthClass(meta?.width),
			getAlignClass(meta?.align),
			meta?.truncate && 'max-w-0 truncate'
		);
	}
</script>

<div class="h-full w-full">
	<Table.Root>
		<Table.Header>
			{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
				<Table.Row>
					{#each headerGroup.headers as header (header.id)}
						<Table.Head colspan={header.colSpan} class={getHeaderClasses(header)}>
							{#if !header.isPlaceholder}
								{#if isActionsColumn(header.id)}
									<span class="sr-only">{m.common_open_menu()}</span>
								{:else}
									<FlexRender content={header.column.columnDef.header} context={header.getContext()} />
								{/if}
							{/if}
						</Table.Head>
					{/each}
				</Table.Row>
			{/each}
		</Table.Header>
		<Table.Body>
			{#each table.getRowModel().rows as row (row.id)}
				<Table.Row data-state={(selectedIds ?? []).includes((row.original as any).id) && 'selected'}>
					{#each row.getVisibleCells() as cell (cell.id)}
						<Table.Cell class={getCellClasses(cell)}>
							<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
						</Table.Cell>
					{/each}
				</Table.Row>
			{:else}
				<Table.Row>
					<Table.Cell colspan={columnsCount} class="h-48">
						<Empty.Root class="backdrop-blur-sm bg-card/30 rounded-lg py-12" role="status" aria-live="polite">
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
		</Table.Body>
	</Table.Root>
</div>

<script lang="ts">
	import { type Table as TableType } from '@tanstack/table-core';
	import * as Table from '$lib/components/ui/table/index.js';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import FlexRender from '$lib/components/ui/data-table/flex-render.svelte';
	import { FolderXIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';

	let {
		table,
		selectedIds,
		columnsCount
	}: {
		table: TableType<any>;
		selectedIds: string[];
		columnsCount: number;
	} = $props();
</script>

<div class="h-full w-full">
	<Table.Root class="relative">
		<Table.Header>
			{#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
				<Table.Row>
					{#each headerGroup.headers as header (header.id)}
						<Table.Head colspan={header.colSpan}>
							{#if !header.isPlaceholder}
								<FlexRender content={header.column.columnDef.header} context={header.getContext()} />
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
						<Table.Cell>
							<FlexRender content={cell.column.columnDef.cell} context={cell.getContext()} />
						</Table.Cell>
					{/each}
				</Table.Row>
			{:else}
				<Table.Row>
					<Table.Cell colspan={columnsCount} class="h-48">
						<Empty.Root class="border border-dashed">
							<Empty.Header>
								<Empty.Media variant="icon">
									<FolderXIcon />
								</Empty.Media>
								<Empty.Title>{m.common_no_results_found()}</Empty.Title>
							</Empty.Header>
						</Empty.Root>
					</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
</div>

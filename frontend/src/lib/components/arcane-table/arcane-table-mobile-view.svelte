<script lang="ts">
	import { type Table as TableType } from '@tanstack/table-core';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { FolderXIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	let {
		table,
		mobileCard,
		mobileFieldVisibility
	}: {
		table: TableType<any>;
		mobileCard: Snippet<[{ row: any; item: any; mobileFieldVisibility: Record<string, boolean> }]>;
		mobileFieldVisibility: Record<string, boolean>;
	} = $props();
</script>

<div class="divide-border/40 divide-y">
	{#each table.getRowModel().rows as row (row.id)}
		{@render mobileCard({ row, item: row.original as any, mobileFieldVisibility })}
	{:else}
		<Empty.Root class="min-h-48 border-0">
			<Empty.Header>
				<Empty.Media variant="icon">
					<FolderXIcon />
				</Empty.Media>
				<Empty.Title>{m.common_no_results_found()}</Empty.Title>
			</Empty.Header>
		</Empty.Root>
	{/each}
</div>

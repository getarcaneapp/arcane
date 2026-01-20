<script lang="ts">
	import { type Table as TableType } from '@tanstack/table-core';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import { FolderXIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';

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

<div class="divide-border/30 divide-y">
	{#each table.getRowModel().rows as row (row.id)}
		{@render mobileCard({ row, item: row.original as any, mobileFieldVisibility })}
	{:else}
		<div class="p-4">
			<Empty.Root class="min-h-48 rounded-xl border-0 bg-card/30 py-12 backdrop-blur-sm" role="status" aria-live="polite">
				<Empty.Header>
					<Empty.Media variant="icon">
						<FolderXIcon class="text-muted-foreground/40 size-10" />
					</Empty.Media>
					<Empty.Title class="text-base font-medium">{m.common_no_results_found()}</Empty.Title>
				</Empty.Header>
			</Empty.Root>
		</div>
	{/each}
</div>

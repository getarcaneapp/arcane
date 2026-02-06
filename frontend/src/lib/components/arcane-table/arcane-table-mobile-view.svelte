<script lang="ts">
	import { type Table as TableType } from '@tanstack/table-core';
	import * as Empty from '$lib/components/ui/empty/index.js';
	import DropdownCard from '$lib/components/dropdown-card.svelte';
	import { FolderXIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import type { Snippet, Component } from 'svelte';
	import type { GroupedData } from './arcane-table.types.svelte.ts';

	let {
		table,
		mobileCard,
		mobileFieldVisibility,
		groupedRows = null,
		groupIcon,
		onGroupToggle,
		groupCollapsedState = {},
		unstyled = false
	}: {
		table: TableType<any>;
		mobileCard: Snippet<[{ row: any; item: any; mobileFieldVisibility: Record<string, boolean> }]>;
		mobileFieldVisibility: Record<string, boolean>;
		groupedRows?: GroupedData<any>[] | null;
		groupIcon?: (groupName: string) => Component;
		onGroupToggle?: (groupName: string) => void;
		groupCollapsedState?: Record<string, boolean>;
		unstyled?: boolean;
	} = $props();

	// Get rows for a specific group from the table model
	function getRowsForGroup(groupItems: any[]) {
		const groupIds = new Set(groupItems.map((item) => item.id));
		return table.getRowModel().rows.filter((row) => groupIds.has((row.original as any).id));
	}

	// Check if we should render grouped view
	const isGrouped = $derived(groupedRows !== null && groupedRows.length > 0);
</script>

<div class="divide-border/30 divide-y">
	{#if isGrouped && groupedRows}
		<div class="space-y-4 py-2">
			{#each groupedRows as group (group.groupName)}
				{@const groupRows = getRowsForGroup(group.items)}
				{@const IconComponent = groupIcon?.(group.groupName)}

				<DropdownCard
					id={`mobile-group-${group.groupName}`}
					title={group.groupName}
					description={`${group.items.length} ${group.items.length === 1 ? 'item' : 'items'}`}
					icon={IconComponent}
				>
					<div class="divide-border/30 divide-y">
						{#each groupRows as row (row.id)}
							{@render mobileCard({ row, item: row.original as any, mobileFieldVisibility })}
						{:else}
							<div class="text-muted-foreground flex h-24 items-center justify-center text-center">
								{m.common_no_results_found()}
							</div>
						{/each}
					</div>
				</DropdownCard>
			{/each}
		</div>

		{#if groupedRows.length === 0}
			<div class="p-4">
				<Empty.Root
					class={cn('min-h-48 rounded-xl border-0 py-12', unstyled ? 'bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
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
			</div>
		{/if}
	{:else}
		<!-- Non-grouped view (original behavior) -->
		{#each table.getRowModel().rows as row (row.id)}
			{@render mobileCard({ row, item: row.original as any, mobileFieldVisibility })}
		{:else}
			<div class="p-4">
				<Empty.Root
					class={cn('min-h-48 rounded-xl border-0 py-12', unstyled ? 'bg-transparent' : 'bg-card/30 backdrop-blur-sm')}
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
			</div>
		{/each}
	{/if}
</div>

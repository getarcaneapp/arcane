<script lang="ts" generics="TData">
	import type { Table } from '@tanstack/table-core';
	import { DataTableFacetedFilter, DataTableViewOptions } from './index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { imageUpdateFilters, usageFilters, severityFilters, templateTypeFilters, projectStatusFilters } from './data.js';
	import { debounced } from '$lib/utils/utils.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';
	import { ResetIcon } from '$lib/icons';
	import type { BulkAction } from './arcane-table.types.svelte';

	let {
		table,
		selectedIds = [],
		selectionDisabled = false,
		bulkActions = [],
		mobileFields = [],
		onToggleMobileField,
		customViewOptions,
		customToolbarActions,
		class: className
	}: {
		table: Table<TData>;
		selectedIds?: string[];
		selectionDisabled?: boolean;
		bulkActions?: BulkAction[];
		mobileFields?: { id: string; label: string; visible: boolean }[];
		onToggleMobileField?: (fieldId: string) => void;
		customViewOptions?: Snippet;
		customToolbarActions?: Snippet;
		class?: string;
	} = $props();

	const isFiltered = $derived(table.getState().columnFilters.length > 0 || !!table.getState().globalFilter);
	const usageColumn = $derived(table.getAllColumns().some((col) => col.id === 'inUse') ? table.getColumn('inUse') : undefined);
	const updatesColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'updates') ? table.getColumn('updates') : undefined
	);
	const severityColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'severity') ? table.getColumn('severity') : undefined
	);
	const statusColumn = $derived(table.getAllColumns().some((col) => col.id === 'status') ? table.getColumn('status') : undefined);
	const serviceCountColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'serviceCount') ? table.getColumn('serviceCount') : undefined
	);
	const typeColumn = $derived(table.getAllColumns().some((col) => col.id === 'type') ? table.getColumn('type') : undefined);

	const debouncedSetGlobal = debounced((v: string) => table.setGlobalFilter(v), 300);
	const hasSelection = $derived(!selectionDisabled && (selectedIds?.length ?? 0) > 0);
	const hasBulkActions = $derived(bulkActions && bulkActions.length > 0);
</script>

<div class={cn('flex flex-col gap-2 px-2 py-2 sm:flex-row sm:items-center sm:justify-between', className)}>
	<div class="flex flex-col gap-2 sm:flex-1 sm:flex-row sm:items-center sm:justify-between">
		<div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:space-x-2">
			<div class="flex items-center gap-2">
				<Input
					placeholder={m.common_search()}
					value={(table.getState().globalFilter as string) ?? ''}
					oninput={(e) => debouncedSetGlobal(e.currentTarget.value)}
					onchange={(e) => table.setGlobalFilter(e.currentTarget.value)}
					onkeydown={(e) => {
						if (e.key === 'Enter') table.setGlobalFilter((e.currentTarget as HTMLInputElement).value);
					}}
					class="h-8 w-full sm:w-[150px] lg:w-[250px]"
				/>

				<div class="md:hidden">
					{#if mobileFields.length > 0 && onToggleMobileField}
						<DataTableViewOptions fields={mobileFields} onToggleField={onToggleMobileField} {customViewOptions} />
					{:else}
						<DataTableViewOptions {table} {customViewOptions} />
					{/if}
				</div>
			</div>

			<div class="flex flex-wrap items-center gap-2 sm:gap-0 sm:space-x-2">
				{#if typeColumn && !severityColumn}
					<DataTableFacetedFilter column={typeColumn} title={m.common_type()} options={templateTypeFilters} />
				{/if}
				{#if usageColumn}
					<DataTableFacetedFilter column={usageColumn} title={m.common_usage()} options={usageFilters} />
				{/if}
				{#if updatesColumn}
					<DataTableFacetedFilter column={updatesColumn} title={m.images_updates()} options={imageUpdateFilters} />
				{/if}
				{#if severityColumn}
					<DataTableFacetedFilter column={severityColumn} title={m.events_col_severity()} options={severityFilters} />
				{/if}
				{#if statusColumn && serviceCountColumn}
					<DataTableFacetedFilter column={statusColumn} title={m.common_status()} options={projectStatusFilters} />
				{/if}

				{#if isFiltered}
					<ArcaneButton
						action="base"
						tone="ghost"
						size="sm"
						icon={ResetIcon}
						customLabel={m.common_reset()}
						onclick={() => {
							table.setColumnFilters([]);
							table.setGlobalFilter('');
						}}
						class="h-8 px-2 lg:px-3"
					/>
				{/if}
			</div>
		</div>
	</div>

	<!-- Bulk actions and View options -->
	<div class="flex items-center gap-2">
		{#if hasSelection}
			{#if hasBulkActions}
				<div class="flex flex-wrap items-center gap-2">
					{#each bulkActions as bulkAction (bulkAction.id)}
						{@const actionType = bulkAction.action === 'up' ? 'start' : bulkAction.action === 'down' ? 'stop' : bulkAction.action}
						<ArcaneButton
							action={actionType}
							icon={bulkAction.icon}
							customLabel={bulkAction.label}
							onclick={() => bulkAction.onClick(selectedIds!)}
							disabled={bulkAction.disabled || bulkAction.loading}
							loading={bulkAction.loading}
						/>
					{/each}
				</div>
			{/if}
		{/if}

		{#if customToolbarActions}
			{@render customToolbarActions()}
		{/if}

		<!-- View options - desktop only, always on the right -->
		<div class="hidden md:block">
			<DataTableViewOptions {table} {customViewOptions} />
		</div>
	</div>
</div>

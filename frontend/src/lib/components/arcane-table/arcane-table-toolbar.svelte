<script lang="ts" generics="TData">
	import type { Table } from '@tanstack/table-core';
	import { DataTableFacetedFilter, DataTableViewOptions } from './index.js';
	import Button from '$lib/components/ui/button/button.svelte';
	import { Input } from '$lib/components/ui/input/index.js';
	import { imageUpdateFilters, usageFilters, severityFilters, templateTypeFilters } from './data.js';
	import { debounced } from '$lib/utils/utils.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';
	import { ResetIcon } from '$lib/icons';

	let {
		table,
		selectedIds = [],
		selectionDisabled = false,
		onRemoveSelected,
		mobileFields = [],
		onToggleMobileField,
		customViewOptions,
		class: className
	}: {
		table: Table<TData>;
		selectedIds?: string[];
		selectionDisabled?: boolean;
		onRemoveSelected?: (ids: string[]) => void;
		mobileFields?: { id: string; label: string; visible: boolean }[];
		onToggleMobileField?: (fieldId: string) => void;
		customViewOptions?: Snippet;
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
	const typeColumn = $derived(table.getAllColumns().some((col) => col.id === 'type') ? table.getColumn('type') : undefined);

	const debouncedSetGlobal = debounced((v: string) => table.setGlobalFilter(v), 300);
	const hasSelection = $derived(!selectionDisabled && (selectedIds?.length ?? 0) > 0);
</script>

<div class={cn('flex flex-col gap-2 px-6 py-4 sm:flex-row sm:items-center sm:justify-between', className)}>
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
				{#if typeColumn}
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

				{#if isFiltered}
					<Button
						variant="ghost"
						onclick={() => {
							table.resetColumnFilters();
							table.resetGlobalFilter();
						}}
						class="h-8 px-2 lg:px-3"
					>
						{m.common_reset()}
						<ResetIcon />
					</Button>
				{/if}
			</div>
		</div>

		<!-- View options - desktop only, end aligned -->
		<div class="hidden md:block">
			<DataTableViewOptions {table} {customViewOptions} />
		</div>
	</div>

	<!-- Actions on the right (wraps on mobile) -->
	{#if hasSelection && onRemoveSelected}
		<ArcaneButton
			action="remove"
			size="sm"
			hoverEffect="none"
			customLabel={m.common_remove_selected_count({ count: selectedIds?.length ?? 0 })}
			onclick={() => onRemoveSelected?.(selectedIds!)}
		/>
	{/if}
</div>

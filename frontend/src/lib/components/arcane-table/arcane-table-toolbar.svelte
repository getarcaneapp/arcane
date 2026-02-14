<script lang="ts" generics="TData">
	import type { Table } from '@tanstack/table-core';
	import { DataTableFacetedFilter, DataTableViewOptions } from './index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import {
		imageUpdateFilters,
		usageFilters,
		severityFilters,
		vulnerabilitySeverityFilters,
		templateTypeFilters,
		projectStatusFilters
	} from './data.js';
	import { debounced } from '$lib/utils/utils.js';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';
	import { ResetIcon, SearchIcon, FilterIcon } from '$lib/icons';
	import type { BulkAction } from './arcane-table.types.svelte';
	import * as Popover from '$lib/components/ui/popover/index.js';

	let {
		table,
		selectedIds = [],
		selectionDisabled = false,
		bulkActions = [],
		mobileFields = [],
		onToggleMobileField,
		customViewOptions,
		customToolbarActions,
		class: className,
		imageNameFilterOptions = []
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
		imageNameFilterOptions?: string[];
	} = $props();

	const isFiltered = $derived(table.getState().columnFilters.length > 0 || !!table.getState().globalFilter);
	const usageColumn = $derived(table.getAllColumns().some((col) => col.id === 'inUse') ? table.getColumn('inUse') : undefined);
	const updatesColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'updates') ? table.getColumn('updates') : undefined
	);
	const severityColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'severity') ? table.getColumn('severity') : undefined
	);
	const vulnSeverityColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'vulnSeverity') ? table.getColumn('vulnSeverity') : undefined
	);
	const imageNameColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'imageName') ? table.getColumn('imageName') : undefined
	);
	const statusColumn = $derived(table.getAllColumns().some((col) => col.id === 'status') ? table.getColumn('status') : undefined);
	const serviceCountColumn = $derived(
		table.getAllColumns().some((col) => col.id === 'serviceCount') ? table.getColumn('serviceCount') : undefined
	);
	const typeColumn = $derived(table.getAllColumns().some((col) => col.id === 'type') ? table.getColumn('type') : undefined);

	const debouncedSetGlobal = debounced((v: string) => table.setGlobalFilter(v), 300);
	const imageNameFilterOptionsFormatted = $derived(imageNameFilterOptions.map((name) => ({ label: name, value: name })));
	const hasSelection = $derived(!selectionDisabled && (selectedIds?.length ?? 0) > 0);
	const hasBulkActions = $derived(bulkActions && bulkActions.length > 0);

	// Check if any filter columns exist
	const hasFilterColumns = $derived(
		!!(typeColumn && !severityColumn && !vulnSeverityColumn) ||
			!!usageColumn ||
			!!updatesColumn ||
			!!severityColumn ||
			!!vulnSeverityColumn ||
			!!(imageNameColumn && imageNameFilterOptions.length > 0) ||
			!!(statusColumn && serviceCountColumn)
	);
	const activeFilterCount = $derived(table.getState().columnFilters.length);
</script>

<div class={cn('flex items-center justify-between gap-2 px-3 py-2.5', className)}>
	<div class="flex min-w-0 flex-1 items-center gap-2 md:flex-none">
		<div class="relative min-w-0 flex-1 md:w-64 md:flex-none">
			<SearchIcon class="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
			<Input
				placeholder={m.common_search()}
				value={(table.getState().globalFilter as string) ?? ''}
				oninput={(e) => debouncedSetGlobal(e.currentTarget.value)}
				onchange={(e) => table.setGlobalFilter(e.currentTarget.value)}
				onkeydown={(e) => {
					if (e.key === 'Enter') table.setGlobalFilter((e.currentTarget as HTMLInputElement).value);
				}}
				class="h-9 w-full pl-8"
			/>
		</div>

		{#if hasFilterColumns}
			<div class="hidden items-center gap-1.5 md:flex">
				{#if typeColumn && !severityColumn && !vulnSeverityColumn}
					<DataTableFacetedFilter column={typeColumn} title={m.common_type()} options={templateTypeFilters} />
				{/if}
				{#if usageColumn}
					<DataTableFacetedFilter column={usageColumn} title={m.common_usage()} options={usageFilters} />
				{/if}
				{#if updatesColumn}
					<DataTableFacetedFilter column={updatesColumn} title={m.images_updates()} options={imageUpdateFilters} />
				{/if}
				{#if vulnSeverityColumn}
					<DataTableFacetedFilter
						column={vulnSeverityColumn}
						title={m.events_col_severity()}
						options={vulnerabilitySeverityFilters}
					/>
				{:else if severityColumn}
					<DataTableFacetedFilter column={severityColumn} title={m.events_col_severity()} options={severityFilters} />
				{/if}
				{#if imageNameColumn && imageNameFilterOptionsFormatted.length > 0}
					<DataTableFacetedFilter column={imageNameColumn} title={m.common_image()} options={imageNameFilterOptionsFormatted} />
				{/if}
				{#if statusColumn && serviceCountColumn}
					<DataTableFacetedFilter column={statusColumn} title={m.common_status()} options={projectStatusFilters} />
				{/if}
			</div>

			<div class="md:hidden">
				<Popover.Root>
					<Popover.Trigger>
						{#snippet child({ props })}
							<ArcaneButton {...props} action="base" tone="outline" size="icon" class="relative size-9">
								<FilterIcon class="size-4" />
								{#if activeFilterCount > 0}
									<span
										class="bg-primary text-primary-foreground absolute -top-1 -right-1 flex size-4 items-center justify-center rounded-full text-[10px] font-medium"
									>
										{activeFilterCount}
									</span>
								{/if}
							</ArcaneButton>
						{/snippet}
					</Popover.Trigger>
					<Popover.Content align="end" class="w-56 p-2">
						<div class="flex flex-col gap-1.5">
							{#if typeColumn && !severityColumn && !vulnSeverityColumn}
								<DataTableFacetedFilter column={typeColumn} title={m.common_type()} options={templateTypeFilters} />
							{/if}
							{#if usageColumn}
								<DataTableFacetedFilter column={usageColumn} title={m.common_usage()} options={usageFilters} />
							{/if}
							{#if updatesColumn}
								<DataTableFacetedFilter column={updatesColumn} title={m.images_updates()} options={imageUpdateFilters} />
							{/if}
							{#if vulnSeverityColumn}
								<DataTableFacetedFilter
									column={vulnSeverityColumn}
									title={m.events_col_severity()}
									options={vulnerabilitySeverityFilters}
								/>
							{:else if severityColumn}
								<DataTableFacetedFilter column={severityColumn} title={m.events_col_severity()} options={severityFilters} />
							{/if}
							{#if imageNameColumn && imageNameFilterOptionsFormatted.length > 0}
								<DataTableFacetedFilter
									column={imageNameColumn}
									title={m.common_image()}
									options={imageNameFilterOptionsFormatted}
								/>
							{/if}
							{#if statusColumn && serviceCountColumn}
								<DataTableFacetedFilter column={statusColumn} title={m.common_status()} options={projectStatusFilters} />
							{/if}
						</div>
					</Popover.Content>
				</Popover.Root>
			</div>
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
				class="h-9 shrink-0"
			/>
		{/if}
	</div>

	<div class="flex shrink-0 items-center gap-2">
		{#if hasSelection && hasBulkActions}
			<div class="flex items-center gap-1.5">
				{#each bulkActions as bulkAction (bulkAction.id)}
					{@const actionType = bulkAction.action === 'up' ? 'start' : bulkAction.action === 'down' ? 'stop' : bulkAction.action}
					<ArcaneButton
						action={actionType}
						size="sm"
						icon={bulkAction.icon}
						customLabel={bulkAction.label}
						onclick={() => bulkAction.onClick(selectedIds!)}
						disabled={bulkAction.disabled || bulkAction.loading}
						loading={bulkAction.loading}
						class="h-9"
					/>
				{/each}
			</div>
		{/if}

		{#if customToolbarActions}
			{@render customToolbarActions()}
		{/if}

		<div class="hidden md:block">
			<DataTableViewOptions {table} {customViewOptions} />
		</div>
		<div class="md:hidden">
			{#if mobileFields.length > 0 && onToggleMobileField}
				<DataTableViewOptions fields={mobileFields} onToggleField={onToggleMobileField} {customViewOptions} />
			{:else}
				<DataTableViewOptions {table} {customViewOptions} />
			{/if}
		</div>
	</div>
</div>

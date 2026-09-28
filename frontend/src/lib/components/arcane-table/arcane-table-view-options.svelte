<script lang="ts" generics="TData extends Record<string, any>">
	import type { ArcaneSvelteTable } from './table-features';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Snippet } from 'svelte';
	import { EyeOnIcon, ArrowUpIcon, ArrowDownIcon } from '#lib/icons/index.js';

	let {
		table,
		fields,
		onToggleField,
		customViewOptions,
		wrapText = false,
		onToggleWrapText,
		showSorting = false
	}: {
		table?: ArcaneSvelteTable<TData>;
		fields?: { id: string; label: string; visible: boolean }[];
		onToggleField?: (fieldId: string) => void;
		customViewOptions?: Snippet;
		wrapText?: boolean;
		onToggleWrapText?: () => void;
		showSorting?: boolean;
	} = $props();

	// Mobile cards have no column headers, so the menu exposes the same column sorts.
	// Cards follow mobile field visibility, not desktop column visibility.
	function isSortVisible(columnId: string, visible: boolean): boolean {
		if (!fields) return visible;
		return fields.find((field) => field.id === columnId)?.visible ?? true;
	}
	const sortableColumns = $derived(
		showSorting && table
			? table.getAllColumns().filter((col) => col.getCanSort() && isSortVisible(col.id, col.getIsVisible()))
			: []
	);
	const activeSort = $derived(table?.atoms.sorting.get()[0]);
	const activeSortColumn = $derived(activeSort ? sortableColumns.find((col) => col.id === String(activeSort.id)) : undefined);

	function selectSortColumn(columnId: string) {
		const column = sortableColumns.find((col) => col.id === columnId);
		if (!column) return;
		column.toggleSorting(activeSort?.desc ?? false);
	}
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{...props}
				action="base"
				tone="ghost"
				icon={EyeOnIcon}
				customLabel={m.common_view()}
				class="border border-input hover:bg-card/60 hover:text-inherit"
			/>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		{#if customViewOptions || onToggleWrapText}
			<DropdownMenu.Group>
				<DropdownMenu.Label>{m.common_view()}</DropdownMenu.Label>
				<DropdownMenu.Separator />
				{#if onToggleWrapText}
					<DropdownMenu.CheckboxItem checked={wrapText} onCheckedChange={() => onToggleWrapText()}>
						{m.wrap_text()}
					</DropdownMenu.CheckboxItem>
				{/if}
				{#if customViewOptions}
					{@render customViewOptions()}
				{/if}
			</DropdownMenu.Group>
			<DropdownMenu.Separator />
		{/if}
		{#if sortableColumns.length > 0}
			<DropdownMenu.Group>
				<DropdownMenu.Label>{m.common_sort_by()}</DropdownMenu.Label>
				<DropdownMenu.Separator />
				<DropdownMenu.RadioGroup value={activeSortColumn?.id ?? ''} onValueChange={selectSortColumn}>
					{#each sortableColumns as column (column.id)}
						<DropdownMenu.RadioItem value={column.id} closeOnSelect={false}>
							{column.columnDef.meta?.title ?? column.id}
						</DropdownMenu.RadioItem>
					{/each}
				</DropdownMenu.RadioGroup>
				<DropdownMenu.Separator />
				<DropdownMenu.RadioGroup
					value={activeSortColumn ? (activeSort?.desc ? 'desc' : 'asc') : ''}
					onValueChange={(value) => activeSortColumn?.toggleSorting(value === 'desc')}
				>
					<DropdownMenu.RadioItem value="asc" disabled={!activeSortColumn} closeOnSelect={false}>
						<ArrowUpIcon class="size-4 text-muted-foreground/70" />
						{m.common_sort_asc()}
					</DropdownMenu.RadioItem>
					<DropdownMenu.RadioItem value="desc" disabled={!activeSortColumn} closeOnSelect={false}>
						<ArrowDownIcon class="size-4 text-muted-foreground/70" />
						{m.common_sort_desc()}
					</DropdownMenu.RadioItem>
				</DropdownMenu.RadioGroup>
			</DropdownMenu.Group>
			<DropdownMenu.Separator />
		{/if}
		<DropdownMenu.Group>
			<DropdownMenu.Label>{m.common_toggle_columns()}</DropdownMenu.Label>
			<DropdownMenu.Separator />

			{#if fields && onToggleField}
				{#each fields as field (field.id)}
					<DropdownMenu.CheckboxItem checked={field.visible} onCheckedChange={() => onToggleField(field.id)}>
						{field.label}
					</DropdownMenu.CheckboxItem>
				{/each}
			{:else if table}
				{#each table
					.getAllColumns()
					.filter((col) => typeof col.accessorFn !== 'undefined' && col.getCanHide()) as column (column)}
					{@const headerText = column.columnDef.meta?.title ?? column.id}
					<DropdownMenu.CheckboxItem
						checked={column.getIsVisible()}
						onCheckedChange={(checked) => column.toggleVisibility(checked === true)}
					>
						{headerText}
					</DropdownMenu.CheckboxItem>
				{/each}
			{/if}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>

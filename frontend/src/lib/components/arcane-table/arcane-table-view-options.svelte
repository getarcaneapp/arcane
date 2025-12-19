<script lang="ts" generics="TData">
	import type { Table } from '@tanstack/table-core';
	import { buttonVariants } from '$lib/components/ui/button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';
	import { ViewOptionsIcon } from '$lib/icons';

	let {
		table,
		fields,
		onToggleField,
		customViewOptions
	}: {
		table?: Table<TData>;
		fields?: { id: string; label: string; visible: boolean }[];
		onToggleField?: (fieldId: string) => void;
		customViewOptions?: Snippet;
	} = $props();
</script>

<DropdownMenu.Root>
	<DropdownMenu.Trigger
		class={buttonVariants({
			variant: 'ghost',
			size: 'sm',
			class: 'border-input hover:bg-card/60 h-8 border hover:text-inherit'
		})}
	>
		<ViewOptionsIcon />
		{m.common_view()}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="end">
		{#if customViewOptions}
			<DropdownMenu.Group>
				<DropdownMenu.Label>{m.common_view()}</DropdownMenu.Label>
				<DropdownMenu.Separator />
				{@render customViewOptions()}
			</DropdownMenu.Group>
			<DropdownMenu.Separator />
		{/if}
		<DropdownMenu.Group>
			<DropdownMenu.Label>{m.common_toggle_columns()}</DropdownMenu.Label>
			<DropdownMenu.Separator />

			{#if table}
				{#each table
					.getAllColumns()
					.filter((col) => typeof col.accessorFn !== 'undefined' && col.getCanHide()) as column (column)}
					<DropdownMenu.CheckboxItem
						bind:checked={() => column.getIsVisible(), (v) => column.toggleVisibility(!!v)}
						class="capitalize"
					>
						{column.id}
					</DropdownMenu.CheckboxItem>
				{/each}
			{:else if fields && onToggleField}
				{#each fields as field (field.id)}
					<DropdownMenu.CheckboxItem bind:checked={() => field.visible, (v) => onToggleField(field.id)} class="capitalize">
						{field.label}
					</DropdownMenu.CheckboxItem>
				{/each}
			{/if}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>

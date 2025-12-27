<script lang="ts" generics="TData">
	import type { Table } from '@tanstack/table-core';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
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
	<DropdownMenu.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{...props}
				action="base"
				tone="ghost"
				size="sm"
				icon={ViewOptionsIcon}
				customLabel={m.common_view()}
				class="border-input hover:bg-card/60 h-8 border hover:text-inherit"
			/>
		{/snippet}
	</DropdownMenu.Trigger>
	<DropdownMenu.Content align="center">
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
					{@const meta = column.columnDef.meta as { title?: string }}
					{@const headerText = meta?.title ?? column.id}
					<DropdownMenu.CheckboxItem bind:checked={() => column.getIsVisible(), (v) => column.toggleVisibility(!!v)}>
						{headerText}
					</DropdownMenu.CheckboxItem>
				{/each}
			{:else if fields && onToggleField}
				{#each fields as field (field.id)}
					<DropdownMenu.CheckboxItem bind:checked={() => field.visible, (v) => onToggleField(field.id)}>
						{field.label}
					</DropdownMenu.CheckboxItem>
				{/each}
			{/if}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>

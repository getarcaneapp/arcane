<script lang="ts" generics="TData extends Record<string, any>">
	import type { ArcaneSvelteTable } from './table-features';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { m } from '$lib/paraglide/messages';
	import type { Snippet } from 'svelte';
	import { EyeOnIcon } from '$lib/icons';

	let {
		table,
		fields,
		onToggleField,
		customViewOptions
	}: {
		table?: ArcaneSvelteTable<TData>;
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
				icon={EyeOnIcon}
				customLabel={m.common_view()}
				class="border-input hover:bg-card/60 border hover:text-inherit"
			/>
		{/snippet}
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
					{@const headerText = column.columnDef.meta?.title ?? column.id}
					<DropdownMenu.CheckboxItem
						checked={column.getIsVisible()}
						onCheckedChange={(checked) => column.toggleVisibility(checked === true)}
					>
						{headerText}
					</DropdownMenu.CheckboxItem>
				{/each}
			{:else if fields && onToggleField}
				{#each fields as field (field.id)}
					<DropdownMenu.CheckboxItem checked={field.visible} onCheckedChange={() => onToggleField(field.id)}>
						{field.label}
					</DropdownMenu.CheckboxItem>
				{/each}
			{/if}
		</DropdownMenu.Group>
	</DropdownMenu.Content>
</DropdownMenu.Root>

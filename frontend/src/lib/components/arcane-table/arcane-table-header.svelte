<script lang="ts">
	import type { SvelteHTMLElements } from 'svelte/elements';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ArrowUpIcon, ArrowDownIcon, ArrowsUpDownIcon, EyeOffIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import type { ColumnAlign } from './arcane-table.types.svelte';

	type DivAttributes = SvelteHTMLElements['div'];

	// Structural shape of the column controls this header drives. v9's `Column<TFeatures, TData>`
	// is declared `in out TData` (invariant), so even `ArcaneColumn<any>` rejects the deferred
	// `TData` at the renderComponent call site (re-verified at 9.1.0 stable, 2026-08). Depending
	// only on the methods used here keeps the prop generic-free with no `any`.
	type SortableHeaderColumn = {
		getCanSort: () => boolean;
		getIsSorted: () => false | 'asc' | 'desc';
		toggleSorting: (desc?: boolean) => void;
		toggleVisibility: (value?: boolean) => void;
	};

	let {
		column,
		title,
		align,
		class: className,
		...restProps
	}: {
		column?: SortableHeaderColumn;
		title: string;
		align?: ColumnAlign;
		class?: string;
	} & DivAttributes = $props();
</script>

{#if !column?.getCanSort()}
	<div class={cn(align === 'center' && 'text-center', align === 'right' && 'text-right', className)} {...restProps}>
		{title}
	</div>
{:else}
	<div
		class={cn('flex items-center', align === 'center' && 'justify-center', align === 'right' && 'justify-end', className)}
		{...restProps}
	>
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<ArcaneButton
						{...props}
						action="base"
						tone="ghost"
						size="sm"
						customLabel={title}
						class={cn(
							'h-8 text-xs data-[state=open]:bg-accent',
							(align === undefined || align === 'left') && '-ml-3',
							align === 'right' && '-mr-3',
							column.getIsSorted() ? 'text-foreground' : 'text-muted-foreground'
						)}
					>
						{#if column.getIsSorted() === 'desc'}
							<ArrowDownIcon class="size-3.5 text-primary dark:text-primary-tint" />
						{:else if column.getIsSorted() === 'asc'}
							<ArrowUpIcon class="size-3.5 text-primary dark:text-primary-tint" />
						{:else}
							<ArrowsUpDownIcon class="size-3.5 text-muted-foreground/50" />
						{/if}
					</ArcaneButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="start">
				<DropdownMenu.Item onclick={() => column.toggleSorting(false)}>
					<ArrowUpIcon class="mr-2 size-4 text-muted-foreground/70" />
					{m.common_sort_asc()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => column.toggleSorting(true)}>
					<ArrowDownIcon class="mr-2 size-4 text-muted-foreground/70" />
					{m.common_sort_desc()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item onclick={() => column.toggleVisibility(false)}>
					<EyeOffIcon class="mr-2 size-4 text-muted-foreground/70" />
					{m.common_hide()}
				</DropdownMenu.Item>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</div>
{/if}

<script lang="ts">
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages';
	import { ArrowUpIcon, ArrowDownIcon, ArrowsUpDownIcon, EyeOffIcon } from '#lib/icons';
	import type { SvelteHTMLElements } from 'svelte/elements';

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
		class: className,
		...restProps
	}: {
		column?: SortableHeaderColumn;
		title: string;
		class?: string;
	} & DivAttributes = $props();
</script>

{#if !column?.getCanSort()}
	<div class={className} {...restProps}>
		{title}
	</div>
{:else}
	<div class={cn('flex items-center', className)} {...restProps}>
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<ArcaneButton
						{...props}
						action="base"
						tone="ghost"
						size="sm"
						customLabel={title}
						class="-ml-3 h-8 data-[state=open]:bg-accent"
					>
						{#if column.getIsSorted() === 'desc'}
							<ArrowDownIcon class="size-4 text-foreground" />
						{:else if column.getIsSorted() === 'asc'}
							<ArrowUpIcon class="size-4 text-foreground" />
						{:else}
							<ArrowsUpDownIcon class="size-4 text-muted-foreground/70" />
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

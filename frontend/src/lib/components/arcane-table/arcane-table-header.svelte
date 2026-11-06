<script lang="ts">
	import type { SvelteHTMLElements } from 'svelte/elements';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { ArrowUpIcon, ArrowDownIcon, ArrowsUpDownIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	import type { ColumnAlign } from './arcane-table.types.svelte';

	type DivAttributes = SvelteHTMLElements['div'];

	type SortableHeaderColumn = {
		getCanSort: () => boolean;
		getIsSorted: () => false | 'asc' | 'desc';
		toggleSorting: () => void;
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
		<ArcaneButton
			action="base"
			tone="ghost"
			customLabel={title}
			showLabel={align !== 'right'}
			aria-label={`${m.common_sort_by()} ${title}`}
			onclick={() => column.toggleSorting()}
			class={cn(
				'h-8 gap-1.5 px-2.5 text-xs-plus leading-5 has-[svg]:px-2.5',
				(align === undefined || align === 'left') && '-ml-3',
				align === 'right' && '-mr-3',
				column.getIsSorted() ? 'text-foreground!' : 'text-muted-foreground!'
			)}
		>
			{#if column.getIsSorted() === 'desc'}
				<ArrowDownIcon class="size-3.5 text-primary-text" />
			{:else if column.getIsSorted() === 'asc'}
				<ArrowUpIcon class="size-3.5 text-primary-text" />
			{:else}
				<ArrowsUpDownIcon class="size-3.5 text-muted-foreground" />
			{/if}
			{#if align === 'right'}
				{title}
			{/if}
		</ArcaneButton>
	</div>
{/if}

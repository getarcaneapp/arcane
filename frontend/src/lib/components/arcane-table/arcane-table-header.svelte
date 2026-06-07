<script lang="ts">
	import { type Column } from '@tanstack/table-core';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { cn } from '$lib/utils.js';
	import { m } from '$lib/paraglide/messages';
	import { ArrowUpIcon, ArrowDownIcon, ArrowsUpDownIcon, EyeOffIcon } from '$lib/icons';
	import type { SvelteHTMLElements } from 'svelte/elements';

	type DivAttributes = SvelteHTMLElements['div'];

	let {
		column,
		title,
		class: className,
		...restProps
	}: {
		column?: Column<any, any>;
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
						class="data-[state=open]:bg-accent -ml-3 h-8"
					>
						{#if column.getIsSorted() === 'desc'}
							<ArrowDownIcon />
						{:else if column.getIsSorted() === 'asc'}
							<ArrowUpIcon />
						{:else}
							<ArrowsUpDownIcon />
						{/if}
					</ArcaneButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="start">
				<DropdownMenu.Item onclick={() => column.toggleSorting(false)}>
					<ArrowUpIcon class="text-muted-foreground/70 mr-2 size-4" />
					{m.common_sort_asc()}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={() => column.toggleSorting(true)}>
					<ArrowDownIcon class="text-muted-foreground/70 mr-2 size-4" />
					{m.common_sort_desc()}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item onclick={() => column.toggleVisibility(false)}>
					<EyeOffIcon class="text-muted-foreground/70 mr-2 size-4" />
					{m.common_hide()}
				</DropdownMenu.Item>
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</div>
{/if}

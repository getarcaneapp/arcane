<script lang="ts" generics="TData, TValue">
	import type { Column } from '@tanstack/table-core';
	import { SvelteSet } from 'svelte/reactivity';
	import * as Command from '$lib/components/ui/command/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils.js';
	import { Separator } from '$lib/components/ui/separator/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import type { Component } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { CheckIcon, FilterIcon } from '$lib/icons';

	let {
		column,
		title,
		options,
		showCheckboxes = true
	}: {
		column: Column<TData, TValue>;
		title: string;
		options: {
			label: string;
			value: string | boolean;
			icon?: Component;
		}[];
		showCheckboxes?: boolean;
	} = $props();

	const selectedValues = $derived(new SvelteSet(column?.getFilterValue() as any[]));
</script>

<Popover.Root>
	<Popover.Trigger>
		{#snippet child({ props })}
			<Button
				{...props}
				variant="ghost"
				size="sm"
				class="border-input hover:bg-card/60 h-8 min-w-24 border border-dashed hover:text-inherit"
				data-testid={`facet-${title.toLowerCase()}-trigger`}
			>
				<FilterIcon />
				{title}
				{#if selectedValues.size > 0}
					<Separator orientation="vertical" class="mx-2 h-4" />
					<Badge variant="secondary" class="rounded-sm px-1 font-normal lg:hidden">
						{selectedValues.size}
					</Badge>
					<div class="hidden space-x-1 lg:flex">
						{#if selectedValues.size > 2}
							<Badge variant="secondary" class="rounded-sm px-1 font-normal">
								{m.common_selected_count({ count: selectedValues.size })}
							</Badge>
						{:else}
							{#each options.filter((opt) => selectedValues.has(opt.value)) as option (option)}
								<Badge variant="secondary" class="rounded-sm px-1 font-normal">
									{option.label}
								</Badge>
							{/each}
						{/if}
					</div>
				{/if}
			</Button>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-[240px] p-0" align="start" data-testid={`facet-${title.toLowerCase()}-content`}>
		<Command.Root class="rounded-none bg-transparent">
			<Command.Input placeholder={title} />
			<Command.List>
				<Command.Empty>{m.common_no_results_found()}</Command.Empty>
				<Command.Group>
					{#each options as option (option)}
						{@const isSelected = selectedValues.has(option.value)}
						<Command.Item
							data-testid={`facet-${title.toLowerCase()}-option-${String(option.value)}`}
							onSelect={() => {
								if (isSelected) selectedValues.delete(option.value);
								else selectedValues.add(option.value);
								const filterValues = Array.from(selectedValues);
								column?.setFilterValue(filterValues.length ? filterValues : undefined);
							}}
							class="gap-2"
						>
							{#if showCheckboxes}
								<div
									class={cn(
										'border-primary flex size-4 shrink-0 items-center justify-center rounded-sm border',
										isSelected ? 'bg-primary text-primary-foreground' : 'opacity-50 [&_svg]:invisible'
									)}
								>
									<CheckIcon class="text-foreground size-6" />
								</div>
							{/if}
							{#if option.icon}
								{@const Icon = option.icon}
								<Icon class="text-muted-foreground shrink-0" />
							{/if}

							<span class="truncate">{option.label}</span>
						</Command.Item>
					{/each}
				</Command.Group>
				{#if selectedValues.size > 0}
					<Command.Separator />
					<Command.Group>
						<Command.Item onselect={() => column?.setFilterValue(undefined)} class="justify-center text-center">
							{m.common_clear_filters()}
						</Command.Item>
					</Command.Group>
				{/if}
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>

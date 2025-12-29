<script lang="ts" generics="TData, TValue">
	import type { Column } from '@tanstack/table-core';
	import { SvelteSet } from 'svelte/reactivity';
	import * as Command from '$lib/components/ui/command/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { cn } from '$lib/utils.js';
	import { Separator } from '$lib/components/ui/separator/index.js';
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
			<ArcaneButton
				{...props}
				action="base"
				tone="ghost"
				size="sm"
				icon={FilterIcon}
				customLabel={title}
				class="border-input hover:bg-card/60 h-8 border border-dashed hover:text-inherit"
				data-testid={`facet-${title.toLowerCase()}-trigger`}
			>
				{#if selectedValues.size > 0}
					<Separator orientation="vertical" class="mx-1 h-4" />
					<div class="text-muted-foreground flex items-center gap-1 text-xs font-medium">
						{#if selectedValues.size > 2}
							<span>{selectedValues.size}</span>
						{:else}
							{#each options.filter((opt) => selectedValues.has(opt.value)) as option, i (option)}
								{#if i > 0}
									<span class="opacity-50">,</span>
								{/if}
								<span>{option.label}</span>
							{/each}
						{/if}
					</div>
				{/if}
			</ArcaneButton>
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

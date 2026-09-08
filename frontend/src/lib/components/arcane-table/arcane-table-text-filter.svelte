<script lang="ts">
	import * as Popover from '#lib/components/ui/popover/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages.js';
	import { FilterIcon } from '#lib/icons/index.js';

	type FilterableColumn = {
		getFilterValue: () => unknown;
		setFilterValue: (value: unknown) => void;
	};

	let {
		column,
		title,
		placeholder
	}: {
		column: FilterableColumn;
		title: string;
		placeholder: string;
	} = $props();

	let open = $state(false);
	const currentValue = $derived(String(column?.getFilterValue() ?? ''));
	let draft = $derived(currentValue);

	function apply() {
		const value = draft.trim();
		if (value === currentValue) return;
		column?.setFilterValue(value || undefined);
	}
</script>

<Popover.Root
	bind:open
	onOpenChange={(next) => {
		if (!next) apply();
	}}
>
	<Popover.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{...props}
				action="base"
				tone="ghost"
				size="sm"
				icon={FilterIcon}
				customLabel={title}
				class={cn(
					'h-8 border border-dashed border-input hover:bg-card/60 hover:text-inherit',
					currentValue && 'border-solid border-primary/40 bg-primary/10 text-foreground'
				)}
				data-testid={`facet-${title.toLowerCase()}-trigger`}
			>
				{#if currentValue}
					<Separator orientation="vertical" class="mx-1 h-4" />
					<span class="max-w-40 truncate text-xs font-medium text-muted-foreground">{currentValue}</span>
				{/if}
			</ArcaneButton>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content class="w-[240px] p-2" align="start" data-testid={`facet-${title.toLowerCase()}-content`}>
		<div class="flex flex-col gap-2">
			<Input
				{placeholder}
				bind:value={draft}
				onkeydown={(e) => {
					if (e.key !== 'Enter') return;
					apply();
					open = false;
				}}
				class="h-8"
			/>
			{#if currentValue}
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					customLabel={m.common_clear_filters()}
					onclick={() => column?.setFilterValue(undefined)}
					class="h-8 justify-center"
				/>
			{/if}
		</div>
	</Popover.Content>
</Popover.Root>

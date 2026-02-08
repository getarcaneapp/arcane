<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button';
	import * as Command from '$lib/components/ui/command';
	import * as Popover from '$lib/components/ui/popover';
	import { Spinner } from '$lib/components/ui/spinner';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { CheckIcon, ArrowDownIcon } from '$lib/icons';
	import { tick } from 'svelte';
	import type { FormEventHandler } from 'svelte/elements';
	import type { Action, ArcaneButtonSize, ArcaneButtonTone } from '$lib/components/arcane-button';

	type SearchableSelectItem = {
		value: string;
		label: string;
		disabled?: boolean;
		hint?: string;
		selected?: boolean;
	};

	let {
		items,
		value = $bindable(''),
		displayText,
		onSelect,
		oninput,
		isLoading,
		disableSearch = false,
		selectText = m.common_select_option(),
		emptyText = m.common_no_results_found(),
		closeOnSelect = true,
		triggerId,
		placeholder,
		disabled = false,
		action = 'base',
		tone,
		variant = 'outline',
		size = 'sm',
		contentClass,
		commandClass,
		inputClass,
		listClass,
		itemClass,
		showCheckboxes = true,
		class: className
	}: {
		items: SearchableSelectItem[];
		value?: string;
		displayText?: string;
		oninput?: FormEventHandler<HTMLInputElement>;
		onSelect?: (value: string) => void;
		isLoading?: boolean;
		disableSearch?: boolean;
		selectText?: string;
		emptyText?: string;
		closeOnSelect?: boolean;
		triggerId?: string;
		placeholder?: boolean;
		disabled?: boolean;
		action?: Action;
		tone?: ArcaneButtonTone;
		variant?: 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link';
		size?: ArcaneButtonSize;
		contentClass?: string;
		commandClass?: string;
		inputClass?: string;
		listClass?: string;
		itemClass?: string;
		showCheckboxes?: boolean;
		class?: string;
	} = $props();

	let open = $state(false);
	let filteredItems = $state<SearchableSelectItem[]>([]);

	const resolvedLabel = $derived.by(() => {
		if (displayText) return displayText;
		const match = items.find((item) => item.value === value);
		return match?.label ?? selectText;
	});

	const isPlaceholder = $derived.by(() => {
		if (placeholder !== undefined) return placeholder;
		if (displayText) return false;
		return !value;
	});

	const resolvedTone = $derived.by(() => {
		if (tone) return tone;
		switch (variant) {
			case 'ghost':
				return 'ghost';
			case 'link':
				return 'link';
			case 'destructive':
				return 'outline-destructive';
			case 'default':
				return 'outline-primary';
			case 'secondary':
				return 'outline';
			case 'outline':
			default:
				return 'outline';
		}
	});

	const isCompact = $derived(size === 'sm' || size === 'icon');

	function closeAndFocusTrigger() {
		if (!closeOnSelect) return;
		open = false;
		if (!triggerId) return;
		tick().then(() => {
			document.getElementById(triggerId)?.focus();
		});
	}

	function filterItems(searchString: string) {
		if (!searchString) {
			filteredItems = items;
			return;
		}
		filteredItems = items.filter((item) => item.label.toLowerCase().includes(searchString.toLowerCase()));
	}

	$effect(() => {
		filteredItems = items;
	});

	$effect(() => {
		if (open) {
			filteredItems = items;
		}
	});
</script>

<Popover.Root bind:open>
	<Popover.Trigger>
		{#snippet child({ props })}
			<ArcaneButton
				{action}
				tone={resolvedTone}
				{size}
				role="combobox"
				aria-expanded={open}
				{...props}
				{disabled}
				id={triggerId}
				class={cn(
					'[&>span]:flex [&>span]:w-full [&>span]:items-center [&>span]:justify-between',
					isCompact && 'h-8 px-2.5 text-sm',
					isPlaceholder && 'text-muted-foreground',
					className
				)}
			>
				<span class="min-w-0 flex-1 truncate text-left">{resolvedLabel}</span>
				<ArrowDownIcon class="ml-2 size-3 shrink-0 opacity-50" />
			</ArcaneButton>
		{/snippet}
	</Popover.Trigger>
	<Popover.Content
		class={cn(
			'bg-popover text-popover-foreground backdrop-blur-0 w-[var(--bits-popover-anchor-width)] p-0 backdrop-saturate-100',
			contentClass
		)}
	>
		<Command.Root shouldFilter={false} class={cn('rounded-none bg-transparent', commandClass)}>
			{#if !disableSearch}
				<Command.Input
					placeholder={m.common_search()}
					class={cn(inputClass)}
					oninput={(e) => {
						filterItems(e.currentTarget.value);
						oninput?.(e);
					}}
				/>
			{/if}
			<Command.Empty>
				{#if isLoading}
					<div class="flex w-full justify-center">
						<Spinner class="size-4" />
					</div>
				{:else}
					{emptyText}
				{/if}
			</Command.Empty>
			<Command.List class={cn(listClass)}>
				<Command.Group>
					{#each filteredItems as item (item.value)}
						{@const isSelected = item.selected ?? item.value === value}
						<Command.Item
							value={item.value}
							disabled={item.disabled}
							class={cn(itemClass)}
							onSelect={() => {
								if (item.disabled) return;
								onSelect?.(item.value);
								closeAndFocusTrigger();
							}}
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
							{:else}
								<CheckIcon class={cn('mr-2 size-4', isSelected ? 'opacity-100' : 'opacity-0')} />
							{/if}
							<span class={cn('min-w-0 flex-1 truncate', item.disabled && 'text-muted-foreground')} title={item.label}>
								{item.label}
							</span>
							{#if item.hint}
								<span class="text-muted-foreground ml-auto text-xs">{item.hint}</span>
							{/if}
						</Command.Item>
					{/each}
				</Command.Group>
			</Command.List>
		</Command.Root>
	</Popover.Content>
</Popover.Root>

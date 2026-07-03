<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import * as Select from '$lib/components/ui/select/index.js';
	import { m } from '$lib/paraglide/messages';

	let {
		id,
		name,
		value = $bindable<string>(),
		label,
		description,
		error,
		disabled = false,
		placeholder = m.common_select_option(),
		options = [],
		groupLabel,
		hideLabel = false,
		triggerClass = 'w-full',
		triggerSize = 'default',
		onValueChange
	}: {
		id: string;
		name?: string;
		value: string;
		label: string;
		description?: string;
		error?: string | null;
		disabled?: boolean;
		placeholder?: string;
		options: { label: string; value: string; description?: string }[];
		groupLabel?: string;
		hideLabel?: boolean;
		triggerClass?: string;
		triggerSize?: 'sm' | 'default';
		onValueChange?: (value: string) => void;
	} = $props();

	const selectedLabel = $derived(options.find((o) => o.value === value)?.label ?? placeholder);
</script>

{#snippet optionItems()}
	{#each options as option (option.value)}
		<Select.Item value={option.value}>
			<div class="flex flex-col items-start gap-1">
				<span class="font-medium">{option.label}</span>
				{#if option.description}
					<span class="text-muted-foreground text-xs">{option.description}</span>
				{/if}
			</div>
		</Select.Item>
	{/each}
{/snippet}

<div class="space-y-2">
	{#if hideLabel}
		<Label for={id} class="sr-only">
			{label}
		</Label>
	{:else}
		<div>
			<Label for={id} class="text-sm font-medium">
				{label}
			</Label>
			{#if description}
				<p class="text-muted-foreground mt-0.5 text-xs">{description}</p>
			{/if}
		</div>
	{/if}

	<Select.Root type="single" bind:value {name} {disabled} onValueChange={(v) => onValueChange?.(v)}>
		<Select.Trigger size={triggerSize} class="{triggerClass} {error ? 'border-destructive' : ''}" {id}>
			<span>{selectedLabel}</span>
		</Select.Trigger>

		<Select.Content>
			{#if groupLabel}
				<Select.Group>
					<Select.Label>{groupLabel}</Select.Label>
					{@render optionItems()}
				</Select.Group>
			{:else}
				{@render optionItems()}
			{/if}
		</Select.Content>
	</Select.Root>

	{#if error}
		<p class="text-destructive text-xs font-medium">{error}</p>
	{/if}
</div>

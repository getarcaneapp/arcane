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
		onValueChange?: (value: string) => void;
	} = $props();

	const selectedLabel = $derived(options.find((o) => o.value === value)?.label ?? placeholder);
</script>

<div class="space-y-2">
	<div>
		<Label for={id} class="text-sm font-medium">
			{label}
		</Label>
		{#if description}
			<p class="text-muted-foreground mt-0.5 text-xs">{description}</p>
		{/if}
	</div>

	<Select.Root type="single" bind:value {name} {disabled} onValueChange={(v) => onValueChange?.(v)}>
		<Select.Trigger class="w-full {error ? 'border-destructive' : ''}" {id}>
			<span>{selectedLabel}</span>
		</Select.Trigger>

		<Select.Content>
			{#if groupLabel}
				<Select.Group>
					<Select.Label>{groupLabel}</Select.Label>
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
				</Select.Group>
			{:else}
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
			{/if}
		</Select.Content>
	</Select.Root>

	{#if error}
		<p class="text-destructive text-xs font-medium">{error}</p>
	{/if}
</div>

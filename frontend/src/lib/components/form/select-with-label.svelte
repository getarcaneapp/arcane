<script lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { m } from '#lib/paraglide/messages.js';

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
		compact = false,
		triggerSize = 'default',
		'aria-describedby': ariaDescribedby,
		'aria-errormessage': ariaErrormessage,
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
		options: { label: string; value: string; description?: string; badge?: string }[];
		groupLabel?: string;
		hideLabel?: boolean;
		compact?: boolean;
		triggerSize?: 'sm' | 'default';
		'aria-describedby'?: string;
		'aria-errormessage'?: string;
		onValueChange?: (value: string) => void;
	} = $props();

	const uid = $props.id();
	const selected = $derived(options.find((o) => o.value === value));
	const describedBy = $derived(
		[ariaDescribedby, description && !hideLabel && `${uid}-description`, error && `${uid}-error`].filter(Boolean).join(' ') ||
			undefined
	);
</script>

{#snippet optionItems()}
	{#each options as option (option.value)}
		<Select.Item value={option.value}>
			<div class="flex flex-col items-start gap-1">
				<span class="flex items-center gap-2 font-medium">
					{option.label}
					{#if option.badge}
						<Badge variant="purple" size="xs" class="relative -top-px">{option.badge}</Badge>
					{/if}
				</span>
				{#if option.description}
					<span class="text-xs text-muted-foreground">{option.description}</span>
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
			<Label for={id}>
				{label}
			</Label>
			{#if description}
				<p id="{uid}-description" class="mt-0.5 text-xs text-muted-foreground">{description}</p>
			{/if}
		</div>
	{/if}

	<Select.Root type="single" bind:value {name} {disabled} onValueChange={(v) => onValueChange?.(v)}>
		<Select.Trigger
			size={triggerSize}
			class={compact ? 'w-40' : 'w-full'}
			aria-invalid={!!error || !!ariaErrormessage}
			aria-describedby={describedBy}
			aria-errormessage={error ? `${uid}-error` : ariaErrormessage}
			{id}
		>
			<span class="flex items-center gap-2">
				{selected?.label ?? placeholder}
				{#if selected?.badge}
					<Badge variant="purple" size="xs" class="relative -top-px">{selected.badge}</Badge>
				{/if}
			</span>
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
		<p id="{uid}-error" class="text-xs font-medium text-destructive">{error}</p>
	{/if}
</div>

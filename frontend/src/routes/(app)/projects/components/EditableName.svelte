<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { tick } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { InfoIcon, EditIcon } from '$lib/icons';

	let {
		value = $bindable(),
		displayValue,
		ref = $bindable(),
		variant = 'block',
		error,
		originalValue,
		canEdit = true,
		onCommit,
		placeholder = '',
		disabledMessage = m.compose_name_change_not_allowed(),
		class: className = ''
	}: {
		value: string;
		displayValue?: string;
		ref: HTMLInputElement | null;
		variant?: 'block' | 'inline';
		error?: string;
		originalValue: string;
		canEdit?: boolean;
		onCommit?: () => void;
		placeholder?: string;
		disabledMessage?: string;
		class?: string;
	} = $props();

	let shownValue = $derived(displayValue ?? value);

	let isEditing = $state(false);
	let wrapperClass = $derived(cn('group', variant === 'block' ? 'w-full' : 'min-w-0', className));

	async function beginEdit() {
		if (!canEdit) return;
		isEditing = true;
		await tick();
		ref?.focus();
	}
</script>

<div class={wrapperClass}>
	{#if isEditing}
		{#if variant === 'block'}
			<Input
				bind:ref
				bind:value
				{placeholder}
				class="h-8 max-w-[280px] min-w-[120px] px-2 text-left text-base font-semibold {error ? 'border-destructive' : ''}"
				autofocus
				onkeydown={(e) => {
					if (e.key === 'Enter') {
						e.preventDefault();
						onCommit?.();
						isEditing = false;
					}
					if (e.key === 'Escape') {
						value = originalValue;
						isEditing = false;
					}
				}}
				onblur={() => {
					if (!isEditing) return;
					onCommit?.();
					isEditing = false;
				}}
				disabled={!canEdit}
			/>
		{:else}
			<Input
				bind:ref
				bind:value
				{placeholder}
				class="h-8 max-w-[360px] px-2 text-lg font-semibold {error ? 'border-destructive' : ''}"
				autofocus
				onkeydown={(e) => {
					if (e.key === 'Enter') {
						e.preventDefault();
						onCommit?.();
						isEditing = false;
					}
					if (e.key === 'Escape') {
						value = originalValue;
						isEditing = false;
					}
				}}
				onblur={() => {
					if (!isEditing) return;
					onCommit?.();
					isEditing = false;
				}}
				disabled={!canEdit}
			/>
		{/if}
	{:else if variant === 'block'}
		<h1 class="m-0 w-full">
			<button
				type="button"
				class="hover:bg-muted/50 focus:ring-ring min-h-[32px] w-full rounded bg-transparent px-1 py-1 text-center text-base font-semibold transition-colors focus:ring-2 focus:ring-offset-2 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50 {error
					? 'border border-destructive'
					: ''}"
				title={canEdit ? `${shownValue || placeholder} (tap to edit)` : shownValue || placeholder}
				onclick={beginEdit}
				disabled={!canEdit}
			>
				<span class="block truncate {!shownValue && placeholder ? 'text-muted-foreground' : ''}">{shownValue || placeholder}</span
				>
			</button>
		</h1>

		{#if canEdit}
			<div class="flex items-center justify-center">
				<span class="text-muted-foreground flex items-center gap-0.5 text-[8px] whitespace-nowrap opacity-30">
					<EditIcon class="size-1.5" />
					{m.tap_to_edit()}
				</span>
			</div>
		{:else}
			<div class="flex items-center justify-center">
				<span class="text-muted-foreground inline-flex cursor-help items-center gap-0.5 text-[8px] whitespace-nowrap opacity-40">
					<InfoIcon class="size-1.5" />
					{m.cannot_edit()}
				</span>
			</div>
		{/if}
	{:else}
		<div class="flex h-8 min-w-0 items-center gap-1.5">
			<h1 class="m-0 max-w-[360px] min-w-0">
				<button
					type="button"
					class={cn(
						'w-full truncate bg-transparent px-0 py-0 text-left text-lg font-semibold',
						!shownValue && placeholder && 'text-muted-foreground',
						error && 'rounded border border-destructive px-1'
					)}
					title={shownValue || placeholder}
					onclick={beginEdit}
					disabled={!canEdit}
				>
					{shownValue || placeholder}
				</button>
			</h1>
			{#if canEdit}
				<ArcaneButton
					action="base"
					tone="ghost"
					size="icon"
					class="pointer-events-none size-6 opacity-0 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100"
					customLabel="Edit name"
					showLabel={false}
					icon={EditIcon}
					onclick={beginEdit}
				/>
			{:else}
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger>
						<span class="text-muted-foreground inline-flex cursor-help items-center">
							<InfoIcon class="size-4 shrink-0" />
						</span>
					</ArcaneTooltip.Trigger>
					<ArcaneTooltip.Content>
						{disabledMessage}
					</ArcaneTooltip.Content>
				</ArcaneTooltip.Root>
			{/if}
		</div>
	{/if}
</div>

{#if error}
	<p class="text-destructive mt-1 text-xs">{error}</p>
{/if}

<script lang="ts">
	import { Input } from '$lib/components/ui/input/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import InfoIcon from '@lucide/svelte/icons/info';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import { tick } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';

	let {
		value = $bindable(),
		ref = $bindable(),
		variant = 'block',
		error,
		originalValue,
		canEdit = true,
		onCommit,
		placeholder = '',
		class: className = ''
	}: {
		value: string;
		ref: HTMLInputElement | null;
		variant?: 'block' | 'inline';
		error?: string;
		originalValue: string;
		canEdit?: boolean;
		onCommit?: () => void;
		placeholder?: string;
		class?: string;
	} = $props();

	let isEditing = $state(false);

	async function beginEdit() {
		if (!canEdit) return;
		isEditing = true;
		await tick();
		ref?.focus();
	}
</script>

<div class={cn('group w-full', className)}>
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
				class="hover:bg-muted/50 focus:ring-ring min-h-[32px] w-full rounded bg-transparent px-1 py-1 text-center text-base font-semibold transition-colors focus:ring-2 focus:ring-offset-2 focus:outline-none disabled:cursor-not-allowed disabled:opacity-50"
				title={canEdit ? `${value || placeholder} (tap to edit)` : value || placeholder}
				onclick={beginEdit}
				disabled={!canEdit}
			>
				<span class="block truncate {!value && placeholder ? 'text-muted-foreground' : ''}">{value || placeholder}</span>
			</button>
		</h1>

		{#if canEdit}
			<div class="flex items-center justify-center">
				<span class="text-muted-foreground flex items-center gap-0.5 text-[8px] whitespace-nowrap opacity-30">
					<PencilIcon class="size-1.5" />
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
		<div class="flex items-center gap-1">
			<h1 class="m-0 max-w-[360px]">
				<button
					type="button"
					class="w-full truncate bg-transparent px-0 py-0 text-left text-lg leading-none font-semibold {!value && placeholder
						? 'text-muted-foreground'
						: ''}"
					title={value || placeholder}
					onclick={beginEdit}
					disabled={!canEdit}
				>
					{value || placeholder}
				</button>
			</h1>
			{#if canEdit}
				<Button
					variant="ghost"
					size="icon"
					class="size-6 opacity-0 transition-opacity group-hover:opacity-100"
					aria-label="Edit name"
					title="Edit name"
					onclick={beginEdit}
				>
					<PencilIcon class="size-3.5" />
				</Button>
			{:else}
				<Tooltip.Provider>
					<Tooltip.Root>
						<Tooltip.Trigger>
							<span class="text-muted-foreground inline-flex cursor-help items-center leading-none">
								<InfoIcon class="relative top-0.5 size-4 shrink-0" />
							</span>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.compose_name_change_not_allowed()}</Tooltip.Content>
					</Tooltip.Root>
				</Tooltip.Provider>
			{/if}
		</div>
	{/if}
</div>

{#if isEditing && error}
	<p class="text-destructive mt-1 text-xs">{error}</p>
{/if}

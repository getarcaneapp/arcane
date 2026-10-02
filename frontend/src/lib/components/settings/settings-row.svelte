<script lang="ts">
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertTriangleIcon } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';
	import type { Snippet } from 'svelte';

	// One setting: label and description on the left, the control on the right.
	// `switch` keeps a compact right-aligned control; `wide` drops the control under the label.
	interface Props {
		label: string;
		description?: string;
		helpText?: string | Snippet;
		/** Caution shown as a small warning alert; use for side effects worth a second look. */
		warningText?: string;
		error?: string | null;
		/** Id of the control, so the label focuses it. */
		for?: string;
		layout?: 'default' | 'switch' | 'wide';
		labelExtra?: Snippet;
		/** Extra content under the row (e.g. fields revealed by a switch), indented. */
		expanded?: Snippet;
		contentClass?: string;
		class?: string;
		children: Snippet;
	}

	let {
		label,
		description,
		helpText,
		warningText,
		error,
		for: htmlFor,
		layout = 'default',
		labelExtra,
		expanded,
		contentClass,
		class: className,
		children
	}: Props = $props();
</script>

{#snippet notes()}
	{#if error}
		<p class="mt-1.5 text-xs font-medium text-destructive">{error}</p>
	{/if}
	{#if typeof helpText === 'function'}
		<div class="mt-1.5 text-xs text-muted-foreground">{@render helpText()}</div>
	{:else if helpText}
		<p class="mt-1.5 text-xs text-muted-foreground">{helpText}</p>
	{/if}
	{#if warningText}
		<Alert.Root variant="warning-subtle" size="sm" class="mt-2 w-fit max-w-full">
			<AlertTriangleIcon class="size-4" />
			<Alert.Description>{warningText}</Alert.Description>
		</Alert.Root>
	{/if}
{/snippet}

<div class={cn('flex flex-col gap-4 px-5 py-4', className)}>
	<div
		class={cn(
			'grid gap-x-8 gap-y-2',
			layout === 'default' && 'md:grid-cols-settings-row md:items-start',
			layout === 'switch' && 'grid-cols-content-action items-center'
		)}
	>
		<div class="min-w-0">
			<Label for={htmlFor}>{label}</Label>
			{#if description}
				<p class="mt-0.5 text-xs text-muted-foreground">{description}</p>
			{/if}
			{#if labelExtra}
				{@render labelExtra()}
			{/if}
			{#if layout === 'switch'}
				{@render notes()}
			{/if}
		</div>
		<div class={cn('min-w-0', layout === 'switch' && 'flex justify-end', contentClass)}>
			{@render children()}
			{#if layout !== 'switch'}
				{@render notes()}
			{/if}
		</div>
	</div>
	{#if expanded}
		<div class="flex flex-col gap-4 border-l-2 border-border/60 pl-5">
			{@render expanded()}
		</div>
	{/if}
</div>

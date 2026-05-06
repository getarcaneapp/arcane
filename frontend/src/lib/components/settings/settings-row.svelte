<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import { cn } from '$lib/utils';
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		description?: string;
		helpText?: string | Snippet;
		labelExtra?: Snippet;
		children: Snippet;
		contentClass?: string;
		layout?: 'stacked' | 'inline';
	}

	let { label, description, helpText, labelExtra, children, contentClass, layout = 'stacked' }: Props = $props();
</script>

{#if layout === 'inline'}
	<div class="flex items-start justify-between gap-6">
		<div class="min-w-0 flex-1">
			<Label class="text-sm font-medium">{label}</Label>
			{#if description}
				<p class="text-muted-foreground mt-0.5 text-xs">{description}</p>
			{/if}
			{#if typeof helpText === 'function'}
				{@render helpText()}
			{:else if helpText}
				<p class="text-muted-foreground mt-1 text-xs">{helpText}</p>
			{/if}
			{#if labelExtra}
				{@render labelExtra()}
			{/if}
		</div>
		<div class={cn('shrink-0', contentClass)}>
			{@render children()}
		</div>
	</div>
{:else}
	<div class="space-y-2">
		<div>
			<Label class="text-sm font-medium">{label}</Label>
			{#if description}
				<p class="text-muted-foreground mt-0.5 text-xs">{description}</p>
			{/if}
			{#if typeof helpText === 'function'}
				{@render helpText()}
			{:else if helpText}
				<p class="text-muted-foreground mt-1 text-xs">{helpText}</p>
			{/if}
			{#if labelExtra}
				{@render labelExtra()}
			{/if}
		</div>
		<div class={cn('pt-1', contentClass)}>
			{@render children()}
		</div>
	</div>
{/if}

<script lang="ts">
	import { Label } from '$lib/components/ui/label';
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		description?: string;
		helpText?: string | Snippet;
		labelExtra?: Snippet;
		children: Snippet;
		contentClass?: string;
	}

	let { label, description, helpText, labelExtra, children, contentClass }: Props = $props();
</script>

<div class="grid gap-4 md:grid-cols-[1fr_1.5fr] md:gap-8">
	<div>
		<Label class="text-base">{label}</Label>
		{#if description}
			<p class="text-muted-foreground mt-1 text-sm">{description}</p>
		{/if}
		{#if typeof helpText === 'function'}
			{@render helpText()}
		{:else if helpText}
			<p class="text-muted-foreground mt-2 text-xs">{helpText}</p>
		{/if}
		{#if labelExtra}
			{@render labelExtra()}
		{/if}
	</div>
	<div class={contentClass}>
		{@render children()}
	</div>
</div>

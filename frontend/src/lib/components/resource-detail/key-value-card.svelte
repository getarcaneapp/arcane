<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		children: Snippet;
		labelClass?: string;
		valueClass?: string;
		valueTitle?: string;
		cardClass?: string;
		contentClass?: string;
		variant?: 'default' | 'subtle' | 'outlined';
	}

	let {
		label,
		children,
		labelClass = 'text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase',
		valueClass = 'text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all',
		valueTitle,
		cardClass,
		contentClass,
		variant = 'subtle'
	}: Props = $props();
</script>

{#snippet content()}
	<Card.Content class={contentClass ?? 'flex flex-col gap-2 p-4'}>
		<div class={labelClass}>{label}</div>
		<div class={valueClass} title={valueTitle}>
			{@render children()}
		</div>
	</Card.Content>
{/snippet}

<Card.Root {variant} class={cardClass}>
	{@render content()}
</Card.Root>

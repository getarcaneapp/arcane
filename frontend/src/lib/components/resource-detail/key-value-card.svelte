<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import type { Snippet } from 'svelte';

	interface Props {
		label: string;
		children: Snippet;
		labelClass?: string;
		valueClass?: string;
		valueTitle?: string;
		cardClass?: string;
	}

	let {
		label,
		children,
		labelClass = 'text-muted-foreground text-xs font-semibold tracking-wide break-all uppercase',
		valueClass = 'text-foreground cursor-pointer font-mono text-sm font-medium break-all select-all',
		valueTitle,
		cardClass
	}: Props = $props();
</script>

{#snippet content()}
	<Card.Content class="flex flex-col gap-2 p-4">
		<div class={labelClass}>{label}</div>
		<div class={valueClass} title={valueTitle}>
			{@render children()}
		</div>
	</Card.Content>
{/snippet}

<Card.Root variant="subtle" class={cardClass}>
	{@render content()}
</Card.Root>

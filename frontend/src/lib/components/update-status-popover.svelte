<script lang="ts">
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import type { Snippet } from 'svelte';

	type TooltipSide = 'top' | 'right' | 'bottom' | 'left';

	interface Props {
		open?: boolean;
		interactive?: boolean;
		directTrigger?: boolean;
		side?: TooltipSide;
		contentClass?: string;
		trigger: Snippet<[{ props: Record<string, unknown> }]>;
		content: Snippet;
	}

	let {
		open = $bindable(false),
		interactive = false,
		directTrigger = false,
		side = 'right',
		contentClass = 'max-w-[280px] p-0',
		trigger,
		content
	}: Props = $props();
</script>

<ArcaneTooltip.Root bind:open {interactive}>
	{#if directTrigger}
		<ArcaneTooltip.Trigger>
			{#snippet child({ props })}
				{@render trigger({ props })}
			{/snippet}
		</ArcaneTooltip.Trigger>
	{:else}
		<ArcaneTooltip.Trigger>
			{@render trigger({ props: {} })}
		</ArcaneTooltip.Trigger>
	{/if}
	<ArcaneTooltip.Content {side} class={contentClass} data-open={open ? 'true' : 'false'}>
		{@render content()}
	</ArcaneTooltip.Content>
</ArcaneTooltip.Root>

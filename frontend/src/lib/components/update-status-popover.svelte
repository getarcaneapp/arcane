<script lang="ts">
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import type { Snippet } from 'svelte';
	import { cn } from '#lib/utils.js';

	type TooltipSide = 'top' | 'right' | 'bottom' | 'left';

	interface Props {
		open?: boolean;
		interactive?: boolean;
		directTrigger?: boolean;
		side?: TooltipSide;
		contentWidth?: 'sm' | 'md' | 'lg' | 'xl';
		trigger: Snippet<[{ props: Record<string, unknown> }]>;
		content: Snippet;
	}

	let {
		open = $bindable(false),
		interactive = false,
		directTrigger = false,
		side = 'right',
		contentWidth = 'lg',
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
	<ArcaneTooltip.Content
		{side}
		variant="panel"
		class={cn(
			contentWidth === 'sm' && 'max-w-55',
			contentWidth === 'md' && 'max-w-60',
			contentWidth === 'lg' && 'max-w-70',
			contentWidth === 'xl' && 'max-w-80'
		)}
		data-open={open ? 'true' : 'false'}
	>
		{@render content()}
	</ArcaneTooltip.Content>
</ArcaneTooltip.Root>

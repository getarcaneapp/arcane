<script lang="ts">
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import type { Snippet } from 'svelte';

	type TooltipSide = 'top' | 'right' | 'bottom' | 'left';

	interface Props {
		open?: boolean;
		interactive?: boolean;
		side?: TooltipSide;
		contentClass?: string;
		trigger: Snippet;
		content: Snippet;
	}

	let {
		open = $bindable(false),
		interactive = false,
		side = 'right',
		contentClass = 'max-w-[280px] p-0',
		trigger,
		content
	}: Props = $props();
</script>

<ArcaneTooltip.Root bind:open {interactive}>
	<ArcaneTooltip.Trigger>
		{@render trigger()}
	</ArcaneTooltip.Trigger>
	<ArcaneTooltip.Content {side} class={contentClass} data-open={open ? 'true' : 'false'}>
		{@render content()}
	</ArcaneTooltip.Content>
</ArcaneTooltip.Root>

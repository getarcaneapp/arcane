<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { getArcaneTooltipContext } from './context.svelte.js';
	import type { Snippet } from 'svelte';

	let {
		children,
		side = 'top',
		align = 'center',
		class: className,
		...restProps
	}: {
		children?: Snippet;
		side?: 'top' | 'right' | 'bottom' | 'left';
		align?: 'start' | 'center' | 'end';
		class?: string;
	} = $props();

	const ctx = getArcaneTooltipContext();
</script>

{#if ctx.isTouch}
	<Popover.Content {side} {align} class={className} {...restProps}>
		{@render children?.()}
	</Popover.Content>
{:else}
	<Tooltip.Content {side} {align} class={className} {...restProps}>
		{@render children?.()}
	</Tooltip.Content>
{/if}

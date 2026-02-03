<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { getArcaneTooltipContext } from './context.svelte.js';
	import { cn, type WithoutChildrenOrChild } from '$lib/utils.js';
	import type { ComponentProps, Snippet } from 'svelte';

	export type ArcaneTooltipContentProps = WithoutChildrenOrChild<ComponentProps<typeof Tooltip.Content>>;

	let {
		ref = $bindable(null),
		children,
		class: className,
		sideOffset = 0,
		side = 'top',
		arrowClasses,
		portalProps,
		...restProps
	}: ArcaneTooltipContentProps & {
		children?: Snippet;
	} = $props();

	const ctx = getArcaneTooltipContext();
</script>

{#if ctx.isTouch}
	<Popover.Content
		bind:ref
		{sideOffset}
		{side}
		class={cn(
			'bg-popover/90 border-border/50 w-fit max-w-[min(calc(100vw-2rem),320px)] px-3 py-1.5 text-xs text-balance shadow-lg backdrop-blur-md',
			className
		)}
		{...restProps}
	>
		{@render children?.()}
	</Popover.Content>
{:else}
	<Tooltip.Content bind:ref {sideOffset} {side} class={className} {arrowClasses} {portalProps} {...restProps}>
		{@render children?.()}
	</Tooltip.Content>
{/if}

<script lang="ts">
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import * as Popover from '#lib/components/ui/popover/index.js';
	import { getArcaneTooltipContext } from './context.svelte.js';
	import type { WithoutChildrenOrChild } from '#lib/utils.js';
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
		variant = 'default',
		...restProps
	}: ArcaneTooltipContentProps & {
		children?: Snippet;
		/** `panel` drops padding for rich content that brings its own layout. */
		variant?: 'default' | 'panel';
	} = $props();

	const ctx = getArcaneTooltipContext();
</script>

{#if ctx.isTouch}
	<Popover.Content
		bind:ref
		{sideOffset}
		{side}
		variant={variant === 'panel' ? 'panel' : 'tooltip'}
		class={className}
		{...restProps}
	>
		{@render children?.()}
	</Popover.Content>
{:else}
	<Tooltip.Content bind:ref {sideOffset} {side} {variant} class={className} {arrowClasses} {portalProps} {...restProps}>
		{@render children?.()}
	</Tooltip.Content>
{/if}

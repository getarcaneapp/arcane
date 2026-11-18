<script lang="ts">
	import * as Popover from '@arcane/ui/popover';
	import * as Tooltip from '@arcane/ui/tooltip';
	import type { WithoutChildrenOrChild } from '@arcane/ui/utils';
	import type { ComponentProps, Snippet } from 'svelte';

	import { getArcaneTooltipContext } from './context.svelte.js';

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

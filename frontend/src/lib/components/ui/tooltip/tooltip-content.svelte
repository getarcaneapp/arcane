<script lang="ts">
	import { Tooltip as TooltipPrimitive } from 'bits-ui';
	import type { ComponentProps } from 'svelte';

	import { cn } from '#lib/utils.js';
	import type { WithoutChildrenOrChild } from '#lib/utils.js';

	import TooltipPortal from './tooltip-portal.svelte';

	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 6,
		side = 'top',
		children,
		arrowClasses,
		portalProps,
		variant = 'default',
		...restProps
	}: TooltipPrimitive.ContentProps & {
		/** `panel` drops padding for rich content that brings its own layout. */
		variant?: 'default' | 'panel';
		arrowClasses?: string;
		portalProps?: WithoutChildrenOrChild<ComponentProps<typeof TooltipPortal>>;
	} = $props();
</script>

<TooltipPortal {...portalProps}>
	<TooltipPrimitive.Content
		bind:ref
		data-slot="tooltip-content"
		{sideOffset}
		{side}
		class={cn(
			'z-(--arcane-z-surface) w-max max-w-(--max-width-tooltip) origin-(--bits-tooltip-content-transform-origin) rounded-md border border-border/40 bg-popover/90 px-2.5 py-1.5 text-center text-xs wrap-break-word text-popover-foreground shadow-lg backdrop-blur-md backdrop-saturate-150 fade-in-0 zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=delayed-open]:animate-in data-[state=instant-open]:animate-in dark:bg-popover/20',
			variant === 'panel' && 'p-0 text-left',
			className
		)}
		{...restProps}
	>
		{@render children?.()}
		<TooltipPrimitive.Arrow class={cn('fill-popover/90 dark:fill-popover/60', arrowClasses)} />
	</TooltipPrimitive.Content>
</TooltipPortal>

<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { Popover as PopoverPrimitive } from 'bits-ui';

	let {
		ref = $bindable(null),
		class: className,
		sideOffset = 4,
		size = 'default',
		variant = 'default',
		align = 'center',
		portalProps,
		children,
		...restProps
	}: PopoverPrimitive.ContentProps & {
		portalProps?: PopoverPrimitive.PortalProps;
		/** `sm` tightens padding for dense option lists. */
		size?: 'default' | 'sm';
		/** `tooltip` matches Tooltip.Content (touch fallback); `panel` drops padding for rich content. */
		variant?: 'default' | 'tooltip' | 'panel';
	} = $props();
</script>

<PopoverPrimitive.Portal {...portalProps}>
	<PopoverPrimitive.Content
		bind:ref
		data-slot="popover-content"
		{sideOffset}
		{align}
		class={cn(
			'z-(--arcane-z-surface) w-72 origin-(--bits-popover-content-transform-origin) overflow-hidden rounded-xl border bg-background/95 p-4 text-popover-foreground shadow-md outline-hidden backdrop-blur-2xl backdrop-saturate-150 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0 dark:bg-popover/20',
			size === 'sm' && 'p-2',
			variant === 'tooltip' &&
				'w-fit max-w-(--max-width-tooltip) border-border/50 bg-popover/90 px-3 py-1.5 text-xs text-balance shadow-lg backdrop-blur-md',
			variant === 'panel' && 'p-0',
			// Command lists and calendars bring their own padding.
			'has-[>[data-slot=calendar]]:p-0 has-[>[data-slot=command]]:p-0',
			className
		)}
		{...restProps}
	>
		<PopoverPrimitive.Arrow />
		{@render children?.()}
	</PopoverPrimitive.Content>
</PopoverPrimitive.Portal>

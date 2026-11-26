<script lang="ts">
	import { Popover as PopoverPrimitive } from 'bits-ui';

	import { cn } from '#lib/utils.js';

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
			'z-(--arcane-z-surface) w-72 origin-(--bits-popover-content-transform-origin) overflow-hidden rounded-lg border border-border/50 bg-popover/75 p-4 wrap-break-word text-popover-foreground shadow-lg outline-hidden backdrop-blur-xl backdrop-saturate-150 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0 dark:bg-popover/40',
			size === 'sm' && 'p-2',
			variant === 'tooltip' &&
				'w-max max-w-(--max-width-tooltip) rounded-md border-border/40 bg-popover/90 px-2.5 py-1.5 text-center text-xs shadow-lg backdrop-blur-md',
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

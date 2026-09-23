<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		scrollable = false,
		...restProps
	}: TabsPrimitive.ListProps & {
		/** Scrolls horizontally with a hidden scrollbar instead of wrapping. */
		scrollable?: boolean;
	} = $props();
</script>

<TabsPrimitive.List
	bind:ref
	class={cn(
		'inline-flex max-w-full flex-wrap items-center justify-start gap-1 rounded-lg border border-border/60 bg-muted/40 p-1 text-muted-foreground',
		scrollable && 'scrollbar-hide overflow-x-auto',
		className
	)}
	{...restProps}
/>

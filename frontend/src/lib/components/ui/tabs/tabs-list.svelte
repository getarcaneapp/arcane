<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';

	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		scrollable = false,
		variant = 'default',
		...restProps
	}: TabsPrimitive.ListProps & {
		/** Scrolls horizontally with a hidden scrollbar instead of wrapping. */
		scrollable?: boolean;
		variant?: 'default' | 'underline';
	} = $props();
</script>

<TabsPrimitive.List
	bind:ref
	class={cn(
		'inline-flex max-w-full flex-wrap items-center justify-start text-muted-foreground',
		variant === 'default' && 'gap-0.5 rounded-xl bg-muted p-1',
		variant === 'underline' && 'w-full gap-5 border-b border-border',
		scrollable && 'scrollbar-hide overflow-x-auto',
		className
	)}
	{...restProps}
/>

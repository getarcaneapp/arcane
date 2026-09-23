<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		size = 'default',
		variant = 'default',
		...restProps
	}: TabsPrimitive.TriggerProps & {
		size?: 'default' | 'sm';
		/** Tints the active state for options with destructive effects. */
		variant?: 'default' | 'destructive';
	} = $props();
</script>

<TabsPrimitive.Trigger
	bind:ref
	class={cn(
		'inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap text-muted-foreground ring-offset-background transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50 data-[state=active]:bg-primary/15 data-[state=active]:text-primary data-[state=active]:ring-1 data-[state=active]:ring-primary/30 dark:data-[state=active]:text-primary-tint',
		size === 'sm' && 'text-xs',
		variant === 'destructive' &&
			'data-[state=active]:bg-destructive/15 data-[state=active]:text-destructive data-[state=active]:shadow-sm data-[state=active]:hover:bg-destructive/10 dark:data-[state=active]:text-destructive',
		className
	)}
	{...restProps}
/>

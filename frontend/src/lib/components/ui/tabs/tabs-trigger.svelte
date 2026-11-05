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
		variant?: 'default' | 'destructive' | 'underline';
	} = $props();
</script>

<TabsPrimitive.Trigger
	bind:ref
	class={cn(
		'inline-flex items-center justify-center gap-1.5 text-sm font-medium whitespace-nowrap text-muted-foreground ring-offset-background transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none disabled:pointer-events-none disabled:opacity-50',
		variant !== 'underline' &&
			'rounded-lg px-3 py-1.5 hover:text-primary-text data-[state=active]:bg-primary/15 data-[state=active]:text-primary-text data-[state=active]:ring-1 data-[state=active]:ring-primary/30',
		variant === 'underline' &&
			'border-b-2 border-transparent pt-1 pb-2 hover:text-foreground data-[state=active]:border-primary data-[state=active]:text-foreground',
		size === 'sm' && 'text-xs',
		variant === 'destructive' &&
			'data-[state=active]:bg-destructive/15 data-[state=active]:text-destructive data-[state=active]:shadow-sm data-[state=active]:hover:bg-destructive/10 dark:data-[state=active]:text-destructive',
		className
	)}
	{...restProps}
/>

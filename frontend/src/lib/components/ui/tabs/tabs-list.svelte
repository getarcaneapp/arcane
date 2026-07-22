<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, ...restProps }: TabsPrimitive.ListProps = $props();
</script>

<TabsPrimitive.List
	bind:ref
	class={cn(
		'inline-flex items-center justify-start gap-1 rounded-lg border border-border/60 bg-muted/40 p-1 text-muted-foreground',
		className
	)}
	{...restProps}
/>

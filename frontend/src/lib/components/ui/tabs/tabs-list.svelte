<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, ...restProps }: TabsPrimitive.ListProps = $props();
</script>

<TabsPrimitive.List
	bind:ref
	class={cn(
		'text-muted-foreground inline-flex h-10 items-center justify-center rounded-full border border-black/5 bg-black/5 p-1 shadow-sm backdrop-blur-xl backdrop-saturate-150 dark:border-white/10 dark:bg-white/5',
		className
	)}
	{...restProps}
/>

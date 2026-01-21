<script lang="ts">
	import { Tabs as TabsPrimitive } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, ...restProps }: TabsPrimitive.ListProps = $props();
</script>

<TabsPrimitive.List
	bind:ref
	class={cn(
		'text-muted-foreground inline-flex h-10 items-center justify-center rounded-lg bg-black/5 p-1 backdrop-blur-lg dark:bg-white/5',
		className
	)}
	{...restProps}
/>

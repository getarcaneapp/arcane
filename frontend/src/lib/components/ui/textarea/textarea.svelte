<script lang="ts">
	import type { WithElementRef, WithoutChildren } from 'bits-ui';
	import type { HTMLTextareaAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		value = $bindable(),
		class: className,
		...restProps
	}: WithoutChildren<WithElementRef<HTMLTextareaAttributes>> = $props();
</script>

<textarea
	bind:this={ref}
	data-slot="textarea"
	class={cn(
		'bg-input/80 ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring focus-visible:bg-input/90 flex min-h-[80px] w-full rounded-lg px-3 py-2 text-base backdrop-blur-sm transition-all focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:backdrop-blur-md focus-visible:outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
		className
	)}
	bind:value
	{...restProps}
></textarea>

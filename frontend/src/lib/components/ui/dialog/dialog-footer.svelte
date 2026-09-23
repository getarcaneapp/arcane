<script lang="ts">
	import { cn, type WithElementRef } from '#lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="dialog-footer"
	class={cn(
		'flex flex-col-reverse gap-2 sm:flex-row sm:justify-end',
		'[[data-sectioned]>&]:border-t [[data-sectioned]>&]:border-border/60 [[data-sectioned]>&]:px-6 [[data-sectioned]>&]:py-4',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>

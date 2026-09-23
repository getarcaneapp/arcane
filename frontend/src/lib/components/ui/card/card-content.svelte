<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<!-- Padding follows the card: compact in `size="sm"` cards, and lined up with a plain (icon-less) header above it. -->
<div
	bind:this={ref}
	data-slot="card-content"
	class={cn(
		'p-4 [[data-slot=card][data-size=sm]>&]:p-3',
		'[[data-slot=card-header][data-variant=plain]~&]:px-6 [[data-slot=card-header][data-variant=plain]~&]:pt-0 [[data-slot=card-header][data-variant=plain]~&]:pb-6',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>

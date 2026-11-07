<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';
	import { trackOverflow } from '#lib/utils/overflow.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLAttributes<HTMLElement>> = $props();

	let moreBelow = $state(false);
</script>

<div
	bind:this={ref}
	data-slot="sidebar-content"
	data-sidebar="content"
	class={cn(
		'mt-1 flex min-h-0 flex-1 scrollbar-visible flex-col gap-0 overflow-x-hidden overflow-y-auto px-3 pb-4 group-data-[collapsible=icon]:gap-3',
		moreBelow && 'scroll-fade-b',
		className
	)}
	{@attach trackOverflow((overflow) => (moreBelow = overflow.below))}
	{...restProps}
>
	{@render children?.()}
</div>

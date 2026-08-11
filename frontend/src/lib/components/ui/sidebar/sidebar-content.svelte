<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLAttributes<HTMLElement>> = $props();
</script>

<div
	bind:this={ref}
	data-slot="sidebar-content"
	data-sidebar="content"
	class={cn('scrollbar-hide flex min-h-0 flex-1 flex-col gap-1 overflow-x-hidden overflow-y-auto', className)}
	{...restProps}
>
	{@render children?.()}
</div>

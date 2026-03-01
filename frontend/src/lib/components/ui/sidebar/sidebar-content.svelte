<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '$lib/utils.js';
	import { useSidebar } from './context.svelte.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLAttributes<HTMLElement>> = $props();

	const sidebar = useSidebar();
</script>

<div
	bind:this={ref}
	data-slot="sidebar-content"
	data-sidebar="content"
	class={cn(
		'scrollbar-hide flex min-h-0 flex-1 flex-col gap-1 overflow-x-hidden overflow-y-auto',
		sidebar.hoverExpansionEnabled &&
			'group-data-[collapsible=icon]:overflow-hidden group-data-[collapsible=icon]:group-data-[hovered=true]:overflow-x-hidden group-data-[collapsible=icon]:group-data-[hovered=true]:overflow-y-auto',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>

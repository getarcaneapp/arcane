<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		collapsed = false,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLElement>> & { collapsed?: boolean } = $props();
</script>

<div
	bind:this={ref}
	data-slot="sidebar-header"
	data-sidebar="header"
	class={cn('flex flex-col gap-2 px-3 pt-4', collapsed && 'px-2', className)}
	{...restProps}
>
	{@render children?.()}
</div>

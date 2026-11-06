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
	data-slot="sidebar-footer"
	data-sidebar="footer"
	class={cn(
		'mx-3 mb-3 flex shrink-0 flex-col gap-0.5 pt-2 shadow-[inset_0_1px_0_var(--color-sidebar-border)]',
		collapsed && 'mx-2',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>

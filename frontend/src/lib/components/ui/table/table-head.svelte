<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLThAttributes } from 'svelte/elements';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLThAttributes> = $props();
</script>

<th
	bind:this={ref}
	data-slot="table-head"
	class={cn(
		'sticky top-0 z-[var(--arcane-z-sticky)] h-10 bg-background/95 px-4 text-left align-middle text-[11px] font-semibold tracking-[0.06em] whitespace-nowrap text-muted-foreground uppercase backdrop-blur-sm',
		'after:pointer-events-none after:absolute after:inset-x-0 after:bottom-0 after:h-px after:bg-border/70',
		'first:pl-6 last:pr-6 [&:has([role=checkbox])]:pr-0',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</th>

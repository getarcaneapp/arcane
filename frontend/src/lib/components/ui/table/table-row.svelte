<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableRowElement>> = $props();
</script>

<tr
	bind:this={ref}
	data-slot="table-row"
	class={cn(
		'group/row border-border/40 bg-background border-b transition-colors',
		'[&_td]:transition-colors',
		'hover:[--row-bg:color-mix(in_oklch,var(--color-primary)_10%,var(--color-background))]',
		'data-[state=selected]:[--row-bg:color-mix(in_oklch,var(--color-primary)_10%,var(--color-background))]',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>

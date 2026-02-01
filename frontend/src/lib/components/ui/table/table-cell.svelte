<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLTdAttributes } from 'svelte/elements';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLTdAttributes> = $props();
</script>

<td
	bind:this={ref}
	data-slot="table-cell"
	class={cn(
		'bg-transparent px-4 py-3 align-middle whitespace-nowrap',
		'first:pl-6 last:pr-6 [&:has([role=checkbox])]:pr-0',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</td>

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
		'group/row cursor-pointer border-b border-border/40 bg-background transition-colors duration-150',
		'hover:bg-primary/[0.06] data-[state=selected]:bg-primary/[0.12] data-[state=selected]:hover:bg-primary/[0.15]',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>

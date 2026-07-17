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
		'group/row cursor-pointer border-b border-border/40 bg-background transition-colors',
		'hover:bg-primary/10 data-[state=selected]:bg-primary/20',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>

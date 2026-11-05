<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableRowElement>> & {
		variant?: 'default' | 'detail' | 'static';
	} = $props();
</script>

<tr
	bind:this={ref}
	data-slot="table-row"
	class={cn(
		'group/row cursor-pointer border-b border-border/40 bg-background transition-colors duration-150',
		'hover:bg-row-hover data-expanded:bg-row-expanded data-[state=selected]:bg-row-selected data-[state=selected]:hover:bg-row-selected-hover',
		variant === 'detail' && 'bg-muted/10 hover:bg-muted/10',
		variant === 'static' && 'bg-transparent hover:bg-transparent',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>

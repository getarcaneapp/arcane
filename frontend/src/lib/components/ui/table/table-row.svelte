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
		// Fills go on the cells (rounded at the row ends) so the glass behind the table shows through.
		'group/row cursor-pointer border-b border-border/40 *:transition-colors *:duration-150',
		'in-[tbody]:hover:*:bg-row-hover data-expanded:*:bg-row-expanded data-[state=selected]:*:bg-row-selected data-[state=selected]:in-[tbody]:hover:*:bg-row-selected-hover',
		variant === 'detail' && '*:bg-row-expanded in-[tbody]:hover:*:bg-row-expanded',
		variant === 'static' && 'in-[tbody]:hover:*:bg-transparent',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>

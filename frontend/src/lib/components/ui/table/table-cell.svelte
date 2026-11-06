<script lang="ts">
	import type { HTMLTdAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		pinned = false,
		variant = 'default',
		indent = false,
		children,
		...restProps
	}: WithElementRef<HTMLTdAttributes> & {
		pinned?: boolean;
		variant?: 'default' | 'checkbox' | 'expander' | 'flush';
		indent?: boolean;
	} = $props();
</script>

<td
	bind:this={ref}
	data-slot="table-cell"
	class={cn(
		'bg-transparent px-4 py-3 align-middle whitespace-nowrap',
		'first:pl-6 last:pr-6 [&:has([role=checkbox])]:pr-0',
		variant === 'checkbox' && 'w-0 pr-4!',
		variant === 'expander' && 'w-8 px-2',
		variant === 'flush' && 'p-0',
		indent && 'pl-10',
		pinned && 'sticky right-0 z-(--arcane-z-sticky) bg-inherit p-0 whitespace-nowrap',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</td>

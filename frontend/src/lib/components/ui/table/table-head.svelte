<script lang="ts">
	import type { HTMLThAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		pinned = false,
		variant = 'default',
		indent = false,
		children,
		...restProps
	}: WithElementRef<HTMLThAttributes> & {
		pinned?: boolean;
		variant?: 'default' | 'checkbox' | 'expander' | 'flush';
		indent?: boolean;
	} = $props();
</script>

<th
	bind:this={ref}
	data-slot="table-head"
	class={cn(
		'sticky top-0 z-[calc(var(--arcane-z-sticky)+1)] h-10 bg-table-head px-4 text-left align-middle text-xs-plus leading-5 font-medium whitespace-nowrap text-muted-foreground',
		'first:pl-6 last:pr-6 [&:has([role=checkbox])]:pr-0',
		variant === 'checkbox' && 'w-0 pr-4!',
		variant === 'expander' && 'w-8 px-2',
		variant === 'flush' && 'p-0',
		indent && 'pl-10',
		pinned && 'sticky right-0 z-(--arcane-z-page-floating) p-0 whitespace-nowrap',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</th>

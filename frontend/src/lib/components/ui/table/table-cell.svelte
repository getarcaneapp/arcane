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
	data-pinned={pinned || undefined}
	class={cn(
		'bg-transparent px-4 py-3 align-middle whitespace-nowrap',
		'first:rounded-l-lg first:pl-6 last:rounded-r-lg last:pr-6 [&:has([role=checkbox])]:pr-0',
		variant === 'checkbox' && 'w-0 pr-4!',
		variant === 'expander' && 'w-8 px-2',
		variant === 'flush' && 'p-0',
		indent && 'pl-10',
		pinned && 'sticky right-0 z-(--arcane-z-sticky) w-0 p-0! whitespace-nowrap',
		className
	)}
	{...restProps}
>
	{#if pinned}
		<div
			class="pointer-events-none absolute top-1/2 right-2 flex -translate-y-1/2 items-center gap-0.5 rounded-xl border border-border/60 bg-popover p-0.5 opacity-0 shadow-lg transition-opacity duration-150 group-hover/row:pointer-events-auto group-hover/row:opacity-100 focus-within:pointer-events-auto focus-within:opacity-100 has-data-[state=open]:pointer-events-auto has-data-[state=open]:opacity-100 pointer-coarse:pointer-events-auto pointer-coarse:opacity-100"
		>
			{@render children?.()}
		</div>
	{:else}
		{@render children?.()}
	{/if}
</td>

<script lang="ts">
	import { cn, type WithElementRef } from '#lib/utils.js';
	import type { HTMLThAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		pinned = false,
		variant = 'default',
		indent = false,
		children,
		...restProps
	}: WithElementRef<HTMLThAttributes> & {
		/** Sticky, opaque header for a pinned right-edge column. */
		pinned?: boolean;
		/** `checkbox` and `expander` size narrow control columns; `flush` drops padding for full-width panels. */
		variant?: 'default' | 'checkbox' | 'expander' | 'flush';
		/** Indents the first cell of a grouped row. */
		indent?: boolean;
	} = $props();
</script>

<th
	bind:this={ref}
	data-slot="table-head"
	class={cn(
		'sticky top-0 z-[calc(var(--arcane-z-sticky)+1)] h-10 bg-background/70 px-4 text-left align-middle text-2xs font-semibold tracking-[0.06em] whitespace-nowrap text-muted-foreground uppercase',
		'after:pointer-events-none after:absolute after:inset-x-0 after:bottom-0 after:h-px after:bg-border/70',
		'first:pl-6 last:pr-6 [&:has([role=checkbox])]:pr-0',
		variant === 'checkbox' && 'w-0 pr-4!',
		variant === 'expander' && 'w-8 px-2',
		variant === 'flush' && 'p-0',
		indent && 'pl-10',
		pinned &&
			'sticky right-0 z-(--arcane-z-page-floating) bg-background p-0 whitespace-nowrap group-hover/row:bg-primary/6 group-data-[expanded]/row:bg-muted/30 group-data-[state=selected]/row:bg-primary/12',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</th>

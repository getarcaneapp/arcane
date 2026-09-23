<script lang="ts">
	import { cn, type WithElementRef } from '#lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableRowElement>> & {
		/** `detail` is an expanded-row panel; `static` rows (skeletons, embedded tables) don't react to hover. */
		variant?: 'default' | 'detail' | 'static';
	} = $props();
</script>

<tr
	bind:this={ref}
	data-slot="table-row"
	class={cn(
		'group/row cursor-pointer border-b border-border/40 bg-background transition-colors duration-150',
		'hover:bg-primary/[0.06] data-expanded:bg-muted/30 data-[state=selected]:bg-primary/[0.12] data-[state=selected]:hover:bg-primary/[0.15]',
		variant === 'detail' && 'bg-muted/10 hover:bg-muted/10',
		variant === 'static' && 'bg-transparent hover:bg-transparent',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>

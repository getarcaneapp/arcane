<script lang="ts">
	import { cn } from '#lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';

	let {
		class: className,
		variant = 'default',
		children,
		...restProps
	}: HTMLAttributes<HTMLDivElement> & {
		/** `card` sits on a card surface; `plain` drops the frame for embedded lists. */
		variant?: 'default' | 'card' | 'plain';
	} = $props();
</script>

<div
	data-slot="empty"
	class={cn(
		'flex min-w-0 flex-1 flex-col items-center justify-center gap-6 rounded-lg border border-dashed border-border/50 bg-muted/20 p-6 text-center text-balance md:p-12',
		variant === 'card' && 'bg-card/30 py-12',
		variant === 'plain' && 'border-transparent bg-transparent py-12',
		className
	)}
	{...restProps}
>
	{@render children?.()}
</div>

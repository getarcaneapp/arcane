<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';

	import { cn } from '#lib/utils.js';

	let {
		label,
		tone = 'default',
		class: className,
		children,
		...restProps
	}: HTMLButtonAttributes & {
		label: string;
		/** `danger` tints the hover state for destructive shortcuts like cancel. */
		tone?: 'default' | 'danger';
		children: Snippet;
	} = $props();
</script>

<button
	type="button"
	title={label}
	aria-label={label}
	{...restProps}
	class={cn(
		'flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-lg text-muted-foreground transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden disabled:pointer-events-none disabled:opacity-40',
		tone === 'default' && 'hover:bg-foreground/5 hover:text-foreground',
		tone === 'danger' && 'hover:bg-destructive/10 hover:text-destructive',
		className
	)}
>
	{@render children()}
</button>

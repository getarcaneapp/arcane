<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		size = 'default',
		interactive = false,
		selected = false,
		onclick,
		children,
		...restProps
	}: WithElementRef<
		HTMLAttributes<HTMLDivElement> & {
			/** `sm` tightens the content padding for dense tiles. */
			size?: 'default' | 'sm';
			/** Hover affordance for cards that act as links or choices. */
			interactive?: boolean | 'subtle';
			/** Highlights the current or chosen card. */
			selected?: boolean;
			onclick?: (e: MouseEvent) => void;
		}
	> = $props();

	function handleClick(e: MouseEvent) {
		if (onclick) {
			// Check if the clicked element is interactive (button, link, or has onclick)
			const target = e.target as HTMLElement;
			const isInteractive = target.closest('button, a, [onclick], [role="button"]');

			if (!isInteractive) {
				onclick(e);
			}
		}
	}
</script>

<div
	bind:this={ref}
	data-slot="card"
	data-size={size}
	class={cn(
		'group relative isolate gap-0 overflow-hidden rounded-xl border border-border/70 bg-card/10 p-0 text-card-foreground shadow-none transition-colors duration-200',
		onclick && 'cursor-pointer',
		onclick &&
			!interactive &&
			'[&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:bg-muted/60 [&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:shadow-sm',
		interactive === true && 'hover-glow-card',
		interactive === 'subtle' && 'hover-glow-card-subtle',
		selected && 'border-primary/40 bg-primary/5',
		className
	)}
	onclick={onclick ? handleClick : undefined}
	{...restProps}
>
	{@render children?.()}
</div>

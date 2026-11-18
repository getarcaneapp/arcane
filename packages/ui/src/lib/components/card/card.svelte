<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		size = 'default',
		interactive = false,
		selected = false,
		onclick,
		children,
		...restProps
	}: WithElementRef<
		HTMLAttributes<HTMLDivElement> & {
			variant?: 'default' | 'glass' | 'embedded';
			size?: 'default' | 'sm';
			interactive?: boolean | 'subtle';
			selected?: boolean;
			onclick?: (e: MouseEvent) => void;
		}
	> = $props();

	function handleClick(e: MouseEvent) {
		if (onclick) {
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
	data-variant={variant}
	class={cn(
		'group relative isolate gap-0 overflow-hidden rounded-xl border border-border/70 bg-card/10 p-0 text-card-foreground shadow-none transition-colors duration-200',
		variant === 'glass' && 'bg-card/60 shadow-xs backdrop-blur-md dark:bg-surface/40',
		variant === 'embedded' && 'max-md:rounded-none max-md:border-0 max-md:bg-transparent',
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

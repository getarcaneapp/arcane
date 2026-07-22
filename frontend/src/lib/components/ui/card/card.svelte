<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';
	import { cn, type WithElementRef } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'default',
		onclick,
		children,
		...restProps
	}: WithElementRef<
		HTMLAttributes<HTMLDivElement> & { variant?: 'default' | 'subtle' | 'outlined'; onclick?: (e: MouseEvent) => void }
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

	function getVariantClasses(variant: 'default' | 'subtle' | 'outlined') {
		switch (variant) {
			case 'default':
				return 'backdrop-blur-md bg-card/60 shadow-xs dark:bg-surface/40';
			case 'subtle':
				return 'bg-muted/40 dark:bg-surface/30';
			case 'outlined':
				return 'bg-card/50 backdrop-blur-md border-border/70';
			default:
				return 'backdrop-blur-md bg-card/60 shadow-xs dark:bg-surface/40';
		}
	}
</script>

<div
	bind:this={ref}
	data-slot="card"
	class={cn(
		'group relative isolate gap-0 overflow-hidden rounded-xl border border-border/70 p-0 text-card-foreground transition-colors duration-200',
		getVariantClasses(variant),
		onclick
			? 'cursor-pointer [&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:bg-muted/60 [&:not(:has(button:hover,a:hover,[role=button]:hover))]:hover:shadow-sm'
			: '',
		className
	)}
	onclick={onclick ? handleClick : undefined}
	{...restProps}
>
	{@render children?.()}
</div>

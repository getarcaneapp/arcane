<script lang="ts">
	import type { WithElementRef, WithoutChildren } from 'bits-ui';
	import type { HTMLTextareaAttributes } from 'svelte/elements';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		value = $bindable(),
		size = 'default',
		mono = false,
		class: className,
		...restProps
	}: WithoutChildren<WithElementRef<HTMLTextareaAttributes>> & {
		size?: 'default' | 'sm';
		/** Monospace text for code-like values. */
		mono?: boolean;
	} = $props();
</script>

<textarea
	bind:this={ref}
	data-slot="textarea"
	class={cn(
		'flex min-h-20 w-full rounded-lg bg-input/80 px-3 py-2 text-base ring-offset-background backdrop-blur-sm transition-all placeholder:text-muted-foreground focus-visible:bg-input/90 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:backdrop-blur-md focus-visible:outline-none disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
		'aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40',
		size === 'sm' && 'text-xs md:text-xs',
		mono && 'font-mono',
		className
	)}
	bind:value
	{...restProps}></textarea>

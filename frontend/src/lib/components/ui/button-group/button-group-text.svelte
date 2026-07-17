<script lang="ts">
	import { cn } from '$lib/utils.js';
	import type { HTMLAttributes } from 'svelte/elements';
	import type { Snippet } from 'svelte';

	let {
		class: className,
		child,
		...restProps
	}: HTMLAttributes<HTMLDivElement> & {
		child?: Snippet<[{ props: Record<string, unknown> }]>;
	} = $props();

	const classes = $derived(
		cn(
			"flex items-center gap-2 rounded-lg border bg-input/80 px-4 text-sm font-medium shadow-xs backdrop-blur-sm [&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4",
			className
		)
	);

	const mergedProps = $derived({
		...restProps,
		class: classes
	});
</script>

{#if child}
	{@render child({ props: mergedProps })}
{:else}
	<div {...mergedProps}>
		{@render mergedProps.children?.()}
	</div>
{/if}

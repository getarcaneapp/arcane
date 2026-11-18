<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> & {} = $props();
</script>

<div bind:this={ref} data-slot="select-label" class={cn('px-2 py-1.5 text-xs text-muted-foreground', className)} {...restProps}>
	{@render children?.()}
</div>

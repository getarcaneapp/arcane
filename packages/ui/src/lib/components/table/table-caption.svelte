<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { cn, type WithElementRef } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLAttributes<HTMLElement>> = $props();
</script>

<caption bind:this={ref} data-slot="table-caption" class={cn('mt-4 text-sm text-muted-foreground', className)} {...restProps}>
	{@render children?.()}
</caption>

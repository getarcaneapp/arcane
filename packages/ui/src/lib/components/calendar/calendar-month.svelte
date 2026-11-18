<script lang="ts">
	import type { HTMLAttributes } from 'svelte/elements';

	import { type WithElementRef, cn } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLAttributes<HTMLElement>> = $props();
</script>

<div {...restProps} bind:this={ref} class={cn('flex flex-col', className)}>
	{@render children?.()}
</div>

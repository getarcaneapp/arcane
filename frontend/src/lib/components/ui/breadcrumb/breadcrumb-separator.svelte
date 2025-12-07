<script lang="ts">
	import ChevronRight from 'phosphor-svelte/lib/CaretRight';
	import type { WithElementRef } from '$lib/utils.js';
	import type { HTMLLiAttributes } from 'svelte/elements';
	import { cn } from '$lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLLiAttributes> = $props();
</script>

<li role="presentation" aria-hidden="true" class={cn('[&>svg]:size-3.5', className)} bind:this={ref} {...restProps}>
	{#if children}
		{@render children?.()}
	{:else}
		<ChevronRight />
	{/if}
</li>

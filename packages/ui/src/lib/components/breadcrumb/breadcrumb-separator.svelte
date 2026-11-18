<script lang="ts">
	import type { HTMLLiAttributes } from 'svelte/elements';
	import ArrowRightIcon from 'virtual:icons/solar/alt-arrow-right-bold';

	import type { WithElementRef } from '#lib/utils.js';
	import { cn } from '#lib/utils.js';

	let { ref = $bindable(null), class: className, children, ...restProps }: WithElementRef<HTMLLiAttributes> = $props();
</script>

<li role="presentation" aria-hidden="true" class={cn('[&>svg]:size-3.5', className)} bind:this={ref} {...restProps}>
	{#if children}
		{@render children?.()}
	{:else}
		<ArrowRightIcon />
	{/if}
</li>

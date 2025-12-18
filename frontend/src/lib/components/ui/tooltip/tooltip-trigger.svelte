<script lang="ts">
	import { Tooltip as TooltipPrimitive } from 'bits-ui';

	let { ref = $bindable(null), children, child, ...restProps }: TooltipPrimitive.TriggerProps = $props();
</script>

<TooltipPrimitive.Trigger bind:ref data-slot="tooltip-trigger" {...restProps}>
	{#if child}
		{#snippet child({ props }: { props: Record<string, unknown> })}
			{@render child({ props })}
		{/snippet}
	{:else}
		{@render children?.()}
	{/if}
</TooltipPrimitive.Trigger>

<script lang="ts">
	import { cn } from '$lib/utils.js';
	import { Popover as PopoverPrimitive } from 'bits-ui';

	let { ref = $bindable(null), class: className, children, child, ...restProps }: PopoverPrimitive.TriggerProps = $props();
</script>

<PopoverPrimitive.Trigger bind:ref data-slot="popover-trigger" class={cn('', className)} {...restProps}>
	{#if child}
		{#snippet child({ props }: { props: Record<string, unknown> })}
			{@render child({ props })}
		{/snippet}
	{:else}
		{@render children?.()}
	{/if}
</PopoverPrimitive.Trigger>

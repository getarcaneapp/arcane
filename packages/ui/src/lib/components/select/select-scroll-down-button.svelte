<script lang="ts">
	import { Select as SelectPrimitive } from 'bits-ui';
	import ArrowDownIcon from 'virtual:icons/solar/alt-arrow-down-bold';

	import { cn, type WithoutChildrenOrChild } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildrenOrChild<SelectPrimitive.ScrollDownButtonProps> = $props();
</script>

<SelectPrimitive.ScrollDownButton
	bind:ref
	data-slot="select-scroll-down-button"
	class={cn('flex cursor-default items-center justify-center py-1', className)}
	{...restProps}
>
	<ArrowDownIcon class="size-4" />
</SelectPrimitive.ScrollDownButton>

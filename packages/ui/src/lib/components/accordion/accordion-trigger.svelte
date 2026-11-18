<script lang="ts">
	import { Accordion as AccordionPrimitive, type WithoutChild } from 'bits-ui';
	import ArrowDownIcon from 'virtual:icons/solar/alt-arrow-down-bold';

	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		level = 3,
		size = 'default',
		children,
		...restProps
	}: WithoutChild<AccordionPrimitive.TriggerProps> & {
		level?: AccordionPrimitive.HeaderProps['level'];
		size?: 'default' | 'sm';
	} = $props();
</script>

<AccordionPrimitive.Header {level} class="flex">
	<AccordionPrimitive.Trigger
		bind:ref
		class={cn(
			'flex flex-1 items-center justify-between py-4 text-sm font-medium transition-all hover:underline [&[data-state=open]>svg]:rotate-180',
			size === 'sm' && 'py-2',
			className
		)}
		{...restProps}
	>
		{@render children?.()}
		<ArrowDownIcon class="size-4 shrink-0 transition-transform duration-200" />
	</AccordionPrimitive.Trigger>
</AccordionPrimitive.Header>

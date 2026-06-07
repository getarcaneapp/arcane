<script lang="ts">
	import type { Component } from 'svelte';
	import type { WithoutChildren } from 'bits-ui';
	import { getEmblaContext } from './context.js';
	import { cn } from '$lib/utils.js';
	import { Button, type Props } from '$lib/components/ui/button/index.js';

	let {
		ref = $bindable(null),
		class: className,
		variant = 'outline',
		size = 'icon',
		direction,
		icon: Icon,
		label,
		...restProps
	}: WithoutChildren<Props> & {
		direction: 'previous' | 'next';
		icon: Component;
		label: string;
	} = $props();

	// svelte-ignore state_referenced_locally -- direction is fixed per wrapper component.
	const emblaCtx = getEmblaContext(direction === 'next' ? '<Carousel.Next/>' : '<Carousel.Previous/>');
	const isNext = $derived(direction === 'next');
	const canScroll = $derived(isNext ? emblaCtx.canScrollNext : emblaCtx.canScrollPrev);
	const positionClass = $derived(
		emblaCtx.orientation === 'horizontal'
			? `${isNext ? '-end-12' : '-start-12'} top-1/2 -translate-y-1/2`
			: `start-1/2 ${isNext ? '-bottom-12' : '-top-12'} -translate-x-1/2 rotate-90`
	);
</script>

<Button
	data-slot={`carousel-${direction}`}
	{variant}
	{size}
	aria-disabled={!canScroll}
	disabled={!canScroll}
	class={cn('absolute size-8 touch-manipulation rounded-full', positionClass, className)}
	onclick={isNext ? emblaCtx.scrollNext : emblaCtx.scrollPrev}
	onkeydown={emblaCtx.handleKeyDown}
	bind:ref
	{...restProps}
>
	<Icon class="size-4" />
	<span class="sr-only">{label}</span>
</Button>

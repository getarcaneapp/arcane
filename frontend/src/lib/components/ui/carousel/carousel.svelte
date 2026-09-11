<script lang="ts">
	import { type CarouselAPI, type CarouselProps, setEmblaContext } from './context.js';
	import { onDestroy } from 'svelte';
	import { cn, type WithElementRef } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		opts = {},
		plugins = [],
		setApi,
		orientation = 'horizontal',
		class: className,
		children,
		...restProps
	}: WithElementRef<CarouselProps> = $props();

	let api = $state.raw<CarouselAPI>();
	let canScrollNext = $state(false);
	let canScrollPrev = $state(false);
	let scrollSnaps = $state.raw<number[]>([]);
	let selectedIndex = $state(0);

	setEmblaContext({
		get api() {
			return api;
		},
		get orientation() {
			return orientation;
		},
		get options() {
			return opts;
		},
		get plugins() {
			return plugins;
		},
		get canScrollNext() {
			return canScrollNext;
		},
		get canScrollPrev() {
			return canScrollPrev;
		},
		get scrollSnaps() {
			return scrollSnaps;
		},
		get selectedIndex() {
			return selectedIndex;
		},
		scrollPrev,
		scrollNext,
		handleKeyDown,
		onInit,
		scrollTo
	});

	function scrollPrev() {
		api?.scrollPrev();
	}

	function scrollNext() {
		api?.scrollNext();
	}

	function scrollTo(index: number, jump?: boolean) {
		api?.scrollTo(index, jump);
	}

	function onSelect() {
		if (!api) return;
		selectedIndex = api.selectedScrollSnap();
		canScrollNext = api.canScrollNext();
		canScrollPrev = api.canScrollPrev();
	}

	function handleKeyDown(e: KeyboardEvent) {
		if (e.key === 'ArrowLeft') {
			e.preventDefault();
			scrollPrev();
		} else if (e.key === 'ArrowRight') {
			e.preventDefault();
			scrollNext();
		}
	}

	function onInit(event: CustomEvent<CarouselAPI>) {
		api = event.detail;
		setApi?.(api);

		scrollSnaps = api.scrollSnapList();
		api.on('select', onSelect);
		api.on('reInit', onReInit);
		onSelect();
	}

	function onReInit() {
		if (!api) return;
		scrollSnaps = api.scrollSnapList();
		onSelect();
	}

	onDestroy(() => {
		api?.off('select', onSelect);
		api?.off('reInit', onReInit);
	});
</script>

<div
	bind:this={ref}
	data-slot="carousel"
	class={cn('relative', className)}
	role="region"
	aria-roledescription="carousel"
	{...restProps}
>
	{@render children?.()}
</div>

<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { WithoutChildren } from 'bits-ui';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import type { Props } from '#lib/components/ui/button/index.js';
	import CarouselControl from './carousel-control.svelte';

	let { ref = $bindable(null), ...restProps }: WithoutChildren<Props> = $props();
</script>

<CarouselControl bind:ref direction="previous" icon={ArrowLeftIcon} label={m.carousel_previous_slide()} {...restProps} />

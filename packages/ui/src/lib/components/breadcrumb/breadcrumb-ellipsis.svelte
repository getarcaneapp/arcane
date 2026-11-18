<script lang="ts">
	import type { WithElementRef, WithoutChildren } from 'bits-ui';
	import type { HTMLAttributes } from 'svelte/elements';
	import EllipsisIcon from 'virtual:icons/solar/menu-dots-bold';

	import { getUiLabels } from '#lib/labels.js';
	import { cn } from '#lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: WithoutChildren<WithElementRef<HTMLAttributes<HTMLSpanElement>>> = $props();

	const labels = getUiLabels();
</script>

<span
	bind:this={ref}
	role="presentation"
	aria-hidden="true"
	class={cn('flex size-9 items-center justify-center', className)}
	{...restProps}
>
	<EllipsisIcon class="size-4" />
	<span class="sr-only">{labels.more()}</span>
</span>

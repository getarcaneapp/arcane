<script lang="ts">
	import { Progress as ProgressPrimitive, type WithoutChildrenOrChild } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		class: className,
		max = 100,
		value,
		indeterminate = false,
		...restProps
	}: WithoutChildrenOrChild<ProgressPrimitive.RootProps> & { indeterminate?: boolean } = $props();
</script>

<ProgressPrimitive.Root
	bind:ref
	class={cn('bg-secondary relative h-4 w-full overflow-hidden rounded-full', className)}
	{value}
	{max}
	{...restProps}
>
	{#if indeterminate}
		<div class="progress-marquee absolute inset-y-0 left-0">
			<div class="progress-marquee-shine absolute inset-0"></div>
		</div>
	{:else}
		<div
			class="bg-primary h-full w-full flex-1 transition-all"
			style={`transform: translateX(-${100 - (100 * (value ?? 0)) / (max ?? 1)}%)`}
		></div>
	{/if}
</ProgressPrimitive.Root>

<style>
	.progress-marquee {
		width: 35%;
		background: var(--primary);
		animation: marquee 1.2s infinite ease-in-out;
		will-change: transform;
	}

	.progress-marquee-shine {
		background: linear-gradient(
			90deg,
			transparent 0%,
			color-mix(in oklch, white 35%, transparent) 45%,
			color-mix(in oklch, white 55%, transparent) 55%,
			transparent 100%
		);
		opacity: 0.35;
	}

	@keyframes marquee {
		0% {
			transform: translateX(-140%);
		}
		100% {
			transform: translateX(400%);
		}
	}
</style>

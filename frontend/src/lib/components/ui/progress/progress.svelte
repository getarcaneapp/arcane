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
	<div
		class="bg-primary h-full w-full flex-1 transition-all"
		style={`transform: translateX(-${100 - (100 * (value ?? 0)) / (max ?? 1)}%)`}
	></div>
	{#if indeterminate}
		<div class="progress-shimmer absolute inset-0"></div>
	{/if}
</ProgressPrimitive.Root>

<style>
	.progress-shimmer {
		background: linear-gradient(
			90deg,
			transparent 0%,
			rgba(0, 0, 0, 0.15) 40%,
			rgba(0, 0, 0, 0.25) 50%,
			rgba(0, 0, 0, 0.15) 60%,
			transparent 100%
		);
		background-size: 200% 100%;
		animation: shimmer 1.5s infinite linear;
	}

	@keyframes shimmer {
		0% {
			background-position: 200% 0;
		}
		100% {
			background-position: -200% 0;
		}
	}
</style>

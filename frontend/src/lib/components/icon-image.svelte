<script lang="ts">
	import type { Component } from 'svelte';
	import { cn } from '$lib/utils';
	import { ImagesIcon } from '$lib/icons';

	let {
		src,
		alt = '',
		fallback = ImagesIcon,
		class: className = '',
		containerClass = ''
	}: {
		src?: string | null;
		alt?: string;
		fallback?: Component<any>;
		class?: string;
		containerClass?: string;
	} = $props();

	let errorSrc = $state<string | null>(null);
	let validSrc = $derived(src && src !== errorSrc ? src : null);
</script>

<div
	class={cn(
		'bg-muted/40 flex items-center justify-center overflow-hidden rounded-lg ring-1 ring-white/5 ring-inset',
		containerClass
	)}
>
	{#if validSrc}
		<img
			src={validSrc}
			{alt}
			loading="lazy"
			decoding="async"
			referrerpolicy="no-referrer"
			class={cn('object-contain', className)}
			onerror={() => (errorSrc = src ?? null)}
		/>
	{:else}
		{@const FallbackIcon = fallback}
		<FallbackIcon class={cn('text-muted-foreground', className)} />
	{/if}
</div>

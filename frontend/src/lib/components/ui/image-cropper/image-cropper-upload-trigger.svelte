<script lang="ts">
	import { cn } from '#lib/utils.js';
	import { useImageCropperTrigger } from './image-cropper-state.svelte.js';
	import type { ImageCropperUploadTriggerProps } from './types';

	let {
		ref = $bindable(null),
		class: className,
		children,
		type = 'button',
		onclick,
		...rest
	}: ImageCropperUploadTriggerProps = $props();

	const triggerState = useImageCropperTrigger();
</script>

<button
	{...rest}
	bind:this={ref}
	{type}
	onclick={(event) => {
		onclick?.(event);
		if (event.defaultPrevented) return;
		document.getElementById(triggerState.rootState.id)?.click();
	}}
	class={cn(
		'group/avatar relative overflow-hidden rounded-xl hover:cursor-pointer focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:outline-none disabled:pointer-events-none disabled:opacity-70',
		className
	)}
>
	{@render children?.()}
</button>

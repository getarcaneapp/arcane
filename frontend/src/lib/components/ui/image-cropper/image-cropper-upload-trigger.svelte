<script lang="ts">
	import { cn } from '$lib/utils';
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
	class={cn('hover:cursor-pointer', className)}
>
	{@render children?.()}
</button>

<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { useImageCropperCrop } from './image-cropper-state.svelte.js';
	import ActionButton, { type ImageCropperActionButtonProps } from './image-cropper-action-button.svelte';

	let {
		ref = $bindable(null),
		action = 'save',
		size = 'sm',
		customLabel = m.account_crop_photo(),
		disabled = false,
		tone = undefined,
		class: className = undefined,
		onclick
	}: ImageCropperActionButtonProps = $props();

	const cropState = useImageCropperCrop();
</script>

<ActionButton
	bind:ref
	{action}
	{size}
	{tone}
	{customLabel}
	{disabled}
	class={className}
	{onclick}
	onAction={() => cropState.onclick()}
/>

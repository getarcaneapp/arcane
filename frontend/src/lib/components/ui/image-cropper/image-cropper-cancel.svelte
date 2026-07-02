<script lang="ts">
	import { m } from '$lib/paraglide/messages';
	import { useImageCropperCancel } from './image-cropper-state.svelte.js';
	import ActionButton, { type ImageCropperActionButtonProps } from './image-cropper-action-button.svelte';

	let {
		ref = $bindable(null),
		action = 'cancel',
		size = 'sm',
		customLabel = m.common_cancel(),
		disabled = false,
		tone = undefined,
		class: className = undefined,
		onclick
	}: ImageCropperActionButtonProps = $props();

	const cancelState = useImageCropperCancel();
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
	onAction={() => cancelState.onclick()}
/>

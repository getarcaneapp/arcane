<script lang="ts">
	import { ArcaneButton, type Action, type ArcaneButtonSize, type ArcaneButtonTone } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import { useImageCropperCrop } from './image-cropper-state.svelte.js';

	type CropButtonProps = {
		ref?: HTMLElement | null;
		action?: Action;
		size?: ArcaneButtonSize;
		tone?: ArcaneButtonTone;
		customLabel?: string;
		disabled?: boolean;
		class?: string;
		onclick?: (event: MouseEvent & { currentTarget: EventTarget & HTMLButtonElement }) => void;
	};

	let {
		ref = $bindable(null),
		action = 'save',
		size = 'sm',
		customLabel = m.account_crop_photo(),
		disabled = false,
		tone = undefined,
		class: className = undefined,
		onclick
	}: CropButtonProps = $props();

	const cropState = useImageCropperCrop();
</script>

<ArcaneButton
	bind:ref
	{action}
	{size}
	{tone}
	{customLabel}
	{disabled}
	class={className}
	onclick={(event: MouseEvent & { currentTarget: EventTarget & HTMLButtonElement }) => {
		onclick?.(event);
		cropState.onclick();
	}}
/>

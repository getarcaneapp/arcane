<script lang="ts">
	import { ArcaneButton, type Action, type ArcaneButtonSize, type ArcaneButtonTone } from '$lib/components/arcane-button';
	import { m } from '$lib/paraglide/messages';
	import { useImageCropperCancel } from './image-cropper-state.svelte.js';

	type CancelButtonProps = {
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
		action = 'cancel',
		size = 'sm',
		customLabel = m.common_cancel(),
		disabled = false,
		tone = undefined,
		class: className = undefined,
		onclick
	}: CancelButtonProps = $props();

	const cancelState = useImageCropperCancel();
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
		cancelState.onclick();
	}}
/>

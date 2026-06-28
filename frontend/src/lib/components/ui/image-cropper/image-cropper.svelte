<script lang="ts">
	import { onDestroy } from 'svelte';
	import { useId } from 'bits-ui';
	import { box } from 'svelte-toolbelt';
	import { useImageCropperRoot } from './image-cropper-state.svelte.js';
	import type { ImageCropperRootProps } from './types';

	let {
		id = useId(),
		src = $bindable(''),
		onCropped = () => {},
		onError = () => {},
		onUnsupportedFile = () => {},
		children,
		...rest
	}: ImageCropperRootProps = $props();

	const rootState = useImageCropperRoot({
		id: box.with(() => id),
		src: box.with(
			() => src,
			(value) => (src = value)
		),
		onCropped: box.with(() => onCropped),
		onError: box.with(() => onError),
		onUnsupportedFile: box.with(() => onUnsupportedFile)
	});

	onDestroy(() => rootState.dispose());
</script>

{@render children?.()}
<input
	{...rest}
	onchange={(event) => {
		const file = event.currentTarget.files?.[0];
		if (!file) return;

		rootState.onUpload(file);
		event.currentTarget.value = '';
	}}
	type="file"
	{id}
	style="display: none;"
/>

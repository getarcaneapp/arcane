<script lang="ts">
	import Cropper from 'svelte-easy-crop';
	import { useImageCropperCropper } from './image-cropper-state.svelte.js';
	import type { ImageCropperCropperProps } from './types';

	let { cropShape = 'round', aspect = 1, showGrid = false, ...rest }: ImageCropperCropperProps = $props();

	const cropperState = useImageCropperCropper();
</script>

<div class="relative h-full w-full">
	<Cropper
		{...rest}
		{cropShape}
		{aspect}
		{showGrid}
		image={cropperState.rootState.tempUrl}
		oncropcomplete={cropperState.onCropComplete}
	/>
</div>

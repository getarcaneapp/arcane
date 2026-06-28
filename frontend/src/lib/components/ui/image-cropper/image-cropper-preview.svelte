<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar';
	import { UploadIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { useImageCropperPreview } from './image-cropper-state.svelte.js';
	import type { ImageCropperPreviewProps } from './types';

	let { child, class: className }: ImageCropperPreviewProps = $props();

	const previewState = useImageCropperPreview();
</script>

{#if child}
	{@render child({ src: previewState.rootState.src })}
{:else}
	<Avatar.Root class={cn('ring-accent ring-offset-background size-20 ring-2 ring-offset-2', className)}>
		<Avatar.Image src={previewState.rootState.src} />
		<Avatar.Fallback>
			<UploadIcon class="size-4" />
			<span class="sr-only">{m.account_upload_photo()}</span>
		</Avatar.Fallback>
	</Avatar.Root>
{/if}

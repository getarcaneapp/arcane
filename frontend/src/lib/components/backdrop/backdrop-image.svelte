<script lang="ts">
	import {
		BACKDROP_DEFAULT_BLUR,
		BACKDROP_DEFAULT_DIM,
		BACKDROP_MAX_BLUR_PX,
		resolveBackdropSource
	} from '#lib/config/backdrops.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { cn } from '#lib/utils.js';

	const src = $derived(resolveBackdropSource(userStore.current));
	const dim = $derived(userStore.current?.preferences?.backdropDim ?? BACKDROP_DEFAULT_DIM);
	const blur = $derived(userStore.current?.preferences?.backdropBlur ?? BACKDROP_DEFAULT_BLUR);
	let loadedSrc = $state<string>();
	let failedSrc = $state<string>();
	const visible = $derived(src !== undefined && failedSrc !== src);

	$effect(() => {
		if (!visible) return;
		document.documentElement.dataset['backdrop'] = '';
		return () => {
			delete document.documentElement.dataset['backdrop'];
		};
	});
</script>

<div aria-hidden="true" class="pointer-events-none fixed inset-0 -z-10 bg-(--shell-background) select-none">
	{#if src && visible}
		<img
			{src}
			alt=""
			decoding="async"
			class={cn(
				'size-full object-cover opacity-0 blur-(--backdrop-blur) transition-opacity duration-500 dark:brightness-(--backdrop-brightness)',
				blur > 0 && 'scale-110',
				loadedSrc === src && 'opacity-100'
			)}
			style:--backdrop-blur="{(BACKDROP_MAX_BLUR_PX * blur) / 100}px"
			style:--backdrop-brightness={1 - dim / 100}
			onload={() => (loadedSrc = src)}
			onerror={() => (failedSrc = src)}
		/>
		<div class="backdrop-grain absolute inset-0"></div>
	{/if}
</div>

<script lang="ts">
	import * as Slider from '$lib/components/ui/slider/index.js';
	import userStore from '$lib/stores/user-store';
	import { userService } from '$lib/services/user-service';
	import { applyFontSize, FONT_SIZE_MIN, FONT_SIZE_MAX, FONT_SIZE_DEFAULT } from '$lib/utils/theme';
	import { debounced } from '$lib/utils/ws';
	import { queryKeys } from '$lib/query/query-keys';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { get } from 'svelte/store';
	import { m } from '$lib/paraglide/messages';

	let { id = 'fontSizePicker', class: className = '' }: { id?: string; class?: string } = $props();

	const queryClient = useQueryClient();

	const initialSize = get(userStore)?.fontSize ?? FONT_SIZE_DEFAULT;
	let currentSize = $state(initialSize);
	let lastPersisted = initialSize;

	const persist = debounced(async (px: number) => {
		const previous = lastPersisted;
		try {
			await userService.updateMyProfile({ fontSize: px });
			lastPersisted = px;
			await queryClient.invalidateQueries({ queryKey: queryKeys.users.all });
		} catch (err) {
			console.error('Failed to update font size', err);
			currentSize = previous;
			applyFontSize(previous);
		}
	}, 400);

	function handleChange(px: number) {
		applyFontSize(px);
		persist(px);
	}
</script>

<div class={`font-size-picker flex items-center gap-3 ${className}`}>
	<Slider.Root
		type="single"
		bind:value={currentSize}
		min={FONT_SIZE_MIN}
		max={FONT_SIZE_MAX}
		step={1}
		showTicks
		onValueChange={handleChange}
		{id}
		aria-label={m.font_size()}
		class="w-40 sm:w-44"
	/>
	<span class="w-10 text-right text-sm text-muted-foreground tabular-nums">{currentSize}px</span>
</div>

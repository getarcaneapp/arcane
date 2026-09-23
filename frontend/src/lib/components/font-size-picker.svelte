<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';

	import * as Slider from '#lib/components/ui/slider/index.js';
	import userStore from '#lib/stores/user-store.svelte.js';
	import { userService } from '#lib/services/user-service.js';
	import { applyFontSize, FONT_SIZE_MIN, FONT_SIZE_MAX, FONT_SIZE_DEFAULT } from '#lib/utils/theme.svelte.js';
	import { debounced } from '#lib/utils/ws.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { useQueryClient } from '@tanstack/svelte-query';

	import { m } from '#lib/paraglide/messages.js';

	let { id = 'fontSizePicker', class: className = '' }: { id?: string; class?: string } = $props();

	const queryClient = useQueryClient();

	const initialSize = userStore.current?.fontSize ?? FONT_SIZE_DEFAULT;
	let currentSize = $state(initialSize);
	let lastPersisted = initialSize;

	const persist = debounced(async (px: number) => {
		const previous = lastPersisted;

		await handleApiResultWithCallbacks({
			result: await tryCatch(userService.updateMyProfile({ fontSize: px })),
			message: m.common_update_failed({ resource: m.font_size() }),
			onSuccess: async () => {
				lastPersisted = px;
				await queryClient.invalidateQueries({ queryKey: queryKeys.users.all });
			},
			onError: () => {
				currentSize = previous;
				applyFontSize(previous);
			}
		});
	}, 400);

	function handleChange(px: number) {
		applyFontSize(px);
		persist(px);
	}
</script>

<div class={`flex items-center gap-3 ${className}`}>
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

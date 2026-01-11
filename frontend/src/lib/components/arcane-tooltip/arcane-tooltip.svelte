<script lang="ts">
	import { IsTouchDevice } from '$lib/hooks/is-touch-device.svelte.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { setArcaneTooltipContext } from './context.svelte.js';
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		delayDuration = 500,
		interactive = false,
		children
	}: {
		open?: boolean;
		delayDuration?: number;
		interactive?: boolean;
		children?: Snippet;
	} = $props();

	const isTouchDevice = new IsTouchDevice();
	let isTouch = $derived(isTouchDevice.current);

	setArcaneTooltipContext({
		get isTouch() {
			return isTouch;
		},
		get interactive() {
			return interactive;
		},
		get open() {
			return open;
		},
		setOpen: (v) => {
			open = v;
		}
	});
</script>

{#if isTouch}
	<Popover.Root bind:open>
		{@render children?.()}
	</Popover.Root>
{:else}
	<Tooltip.Provider {delayDuration}>
		<Tooltip.Root bind:open>
			{@render children?.()}
		</Tooltip.Root>
	</Tooltip.Provider>
{/if}

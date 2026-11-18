<script lang="ts">
	import * as Popover from '@arcane/ui/popover';
	import * as Tooltip from '@arcane/ui/tooltip';
	import type { Snippet } from 'svelte';

	import { isTouchDevice } from '#lib/hooks/is-touch-device.svelte.js';

	import { setArcaneTooltipContext } from './context.svelte.js';

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
	<Tooltip.Root bind:open {delayDuration}>
		{@render children?.()}
	</Tooltip.Root>
{/if}

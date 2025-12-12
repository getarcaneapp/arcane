<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import type { Snippet } from 'svelte';

	interface Props {
		/** Side to show the tooltip/popover on */
		side?: 'top' | 'right' | 'bottom' | 'left';
		/** Alignment of the tooltip/popover */
		align?: 'start' | 'center' | 'end';
		/** CSS class for the content */
		contentClass?: string;
		/** Arrow CSS classes (only used for Tooltip on desktop) */
		arrowClasses?: string;
		/** Control open state externally */
		open?: boolean;
		/** Delay before showing (only applies to Tooltip on desktop) */
		delayDuration?: number;
		/** The trigger element snippet */
		trigger: Snippet;
		/** The content snippet */
		content: Snippet;
	}

	let {
		side = 'top',
		align = 'center',
		contentClass = '',
		arrowClasses = '',
		open = $bindable(false),
		delayDuration = 0,
		trigger,
		content
	}: Props = $props();

	const isMobile = new IsMobile();
</script>

{#if isMobile.current}
	<Popover.Root bind:open>
		<Popover.Trigger class="*:pointer-events-none">
			{@render trigger()}
		</Popover.Trigger>
		<Popover.Content {side} {align} class={contentClass}>
			{@render content()}
		</Popover.Content>
	</Popover.Root>
{:else}
	<Tooltip.Provider {delayDuration}>
		<Tooltip.Root bind:open>
			<Tooltip.Trigger>
				{@render trigger()}
			</Tooltip.Trigger>
			<Tooltip.Content {side} {align} class={contentClass} {arrowClasses}>
				{@render content()}
			</Tooltip.Content>
		</Tooltip.Root>
	</Tooltip.Provider>
{/if}

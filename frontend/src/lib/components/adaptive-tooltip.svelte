<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { IsTouchDevice } from '$lib/hooks/is-touch-device.svelte.js';
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
		/**
		 * Enable long-press mode for interactive elements on touch devices.
		 * When false (default): wrapper is clickable, children have pointer-events-none (for disabled elements)
		 * When true: requires 500ms long press on mobile to show tooltip (allows normal interaction on quick tap)
		 */
		interactive?: boolean;
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
		interactive = false,
		trigger,
		content
	}: Props = $props();

	const isTouchDevice = new IsTouchDevice();

	let wrapperElement: HTMLElement | null = $state(null);

	// Long press state (only used for interactive mode)
	let longPressTimer: ReturnType<typeof setTimeout> | null = null;
	let isLongPressing = $state(false);
	const LONG_PRESS_DURATION = 200;

	function handleTouchStart() {
		if (!isTouchDevice.current) return;

		// If popover is already open, a tap should close it
		if (open) {
			return;
		}

		isLongPressing = false;
		longPressTimer = setTimeout(() => {
			isLongPressing = true;
			open = true;
		}, LONG_PRESS_DURATION);
	}

	function handleTouchEnd(event: TouchEvent) {
		if (!isTouchDevice.current) return;

		if (longPressTimer) {
			clearTimeout(longPressTimer);
			longPressTimer = null;
		}

		// If popover is open and user taps, close it
		if (open && !isLongPressing) {
			open = false;
			event.preventDefault();
			event.stopPropagation();
			return;
		}

		// If it was a long press, prevent the click from going through
		if (isLongPressing) {
			event.preventDefault();
			event.stopPropagation();
			isLongPressing = false;
		}
	}

	function handleTouchCancel() {
		if (longPressTimer) {
			clearTimeout(longPressTimer);
			longPressTimer = null;
		}
		isLongPressing = false;
	}

	function handleTouchMove() {
		// Cancel long press if user moves their finger
		if (longPressTimer) {
			clearTimeout(longPressTimer);
			longPressTimer = null;
		}
	}

	function handleClick(event: MouseEvent) {
		// Only prevent click if the popover is open (was triggered by long press)
		// This allows normal clicks/taps to go through when popover is closed
		if (open) {
			event.preventDefault();
			event.stopPropagation();
		}
	}
</script>

{#if isTouchDevice.current}
	<!-- Touch device: Use popover -->
	{#if interactive}
		<!-- Interactive element: Long press to show info, prevent normal popover click -->
		<Popover.Root bind:open>
			<Popover.Trigger>
				{#snippet child({ props })}
					<div
						{...props}
						bind:this={wrapperElement}
						ontouchstart={handleTouchStart}
						ontouchend={handleTouchEnd}
						ontouchcancel={handleTouchCancel}
						ontouchmove={handleTouchMove}
						onclick={handleClick}
					>
						{@render trigger()}
					</div>
				{/snippet}
			</Popover.Trigger>
			<Popover.Content {side} {align} class={contentClass}>
				{@render content()}
			</Popover.Content>
		</Popover.Root>
	{:else}
		<!-- Default mode: Wrapper clickable, children non-interactive -->
		<Popover.Root bind:open>
			<Popover.Trigger class="cursor-pointer *:pointer-events-none">
				{@render trigger()}
			</Popover.Trigger>
			<Popover.Content {side} {align} class={contentClass}>
				{@render content()}
			</Popover.Content>
		</Popover.Root>
	{/if}
{:else}
	<!-- Desktop: Use tooltip (always hover-based) -->
	{#if interactive}
		<!-- Interactive element: Normal hover behavior -->
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
	{:else}
		<!-- Default mode: Wrapper hoverable, children non-interactive -->
		<Tooltip.Provider {delayDuration}>
			<Tooltip.Root bind:open>
				<Tooltip.Trigger class="*:pointer-events-none">
					{@render trigger()}
				</Tooltip.Trigger>
				<Tooltip.Content {side} {align} class={contentClass} {arrowClasses}>
					{@render content()}
				</Tooltip.Content>
			</Tooltip.Root>
		</Tooltip.Provider>
	{/if}
{/if}

<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { getArcaneTooltipContext } from './context.svelte.js';
	import { Tooltip as TooltipPrimitive } from 'bits-ui';

	type ChildProps = { props: Record<string, unknown> };

	let { children, child, class: className }: TooltipPrimitive.TriggerProps = $props();

	const ctx = getArcaneTooltipContext();

	// Long press logic for interactive touch
	let longPressTimer: ReturnType<typeof setTimeout> | null = null;
	let isLongPressing = $state(false);

	function handleTouchStart() {
		if (!ctx.isTouch || !ctx.interactive) return;
		if (ctx.open) return;

		isLongPressing = false;
		longPressTimer = setTimeout(() => {
			isLongPressing = true;
			ctx.setOpen(true);
		}, 500);
	}

	function handleTouchEnd(event: TouchEvent) {
		if (!ctx.isTouch || !ctx.interactive) return;

		if (longPressTimer) {
			clearTimeout(longPressTimer);
			longPressTimer = null;
		}

		if (ctx.open && !isLongPressing) {
			ctx.setOpen(false);
			event.preventDefault();
			event.stopPropagation();
			return;
		}

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
		if (longPressTimer) {
			clearTimeout(longPressTimer);
			longPressTimer = null;
		}
	}

	function handleClick(event: MouseEvent) {
		if (ctx.open && ctx.interactive && ctx.isTouch) {
			event.preventDefault();
			event.stopPropagation();
		}
	}
</script>

{#if ctx.isTouch}
	{#if ctx.interactive}
		<Popover.Trigger>
			{#snippet child({ props }: ChildProps)}
				<span
					{...props}
					class={className}
					ontouchstart={handleTouchStart}
					ontouchend={handleTouchEnd}
					ontouchcancel={handleTouchCancel}
					ontouchmove={handleTouchMove}
					onclick={handleClick}
					role="button"
					tabindex="0"
				>
					{#if child}
						{@render child({ props: {} })}
					{:else}
						{@render children?.()}
					{/if}
				</span>
			{/snippet}
		</Popover.Trigger>
	{:else}
		<Popover.Trigger class={className}>
			{#if child}
				{#snippet child({ props }: ChildProps)}
					{@render child({ props })}
				{/snippet}
			{:else}
				{@render children?.()}
			{/if}
		</Popover.Trigger>
	{/if}
{:else}
	<Tooltip.Trigger class={className}>
		{#if child}
			{#snippet child({ props }: ChildProps)}
				{@render child({ props })}
			{/snippet}
		{:else}
			{@render children?.()}
		{/if}
	</Tooltip.Trigger>
{/if}

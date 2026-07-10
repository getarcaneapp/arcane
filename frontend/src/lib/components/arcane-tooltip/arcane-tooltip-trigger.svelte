<script lang="ts">
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { getArcaneTooltipContext } from './context.svelte.js';
	import { Tooltip as TooltipPrimitive } from 'bits-ui';
	import { cn } from '$lib/utils.js';

	type ChildProps = { props: Record<string, unknown> };
	type TriggerEventProps = { onclick?: (event: MouseEvent) => void };

	let {
		children,
		child,
		class: className,
		disabledChild = false
	}: TooltipPrimitive.TriggerProps & { disabledChild?: boolean } = $props();

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

	function getClickHandler(props: Record<string, unknown>) {
		return (props as TriggerEventProps).onclick;
	}

	function handleDirectClick(event: MouseEvent) {
		if (ctx.interactive) {
			handleClick(event);
		}
	}

	function handleWrapperClick(event: MouseEvent, props: Record<string, unknown>) {
		const onclick = getClickHandler(props);

		if (ctx.interactive) {
			handleClick(event);
			if (!event.defaultPrevented) {
				onclick?.(event);
			}
		} else {
			// Check if the click originated from an interactive element
			let target = event.target as HTMLElement | null;
			let isInteractive = false;

			while (target && target !== event.currentTarget) {
				const tag = target.tagName.toLowerCase();
				if (
					['button', 'input', 'select', 'textarea', 'a'].includes(tag) ||
					target.getAttribute('role') === 'button' ||
					target.getAttribute('tabindex') === '0'
				) {
					// Check if it's disabled
					if (
						(target as HTMLButtonElement).disabled ||
						target.getAttribute('aria-disabled') === 'true' ||
						target.classList.contains('disabled')
					) {
						// If disabled, we treat it as non-interactive for the purpose of tooltip
						// (i.e., we WANT the tooltip to show)
						isInteractive = false;
					} else {
						isInteractive = true;
					}
					break;
				}
				target = target.parentElement;
			}

			if (!isInteractive) {
				onclick?.(event);
			}
		}
	}

	function getDirectTouchProps(props: Record<string, unknown>) {
		const { onclick: _onclick, class: triggerClass, ...restProps } = props;

		return {
			...restProps,
			class: cn('pointer-events-auto inline-flex max-w-full min-w-0', triggerClass as string | undefined, className),
			ontouchstart: ctx.interactive ? handleTouchStart : undefined,
			ontouchend: ctx.interactive ? handleTouchEnd : undefined,
			ontouchcancel: ctx.interactive ? handleTouchCancel : undefined,
			ontouchmove: ctx.interactive ? handleTouchMove : undefined,
			onclick: handleDirectClick
		};
	}

	function getDirectTooltipProps(props: Record<string, unknown>) {
		return {
			...props,
			class: cn('inline-flex max-w-full min-w-0', props['class'] as string | undefined, className)
		};
	}
</script>

{#if ctx.isTouch}
	<Popover.Trigger>
		{#snippet child({ props }: ChildProps)}
			{#if child && !disabledChild}
				{@render child({ props: getDirectTouchProps(props) })}
			{:else}
				<div
					{...props}
					class={cn('pointer-events-auto inline-flex max-w-full min-w-0 cursor-pointer', className)}
					ontouchstart={ctx.interactive ? handleTouchStart : undefined}
					ontouchend={ctx.interactive ? handleTouchEnd : undefined}
					ontouchcancel={ctx.interactive ? handleTouchCancel : undefined}
					ontouchmove={ctx.interactive ? handleTouchMove : undefined}
					onclick={(event) => handleWrapperClick(event, props)}
					role="button"
					data-disabled-child={disabledChild ? '' : undefined}
					tabindex="0"
				>
					{#if child}
						{@render child({ props: {} })}
					{:else}
						{@render children?.()}
					{/if}
				</div>
			{/if}
		{/snippet}
	</Popover.Trigger>
{:else}
	<Tooltip.Trigger>
		{#snippet child({ props }: ChildProps)}
			{#if child && !disabledChild}
				{@render child({ props: getDirectTooltipProps(props) })}
			{:else}
				<div
					{...props}
					role="button"
					data-disabled-child={disabledChild ? '' : undefined}
					tabindex="0"
					class={cn('inline-flex max-w-full min-w-0 cursor-pointer', className)}
				>
					{#if child}
						{@render child({ props: {} })}
					{:else}
						{@render children?.()}
					{/if}
				</div>
			{/if}
		{/snippet}
	</Tooltip.Trigger>
{/if}

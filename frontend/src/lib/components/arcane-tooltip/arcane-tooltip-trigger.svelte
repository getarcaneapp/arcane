<script lang="ts">
	import { Tooltip as TooltipPrimitive } from 'bits-ui';

	import * as Popover from '#lib/components/ui/popover/index.js';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { cn } from '#lib/utils.js';

	import { getArcaneTooltipContext } from './context.svelte.js';

	type ChildProps = { props: Record<string, unknown> };
	type TriggerEventProps = { onclick?: (event: MouseEvent) => void };

	let {
		children,
		child: childSnippet,
		class: className,
		disabledChild = false,
		'aria-label': ariaLabel
	}: TooltipPrimitive.TriggerProps & { disabledChild?: boolean } = $props();

	const ctx = getArcaneTooltipContext();

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

	function handleDirectClick(event: MouseEvent, onclick?: (event: MouseEvent) => void) {
		if (ctx.interactive) {
			handleClick(event);
		}
		const target = event.currentTarget as HTMLElement;
		if (!event.defaultPrevented && !target.matches('a[href], button, input, select, textarea, [role="button"]')) {
			onclick?.(event);
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
			let target = event.target as HTMLElement | null;
			let isInteractive = false;

			while (target && target !== event.currentTarget) {
				const tag = target.tagName.toLowerCase();
				if (
					['button', 'input', 'select', 'textarea', 'a'].includes(tag) ||
					target.getAttribute('role') === 'button' ||
					target.getAttribute('tabindex') === '0'
				) {
					if (
						(target as HTMLButtonElement).disabled ||
						target.getAttribute('aria-disabled') === 'true' ||
						target.classList.contains('disabled')
					) {
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
		const { onclick, class: triggerClass, ...restProps } = props as Record<string, unknown> & TriggerEventProps;

		return {
			...restProps,
			class: cn('pointer-events-auto inline-flex max-w-full min-w-0', triggerClass as string | undefined, className),
			ontouchstart: ctx.interactive ? handleTouchStart : undefined,
			ontouchend: ctx.interactive ? handleTouchEnd : undefined,
			ontouchcancel: ctx.interactive ? handleTouchCancel : undefined,
			ontouchmove: ctx.interactive ? handleTouchMove : undefined,
			onclick: (event: MouseEvent) => handleDirectClick(event, onclick)
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
			{#if childSnippet && !disabledChild}
				{@render childSnippet({ props: getDirectTouchProps(props) })}
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
					aria-label={ariaLabel}
					data-disabled-child={disabledChild ? '' : undefined}
					tabindex="0"
				>
					{#if childSnippet}
						{@render childSnippet({ props: {} })}
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
			{#if childSnippet && !disabledChild}
				{@render childSnippet({ props: getDirectTooltipProps(props) })}
			{:else}
				<div
					{...props}
					role="button"
					aria-label={ariaLabel}
					data-disabled-child={disabledChild ? '' : undefined}
					tabindex="0"
					class={cn('inline-flex max-w-full min-w-0 cursor-pointer', className)}
				>
					{#if childSnippet}
						{@render childSnippet({ props: {} })}
					{:else}
						{@render children?.()}
					{/if}
				</div>
			{/if}
		{/snippet}
	</Tooltip.Trigger>
{/if}

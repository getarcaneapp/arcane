<script lang="ts" module>
	import type { WithChildren, WithoutChildren } from 'bits-ui';
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';
	import type { ArcaneButtonSize, Action, ArcaneButtonHoverEffect, ActionConfig, ArcaneButtonTone } from './variants';
	import type { IconType } from '$lib/icons';

	export type ArcaneButtonPropsWithoutHTML = WithChildren<{
		ref?: HTMLElement | null;
		action: Action;
		size?: ArcaneButtonSize;
		tone?: ArcaneButtonTone;
		hoverEffect?: ArcaneButtonHoverEffect;
		loading?: boolean;
		showLabel?: boolean;
		customLabel?: string;
		loadingLabel?: string;
		icon?: IconType | null;
		onClickPromise?: (
			e: MouseEvent & {
				currentTarget: EventTarget & HTMLButtonElement;
			}
		) => Promise<void>;
	}>;

	export type ArcaneAnchorElementProps = ArcaneButtonPropsWithoutHTML &
		WithoutChildren<Omit<HTMLAnchorAttributes, 'href' | 'type'>> & {
			href: HTMLAnchorAttributes['href'];
			type?: never;
			disabled?: HTMLButtonAttributes['disabled'];
		};

	export type ArcaneButtonElementProps = ArcaneButtonPropsWithoutHTML &
		WithoutChildren<Omit<HTMLButtonAttributes, 'type' | 'href'>> & {
			type?: HTMLButtonAttributes['type'];
			href?: never;
			disabled?: HTMLButtonAttributes['disabled'];
		};

	export type ArcaneButtonProps = ArcaneAnchorElementProps | ArcaneButtonElementProps;
</script>

<script lang="ts">
	import { cn } from '$lib/utils';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { arcaneButtonVariants, actionConfigs } from './variants';
	import { m } from '$lib/paraglide/messages';

	let {
		ref = $bindable(null),
		action,
		size = 'default',
		tone = undefined,
		hoverEffect = undefined,
		href = undefined,
		type = 'button',
		loading = false,
		disabled = false,
		showLabel = true,
		customLabel = undefined,
		loadingLabel = undefined,
		icon = undefined,
		tabindex = 0,
		onclick,
		onClickPromise,
		class: className,
		children,
		...rest
	}: ArcaneButtonProps = $props();

	let config = $derived(actionConfigs[action] as ActionConfig);
	let displayLabel = $derived(customLabel ?? config.defaultLabel);
	let displayLoadingLabel = $derived(loadingLabel ?? config.loadingLabel ?? m.common_processing());
	let isIconOnlyButton = $derived(size === 'icon' || !showLabel);

	let IconComponent = $derived(icon === null ? null : (icon ?? config.IconComponent));

	let hasChildren = $derived(!!children);
</script>

<svelte:element
	this={href ? 'a' : 'button'}
	{...rest}
	data-slot="arcane-button"
	type={href ? undefined : type}
	href={href && !disabled ? href : undefined}
	disabled={href ? undefined : disabled || loading}
	aria-disabled={href ? disabled : undefined}
	role={href && disabled ? 'link' : undefined}
	tabindex={href && disabled ? -1 : tabindex}
	class={cn('relative', arcaneButtonVariants({ tone: tone ?? config.tone, size, hoverEffect }), className)}
	aria-label={hasChildren ? undefined : isIconOnlyButton ? displayLabel : undefined}
	bind:this={ref}
	onclick={async (e: any) => {
		onclick?.(e);
		if (type === undefined) return;
		if (onClickPromise) {
			loading = true;
			await onClickPromise(e);
			loading = false;
		}
	}}
>
	{#if type !== undefined && loading}
		<div class="bg-background/30 absolute inset-0 flex items-center justify-center rounded-[inherit] backdrop-blur-[1px]">
			<Spinner class="size-4" />
		</div>
		<span class="sr-only">{m.common_loading_label({ label: displayLoadingLabel })}</span>
	{/if}

	<span class={cn('flex items-center gap-2', loading && 'opacity-0')}>
		{#if IconComponent}
			<IconComponent class="size-4" />
		{/if}
		{#if !isIconOnlyButton && displayLabel}
			{displayLabel}
		{/if}
		{@render children?.()}
	</span>
</svelte:element>

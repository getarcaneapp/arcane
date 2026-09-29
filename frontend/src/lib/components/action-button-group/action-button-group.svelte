<script lang="ts">
	import { flushSync } from 'svelte';
	import { goto } from '$app/navigation';
	import * as ButtonGroup from '#lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import {
		ArcaneButton,
		actionConfigs,
		type ActionConfig,
		type ArcaneButtonSize,
		type ArcaneButtonTone
	} from '#lib/components/arcane-button/index.js';
	import { ArrowDownIcon, EllipsisIcon } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ActionButton, ActionGroup } from './types.js';

	interface Props {
		buttons?: ActionButton[];
		size?: ArcaneButtonSize;
		class?: string;
	}

	let { buttons = [], size = 'default', class: className = '' }: Props = $props();

	const GROUP_ORDER: ActionGroup[] = ['lifecycle', 'deploy', 'manage', 'danger'];

	const groupLabels = $derived<Record<ActionGroup, string>>({
		lifecycle: m.lifecycle(),
		deploy: m.deploy(),
		manage: m.manage(),
		danger: m.danger_zone()
	});

	// Compact mode drops inline labels when the natural row is wider than the host column.
	// The host column's width never depends on this content, so measuring it cannot loop.
	let compact = $state(false);

	const primary = $derived(buttons.filter((b) => b.placement === 'primary'));
	const menuOnly = $derived(buttons.filter((b) => (b.placement ?? 'menu') === 'menu'));
	// A lone menu item is promoted inline rather than hiding behind a one-entry menu.
	const promoted = $derived(menuOnly.length === 1 ? menuOnly : []);
	const secondary = $derived([...buttons.filter((b) => b.placement === 'secondary'), ...promoted]);
	// Icon-only secondaries (Refresh) sit after the Actions menu at the far right.
	const secondaryLabelled = $derived(secondary.filter((b) => !b.iconOnly));
	const secondaryIcons = $derived(secondary.filter((b) => b.iconOnly));
	const menuGroups = $derived(
		GROUP_ORDER.map((key) => ({
			key,
			items: promoted.length > 0 ? [] : menuOnly.filter((b) => (b.group ?? 'manage') === key)
		})).filter((g) => g.items.length > 0)
	);
	const showMenu = $derived(secondary.length > 0 || menuGroups.length > 0);
	// With no menu-only items the trigger exists solely to fold secondaries on narrow screens.
	const menuIsMobileOnly = $derived(menuGroups.length === 0);
	const foldBreakpoint = $derived(secondaryLabelled.length > 0 ? 'lg' : 'sm');
	// Labelled secondaries fold into the menu below lg, or whenever the row is compact.
	const secondariesInline = $derived(!compact);
	const iconSize = $derived(size === 'sm' ? 'size-8' : 'size-9');

	let rootEl: HTMLElement | undefined;
	let rowEl: HTMLElement | undefined;

	function overflows() {
		if (!rootEl || !rowEl) return false;
		return rowEl.scrollWidth > rootEl.clientWidth + 0.5;
	}

	function remeasure() {
		compact = false;
		flushSync();
		compact = overflows();
	}

	function observeRoot(node: HTMLElement) {
		rootEl = node;
		const ro = new ResizeObserver(remeasure);
		ro.observe(node);
		return () => {
			ro.disconnect();
			rootEl = undefined;
		};
	}

	// Content growth (loading labels, new items) may only tighten, never re-expand.
	function observeRow(node: HTMLElement) {
		rowEl = node;
		const ro = new ResizeObserver(() => {
			if (!compact && overflows()) compact = true;
		});
		ro.observe(node);
		return () => {
			ro.disconnect();
			rowEl = undefined;
		};
	}

	function hasMenu(button: ActionButton) {
		return (button.menuItems?.length ?? 0) > 0 || !!button.menuContent;
	}

	function toneFor(button: ActionButton): ArcaneButtonTone {
		if (button.destructive) return 'outline-destructive';
		return (actionConfigs[button.action] as ActionConfig).tone;
	}

	function iconFor(button: ActionButton) {
		if (button.icon === null) return null;
		return button.icon ?? (actionConfigs[button.action] as ActionConfig).IconComponent;
	}

	function activate(target: { onclick?: () => void; href?: string }) {
		if (target.href) {
			void goto(target.href);
			return;
		}
		target.onclick?.();
	}
</script>

{#snippet itemBody(button: ActionButton)}
	{@const Icon = iconFor(button)}
	{#if button.loading}
		<Spinner class="size-4" />
	{:else if Icon}
		<Icon class="size-4" />
	{/if}
	<span class="flex-1">{button.loading ? (button.loadingLabel ?? button.label) : button.label}</span>
	{#if button.badge !== undefined}
		<span class="text-3xs text-muted-foreground">({button.badge})</span>
	{/if}
{/snippet}

{#snippet menuEntries(button: ActionButton)}
	{@const parentDisabled = !!(button.disabled || button.loading)}
	{#each button.menuItems ?? [] as item (item.id)}
		{@const ItemIcon = item.icon}
		<DropdownMenu.Item
			variant={item.destructive ? 'destructive' : 'default'}
			disabled={parentDisabled || item.disabled}
			onclick={() => activate(item)}
		>
			{#if ItemIcon}
				<ItemIcon class="size-4" />
			{/if}
			{item.label}
		</DropdownMenu.Item>
	{/each}
	{@render button.menuContent?.(parentDisabled)}
{/snippet}

{#snippet menuItem(button: ActionButton)}
	{#if button.onclick || button.href}
		<DropdownMenu.Item
			variant={button.destructive ? 'destructive' : 'default'}
			disabled={button.disabled || button.loading}
			title={button.disabledReason}
			onclick={() => activate(button)}
		>
			{@render itemBody(button)}
		</DropdownMenu.Item>
	{/if}
	{#if hasMenu(button)}
		{@render menuEntries(button)}
	{/if}
{/snippet}

{#snippet plainButton(button: ActionButton)}
	<ArcaneButton
		action={button.action}
		tone={toneFor(button)}
		{size}
		showLabel={!compact}
		aria-label={button.label}
		customLabel={button.label}
		loadingLabel={button.loadingLabel}
		loading={button.loading}
		disabled={button.disabled}
		title={button.disabledReason}
		onclick={button.onclick}
		href={button.href}
		rel={button.rel}
		icon={button.icon}
	>
		{#if button.badge !== undefined}
			<span class="rounded-full border px-1 py-0.5 text-3xs text-muted-foreground">{button.badge}</span>
		{/if}
	</ArcaneButton>
{/snippet}

{#snippet dropdownContent(button: ActionButton)}
	<DropdownMenu.Content align="end" class={cn('z-(--arcane-z-surface)', button.menuContent ? 'w-72' : 'min-w-45')}>
		{@render menuEntries(button)}
	</DropdownMenu.Content>
{/snippet}

{#snippet inlineButton(button: ActionButton)}
	{#if button.iconOnly}
		<ArcaneTooltip.Root>
			<ArcaneTooltip.Trigger>
				{#snippet child({ props })}
					<ArcaneButton
						{...props}
						action={button.action}
						tone={toneFor(button)}
						size="icon"
						class={iconSize}
						customLabel={button.label}
						loadingLabel={button.loadingLabel}
						loading={button.loading}
						disabled={button.disabled}
						title={button.disabledReason}
						onclick={button.onclick}
						href={button.href}
						rel={button.rel}
						icon={button.icon}
					/>
				{/snippet}
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content>{button.label}</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>
	{:else if hasMenu(button) && (button.onclick || button.href)}
		<ButtonGroup.Root>
			{@render plainButton(button)}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger disabled={button.disabled || button.loading}>
					{#snippet child({ props })}
						<ArcaneButton
							{...props}
							action="base"
							tone={toneFor(button)}
							size="icon"
							class={cn(iconSize, size === 'sm' && 'rounded-lg')}
							icon={ArrowDownIcon}
							aria-label={m.common_open_menu()}
						/>
					{/snippet}
				</DropdownMenu.Trigger>
				{@render dropdownContent(button)}
			</DropdownMenu.Root>
		</ButtonGroup.Root>
	{:else if hasMenu(button)}
		<DropdownMenu.Root>
			<DropdownMenu.Trigger disabled={button.disabled || button.loading}>
				{#snippet child({ props })}
					<ArcaneButton
						{...props}
						action={button.action}
						tone={toneFor(button)}
						{size}
						showLabel={!compact}
						aria-label={button.label}
						customLabel={button.label}
						loading={button.loading}
						icon={button.icon}
					>
						<ArrowDownIcon class="size-3.5 opacity-60" />
					</ArcaneButton>
				{/snippet}
			</DropdownMenu.Trigger>
			{@render dropdownContent(button)}
		</DropdownMenu.Root>
	{:else}
		{@render plainButton(button)}
	{/if}
{/snippet}

{#if buttons.length > 0}
	<div class={cn('flex min-w-0 items-center justify-end', className)} {@attach observeRoot}>
		<div class="flex shrink-0 items-center gap-2" {@attach observeRow}>
			{#each primary as button (button.id)}
				{@render inlineButton(button)}
			{/each}

			{#if secondaryLabelled.length > 0 && secondariesInline}
				<div class="hidden items-center gap-2 lg:flex">
					{#each secondaryLabelled as button (button.id)}
						{@render inlineButton(button)}
					{/each}
				</div>
			{/if}

			{#if showMenu}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<ArcaneButton
								{...props}
								action="base"
								tone="outline-primary"
								{size}
								class={cn(
									'max-sm:size-9 max-sm:p-0',
									compact && 'size-9 p-0',
									menuIsMobileOnly && !compact && `${foldBreakpoint}:hidden`
								)}
								aria-label={m.common_more_actions()}
							>
								{#if compact}
									<EllipsisIcon class="size-4" />
								{:else}
									<span class="hidden sm:inline">{m.common_actions()}</span>
									<ArrowDownIcon class="hidden size-4 sm:block" />
									<EllipsisIcon class="size-4 sm:hidden" />
								{/if}
							</ArcaneButton>
						{/snippet}
					</DropdownMenu.Trigger>

					<DropdownMenu.Content align="end" class="z-(--arcane-z-surface) min-w-52">
						{#if secondaryLabelled.length > 0}
							<DropdownMenu.Group class={secondariesInline ? 'lg:hidden' : undefined}>
								{#each secondaryLabelled as button (button.id)}
									{@render menuItem(button)}
								{/each}
							</DropdownMenu.Group>
						{/if}
						{#if secondaryIcons.length > 0}
							<DropdownMenu.Group class="sm:hidden">
								{#each secondaryIcons as button (button.id)}
									{@render menuItem(button)}
								{/each}
							</DropdownMenu.Group>
						{/if}
						{#if secondary.length > 0 && menuGroups.length > 0}
							<DropdownMenu.Separator class={secondaryLabelled.length > 0 && secondariesInline ? 'lg:hidden' : 'sm:hidden'} />
						{/if}

						{#each menuGroups as group, i (group.key)}
							{#if i > 0}
								<DropdownMenu.Separator />
							{/if}
							<DropdownMenu.Group>
								{#if menuGroups.length > 1}
									<DropdownMenu.GroupHeading>
										{groupLabels[group.key]}
									</DropdownMenu.GroupHeading>
								{/if}
								{#each group.items as button (button.id)}
									{@render menuItem(button)}
								{/each}
							</DropdownMenu.Group>
						{/each}
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			{/if}

			{#if secondaryIcons.length > 0}
				<div class="hidden items-center gap-2 sm:flex">
					{#each secondaryIcons as button (button.id)}
						{@render inlineButton(button)}
					{/each}
				</div>
			{/if}
		</div>
	</div>
{/if}

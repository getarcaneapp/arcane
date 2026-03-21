<script lang="ts">
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { ArcaneButtonSize } from '$lib/components/arcane-button/index.js';
	import { EllipsisIcon } from '$lib/icons';
	import { cn } from '$lib/utils';
	import { ArrowDownIcon } from '$lib/icons';
	import type { ActionButton, ActionButtonMenuItem } from './types.js';

	interface Props {
		buttons?: ActionButton[];
		size?: ArcaneButtonSize;
		class?: string;
	}

	let { buttons = [], size = 'default', class: className = '' }: Props = $props();

	const DROPDOWN_WIDTH = $derived(size === 'sm' ? 44 : 48);
	const GAP = 8;

	let containerWidth = $state(0);
	let buttonWidths = $state<number[]>([]);
	let measurementNode: HTMLElement | null = null;

	const visibleCount = $derived.by(() => {
		const total = buttons.length;
		if (total === 0 || buttonWidths.length === 0 || containerWidth === 0) {
			return total;
		}

		const totalWidth = buttonWidths.reduce((sum, w, i) => sum + w + (i > 0 ? GAP : 0), 0);
		if (totalWidth <= containerWidth) {
			return total;
		}

		let usedWidth = DROPDOWN_WIDTH;
		for (let i = 0; i < total; i++) {
			const needed = (buttonWidths[i] ?? 0) + (i > 0 ? GAP : 0);
			if (usedWidth + needed > containerWidth) {
				return i;
			}
			usedWidth += needed;
		}
		return total;
	});

	const visibleButtons = $derived(buttons.slice(0, visibleCount));
	const overflowButtons = $derived(buttons.slice(visibleCount));

	function handleOverflowAction(button: ActionButton) {
		if (button.disabled || button.loading) return;
		if (button.href) {
			window.location.assign(button.href);
			return;
		}
		button.onclick?.();
	}

	function handleMenuItemAction(item: ActionButtonMenuItem) {
		if (item.disabled) return;
		if (item.href) {
			window.location.assign(item.href);
			return;
		}
		item.onclick?.();
	}

	function measureButtons(node: HTMLElement) {
		measurementNode = node;
		return {
			destroy: () => {
				measurementNode = null;
			}
		};
	}

	$effect(() => {
		const actionButtons = buttons;
		if (!measurementNode || actionButtons.length === 0) return;

		const timeoutId = setTimeout(() => {
			requestAnimationFrame(() => {
				if (!measurementNode) return;
				const widths: number[] = [];
				for (const child of measurementNode.children) {
					widths.push((child as HTMLElement).offsetWidth);
				}
				if (widths.length > 0 && widths.length === actionButtons.length) {
					buttonWidths = widths;
				}
			});
		}, 0);

		return () => clearTimeout(timeoutId);
	});

	function observeWidth(node: HTMLElement) {
		let rafId: number | null = null;
		const ro = new ResizeObserver((entries) => {
			if (rafId) cancelAnimationFrame(rafId);
			rafId = requestAnimationFrame(() => {
				const width = entries[0]?.contentRect.width ?? 0;
				if (width > 0 && width !== containerWidth) {
					containerWidth = width;
				}
			});
		});
		ro.observe(node);
		return {
			destroy: () => {
				if (rafId) cancelAnimationFrame(rafId);
				ro.disconnect();
			}
		};
	}
</script>

{#snippet buttonContent(button: ActionButton)}
	{#if button.badge !== undefined}
		<span class="text-muted-foreground rounded-full border px-1 py-0.5 text-[10px]">
			{button.badge}
		</span>
	{/if}
{/snippet}

{#snippet splitButton(button: ActionButton, inert: boolean)}
	<ButtonGroup.Root>
		<ArcaneButton
			action={button.action}
			customLabel={button.label}
			loadingLabel={button.loadingLabel}
			loading={button.loading}
			disabled={button.disabled}
			onclick={inert ? () => {} : button.onclick}
			href={button.href}
			rel={button.rel}
			{size}
			icon={button.icon}
		>
			{@render buttonContent(button)}
		</ArcaneButton>

		{#if inert}
			<ArcaneButton action="base" tone="outline" size="icon" onclick={() => {}} class={cn(size === 'sm' ? 'size-8' : 'size-9')}>
				<ArrowDownIcon class="size-4" />
			</ArcaneButton>
		{:else}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger
					class={cn(
						'border-input bg-background hover:bg-accent hover:text-accent-foreground inline-flex items-center justify-center rounded-md border transition-colors outline-none',
						size === 'sm' ? 'size-8' : 'size-9'
					)}
					aria-label="Open menu"
				>
					<ArrowDownIcon class="size-4" />
				</DropdownMenu.Trigger>

				<DropdownMenu.Content align="end" class="min-w-[180px]">
					{#each button.menuItems ?? [] as item (item.id)}
						<DropdownMenu.Item onclick={() => handleMenuItemAction(item)} disabled={item.disabled}>
							{item.label}
						</DropdownMenu.Item>
					{/each}
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{/if}
	</ButtonGroup.Root>
{/snippet}

{#if buttons.length > 0}
	<div class={cn('flex min-w-0 flex-1 items-center justify-end gap-2', className)} use:observeWidth>
		<div use:measureButtons class="pointer-events-none invisible fixed -left-[9999px] flex items-center gap-2" aria-hidden="true">
			{#each buttons as button (button.id)}
				{#if button.menuItems && button.menuItems.length > 0}
					{@render splitButton(button, true)}
				{:else}
					<ArcaneButton
						action={button.action}
						customLabel={button.label}
						loadingLabel={button.loadingLabel}
						loading={button.loading}
						disabled={button.disabled}
						onclick={() => {}}
						href={button.href}
						rel={button.rel}
						{size}
						icon={button.icon}
					>
						{@render buttonContent(button)}
					</ArcaneButton>
				{/if}
			{/each}
		</div>

		<div class="hidden items-center gap-2 lg:flex">
			{#each visibleButtons as button (button.id)}
				{#if button.menuItems && button.menuItems.length > 0}
					{@render splitButton(button, false)}
				{:else}
					<ArcaneButton
						action={button.action}
						customLabel={button.label}
						loadingLabel={button.loadingLabel}
						loading={button.loading}
						disabled={button.disabled}
						onclick={button.onclick}
						href={button.href}
						rel={button.rel}
						{size}
						icon={button.icon}
					>
						{@render buttonContent(button)}
					</ArcaneButton>
				{/if}
			{/each}

			{#if overflowButtons.length > 0}
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<ArcaneButton
								{...props}
								action="base"
								tone="outline"
								size="icon"
								class={cn('shrink-0', size === 'sm' ? 'size-8' : 'size-9')}
							>
								<span class="sr-only">More actions</span>
								<EllipsisIcon class="size-4" />
							</ArcaneButton>
						{/snippet}
					</DropdownMenu.Trigger>

					<DropdownMenu.Content align="end" class="min-w-[160px]">
						<DropdownMenu.Group>
							{#each overflowButtons as button (button.id)}
								{#if button.menuItems && button.menuItems.length > 0}
									<DropdownMenu.Sub>
										<DropdownMenu.SubTrigger disabled={button.disabled || button.loading}>
											<div class="flex w-full items-center justify-between gap-2">
												<span>{button.loading ? button.loadingLabel || button.label : button.label}</span>
												{#if button.badge !== undefined}
													<span class="text-muted-foreground text-[10px]">({button.badge})</span>
												{/if}
											</div>
										</DropdownMenu.SubTrigger>
										<DropdownMenu.SubContent class="min-w-[180px]">
											<DropdownMenu.Item
												onclick={() => handleOverflowAction(button)}
												disabled={button.disabled || button.loading}
											>
												{button.label}
											</DropdownMenu.Item>
											<DropdownMenu.Separator />
											{#each button.menuItems as item (item.id)}
												<DropdownMenu.Item onclick={() => handleMenuItemAction(item)} disabled={item.disabled}>
													{item.label}
												</DropdownMenu.Item>
											{/each}
										</DropdownMenu.SubContent>
									</DropdownMenu.Sub>
								{:else}
									<DropdownMenu.Item onclick={() => handleOverflowAction(button)} disabled={button.disabled || button.loading}>
										<div class="flex w-full items-center justify-between gap-2">
											<span>{button.loading ? button.loadingLabel || button.label : button.label}</span>
											{#if button.badge !== undefined}
												<span class="text-muted-foreground text-[10px]">({button.badge})</span>
											{/if}
										</div>
									</DropdownMenu.Item>
								{/if}
							{/each}
						</DropdownMenu.Group>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			{/if}
		</div>

		<div class="flex items-center gap-2 lg:hidden">
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<ArcaneButton
							{...props}
							action="base"
							tone="outline"
							size="icon"
							class={cn('shrink-0', size === 'sm' ? 'size-8' : 'size-9')}
						>
							<span class="sr-only">More actions</span>
							<EllipsisIcon class="size-4" />
						</ArcaneButton>
					{/snippet}
				</DropdownMenu.Trigger>

				<DropdownMenu.Content align="end" class="min-w-[160px]">
					<DropdownMenu.Group>
						{#each buttons as button (button.id)}
							{#if button.menuItems && button.menuItems.length > 0}
								<DropdownMenu.Sub>
									<DropdownMenu.SubTrigger disabled={button.disabled || button.loading}>
										<div class="flex w-full items-center justify-between gap-2">
											<span>{button.loading ? button.loadingLabel || button.label : button.label}</span>
											{#if button.badge !== undefined}
												<span class="text-muted-foreground text-[10px]">({button.badge})</span>
											{/if}
										</div>
									</DropdownMenu.SubTrigger>
									<DropdownMenu.SubContent class="min-w-[180px]">
										<DropdownMenu.Item onclick={() => handleOverflowAction(button)} disabled={button.disabled || button.loading}>
											{button.label}
										</DropdownMenu.Item>
										<DropdownMenu.Separator />
										{#each button.menuItems as item (item.id)}
											<DropdownMenu.Item onclick={() => handleMenuItemAction(item)} disabled={item.disabled}>
												{item.label}
											</DropdownMenu.Item>
										{/each}
									</DropdownMenu.SubContent>
								</DropdownMenu.Sub>
							{:else}
								<DropdownMenu.Item onclick={() => handleOverflowAction(button)} disabled={button.disabled || button.loading}>
									<div class="flex w-full items-center justify-between gap-2">
										<span>{button.loading ? button.loadingLabel || button.label : button.label}</span>
										{#if button.badge !== undefined}
											<span class="text-muted-foreground text-[10px]">({button.badge})</span>
										{/if}
									</div>
								</DropdownMenu.Item>
							{/if}
						{/each}
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>
{/if}

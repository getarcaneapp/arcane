<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import HeaderCard from '$lib/components/header-card.svelte';
	import StatCard from '$lib/components/stat-card.svelte';
	import type { Snippet } from 'svelte';
	import type { Action } from '$lib/components/arcane-button/index.js';
	import { EllipsisIcon, type IconType } from '$lib/icons';

	export interface ActionButton {
		id: string;
		action: Action;
		label: string;
		loadingLabel?: string;
		loading?: boolean;
		disabled?: boolean;
		onclick: () => void;
		showOnMobile?: boolean;
	}

	export interface StatCardConfig {
		title: string;
		value: string | number;
		subtitle?: string;
		icon: IconType;
		iconColor?: string;
		bgColor?: string;
		class?: string;
	}

	interface Props {
		title: string;
		subtitle?: string;
		icon?: IconType;
		actionButtons?: ActionButton[];
		statCards?: StatCardConfig[];
		mainContent: Snippet;
		additionalContent?: Snippet;
		class?: string;
		containerClass?: string;
	}

	let {
		title,
		subtitle,
		icon: Icon,
		actionButtons = [],
		statCards = [],
		mainContent,
		additionalContent,
		class: className = '',
		containerClass = 'space-y-8 pb-5 md:space-y-10 md:pb-5'
	}: Props = $props();

	const DROPDOWN_WIDTH = 44;
	const GAP = 8;

	let containerWidth = $state(0);
	let buttonWidths = $state<number[]>([]);
	let measurementNode: HTMLElement | null = null;

	const visibleCount = $derived.by(() => {
		const total = actionButtons.length;
		if (total === 0 || buttonWidths.length === 0 || containerWidth === 0) {
			return total;
		}

		const totalWidth = buttonWidths.reduce((sum, w, i) => sum + w + (i > 0 ? GAP : 0), 0);
		if (totalWidth <= containerWidth) {
			return total;
		}

		let usedWidth = DROPDOWN_WIDTH;
		for (let i = 0; i < total; i++) {
			const needed = buttonWidths[i] + (i > 0 ? GAP : 0);
			if (usedWidth + needed > containerWidth) {
				return i;
			}
			usedWidth += needed;
		}
		return total;
	});

	const visibleButtons = $derived(actionButtons.slice(0, visibleCount));
	const overflowButtons = $derived(actionButtons.slice(visibleCount));

	function measureButtons(node: HTMLElement) {
		measurementNode = node;
		return {
			destroy: () => {
				measurementNode = null;
			}
		};
	}

	$effect(() => {
		const buttons = actionButtons;
		if (!measurementNode || buttons.length === 0) return;

		const timeoutId = setTimeout(() => {
			requestAnimationFrame(() => {
				if (!measurementNode) return;
				const widths: number[] = [];
				for (const child of measurementNode.children) {
					widths.push((child as HTMLElement).offsetWidth);
				}
				if (widths.length > 0 && widths.length === buttons.length) {
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

<div class="{containerClass} {className}">
	<HeaderCard>
		<div class="flex items-center justify-between gap-4">
			<div class="flex min-w-64 flex-1 items-center gap-3 sm:gap-4">
				{#if Icon}
					<div
						class="bg-primary/10 text-primary ring-primary/20 flex size-8 shrink-0 items-center justify-center rounded-lg ring-1 sm:size-10"
					>
						<Icon class="size-4 sm:size-5" />
					</div>
				{/if}
				<div class="min-w-0">
					<h1 class="text-3xl font-bold tracking-tight">{title}</h1>
					{#if subtitle}
						<p class="text-muted-foreground mt-1 text-sm sm:text-base">{subtitle}</p>
					{/if}
				</div>
			</div>

			{#if statCards && statCards.length > 0}
				<div class="hidden shrink items-center justify-center md:flex">
					<div class="border-border/50 bg-muted/30 relative overflow-hidden rounded-xl border backdrop-blur-sm">
						<div class="flex flex-wrap items-center justify-center gap-x-4 gap-y-2 px-4 py-2">
							{#each statCards as card, i}
								<StatCard
									variant="mini"
									title={card.title}
									value={card.value}
									icon={card.icon}
									iconColor={card.iconColor}
									class={card.class}
								/>
							{/each}
						</div>
					</div>
				</div>
			{/if}

			<div class="flex min-w-0 flex-1 items-center justify-end gap-2" use:observeWidth>
				{#if actionButtons.length > 0}
					<!-- Hidden measurement container -->
					<div
						use:measureButtons
						class="pointer-events-none invisible fixed -left-[9999px] flex items-center gap-2"
						aria-hidden="true"
					>
						{#each actionButtons as button (button.id)}
							<ArcaneButton
								action={button.action}
								customLabel={button.label}
								loadingLabel={button.loadingLabel}
								loading={button.loading}
								disabled={button.disabled}
								onclick={() => {}}
								size="sm"
							/>
						{/each}
					</div>

					<!-- LG: Dynamic overflow -->
					<div class="hidden items-center gap-2 lg:flex">
						{#each visibleButtons as button (button.id)}
							<ArcaneButton
								action={button.action}
								customLabel={button.label}
								loadingLabel={button.loadingLabel}
								loading={button.loading}
								disabled={button.disabled}
								onclick={button.onclick}
								size="sm"
							/>
						{/each}

						{#if overflowButtons.length > 0}
							<DropdownMenu.Root>
								<DropdownMenu.Trigger>
									{#snippet child({ props })}
										<ArcaneButton {...props} action="base" tone="outline" size="icon" class="size-8 shrink-0">
											<span class="sr-only">More actions</span>
											<EllipsisIcon class="size-4" />
										</ArcaneButton>
									{/snippet}
								</DropdownMenu.Trigger>

								<DropdownMenu.Content align="end" class="min-w-[160px]">
									<DropdownMenu.Group>
										{#each overflowButtons as button (button.id)}
											<DropdownMenu.Item onclick={button.onclick} disabled={button.disabled || button.loading}>
												{button.loading ? button.loadingLabel || button.label : button.label}
											</DropdownMenu.Item>
										{/each}
									</DropdownMenu.Group>
								</DropdownMenu.Content>
							</DropdownMenu.Root>
						{/if}
					</div>

					<!-- MD & SM: Collapse all into dropdown -->
					<div class="flex items-center gap-2 lg:hidden">
						<DropdownMenu.Root>
							<DropdownMenu.Trigger>
								{#snippet child({ props })}
									<ArcaneButton {...props} action="base" tone="outline" size="icon" class="size-8 shrink-0">
										<span class="sr-only">More actions</span>
										<EllipsisIcon class="size-4" />
									</ArcaneButton>
								{/snippet}
							</DropdownMenu.Trigger>

							<DropdownMenu.Content align="end" class="min-w-[160px]">
								<DropdownMenu.Group>
									{#each actionButtons as button (button.id)}
										<DropdownMenu.Item onclick={button.onclick} disabled={button.disabled || button.loading}>
											{button.loading ? button.loadingLabel || button.label : button.label}
										</DropdownMenu.Item>
									{/each}
								</DropdownMenu.Group>
							</DropdownMenu.Content>
						</DropdownMenu.Root>
					</div>
				{/if}
			</div>
		</div>
	</HeaderCard>

	{@render mainContent()}

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>

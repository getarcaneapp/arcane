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

	let visibleCount = $state(0);

	$effect(() => {
		visibleCount = actionButtons.length;
	});
	let buttonWidths: number[] = [];
	let containerNode: HTMLElement | null = null;
	let buttonsNode: HTMLElement | null = null;

	const visibleButtons = $derived(actionButtons.slice(0, visibleCount));
	const overflowButtons = $derived(actionButtons.slice(visibleCount));

	function calculateVisibleCount(containerWidth: number) {
		if (buttonWidths.length === 0 || containerWidth === 0) {
			return actionButtons.length;
		}

		const totalButtonsWidth = buttonWidths.reduce((sum, w, i) => sum + w + (i > 0 ? GAP : 0), 0);

		if (totalButtonsWidth <= containerWidth) {
			return actionButtons.length;
		}

		let usedWidth = DROPDOWN_WIDTH + GAP;
		let count = 0;
		for (let i = 0; i < buttonWidths.length; i++) {
			const needed = buttonWidths[i] + (i > 0 ? GAP : 0);
			if (usedWidth + needed > containerWidth) break;
			usedWidth += needed;
			count++;
		}
		return count;
	}

	function measureButtons() {
		if (!buttonsNode) return;
		const widths: number[] = [];
		for (const child of buttonsNode.children) {
			widths.push((child as HTMLElement).offsetWidth);
		}
		if (widths.length === actionButtons.length) {
			buttonWidths = widths;
		}
	}

	function updateVisibility() {
		if (!containerNode) return;
		const width = containerNode.offsetWidth;
		visibleCount = calculateVisibleCount(width);
	}

	function initMeasurement(node: HTMLElement) {
		buttonsNode = node;
		requestAnimationFrame(() => {
			measureButtons();
			updateVisibility();
		});
		return { destroy: () => (buttonsNode = null) };
	}

	function initContainer(node: HTMLElement) {
		containerNode = node;

		const ro = new ResizeObserver(() => {
			updateVisibility();
		});
		ro.observe(node);

		return {
			destroy: () => {
				ro.disconnect();
				containerNode = null;
			}
		};
	}

	$effect(() => {
		actionButtons;
		if (buttonsNode) {
			requestAnimationFrame(() => {
				measureButtons();
				updateVisibility();
			});
		}
	});
</script>

<div class="{containerClass} {className}">
	<HeaderCard>
		<div class="flex items-center gap-4">
			<div class="flex shrink-0 items-center gap-3 sm:gap-4">
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
				<div class="hidden min-w-0 shrink items-center justify-center md:flex">
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

			{#if actionButtons.length > 0}
				<div use:initMeasurement class="pointer-events-none invisible fixed flex items-center gap-2" aria-hidden="true">
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

				<div use:initContainer class="flex min-w-10 flex-1 items-center justify-end gap-2">
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
			{/if}
		</div>
	</HeaderCard>

	{@render mainContent()}

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>

<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
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
		actionButtons = [],
		statCards = [],
		mainContent,
		additionalContent,
		class: className = '',
		containerClass = 'space-y-8 pb-5 md:space-y-10 md:pb-5'
	}: Props = $props();

	const mobileVisibleButtons = $derived(actionButtons.filter((btn) => btn.showOnMobile));
	const mobileDropdownButtons = $derived(actionButtons.filter((btn) => !btn.showOnMobile));

	const DROPDOWN_WIDTH = 44;
	const GAP = 8;

	let containerWidth = $state(0);
	let buttonWidths = $state<number[]>([]);

	const visibleCount = $derived.by(() => {
		if (buttonWidths.length === 0 || containerWidth === 0) {
			return actionButtons.length;
		}

		const total = buttonWidths.length;

		let totalWidth = buttonWidths.reduce((sum, w, i) => sum + w + (i > 0 ? GAP : 0), 0);
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

	function measureButtons(node: HTMLElement, _buttonCount: number) {
		const measure = () => {
			const children = node.children;
			const widths: number[] = [];
			for (let i = 0; i < children.length; i++) {
				widths.push((children[i] as HTMLElement).offsetWidth);
			}
			buttonWidths = widths;
		};

		requestAnimationFrame(measure);

		return {
			update: () => requestAnimationFrame(measure)
		};
	}

	function observeWidth(node: HTMLElement) {
		const ro = new ResizeObserver((entries) => {
			containerWidth = entries[0].contentRect.width;
		});
		ro.observe(node);
		return { destroy: () => ro.disconnect() };
	}

	const visibleButtons = $derived(actionButtons.slice(0, visibleCount));
	const overflowButtons = $derived(actionButtons.slice(visibleCount));
</script>

<div class="{containerClass} {className}">
	<div class="relative flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
		<div class="flex-1">
			<h1 class="text-2xl font-bold tracking-tight sm:text-3xl">{title}</h1>
			{#if subtitle}
				<p class="text-muted-foreground mt-1 text-sm">{subtitle}</p>
			{/if}
		</div>

		{#if statCards && statCards.length > 0}
			<div class="hidden flex-1 items-center justify-center md:flex">
				<div class="border-border/50 relative overflow-hidden rounded-full border">
					<!-- Subtle muted background overlay -->
					<div class="bg-muted/50 absolute inset-0"></div>

					<!-- Glass effect container -->
					<div class="relative flex items-center gap-4 px-4 py-1.5 backdrop-blur-md">
						{#each statCards as card, i}
							{#if i > 0}
								<div class="bg-border/50 h-4 w-px"></div>
							{/if}
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

		<div class="flex flex-1 items-center justify-end gap-2" use:observeWidth>
			{#if actionButtons.length > 0}
				<!-- Hidden measurement container -->
				<div
					use:measureButtons={actionButtons.length}
					class="pointer-events-none invisible absolute flex items-center gap-2"
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
						/>
					{/each}
				</div>

				<!-- Desktop: Visible buttons with dynamic overflow -->
				<div class="hidden items-center gap-2 sm:flex">
					{#each visibleButtons as button (button.id)}
						<ArcaneButton
							action={button.action}
							customLabel={button.label}
							loadingLabel={button.loadingLabel}
							loading={button.loading}
							disabled={button.disabled}
							onclick={button.onclick}
						/>
					{/each}

					{#if overflowButtons.length > 0}
						<DropdownMenu.Root>
							<DropdownMenu.Trigger>
								{#snippet child({ props })}
									<ArcaneButton {...props} action="base" tone="outline" size="icon" class="size-9 shrink-0">
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

				<!-- Mobile: Primary buttons + dropdown for rest -->
				<div class="absolute top-4 right-4 flex items-center gap-2 sm:hidden">
					{#each mobileVisibleButtons as button}
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

					{#if mobileDropdownButtons.length > 0}
						<DropdownMenu.Root>
							<DropdownMenu.Trigger>
								{#snippet child({ props })}
									<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="bg-background/70 size-9 border">
										<span class="sr-only">Open menu</span>
										<EllipsisIcon class="size-4" />
									</ArcaneButton>
								{/snippet}
							</DropdownMenu.Trigger>

							<DropdownMenu.Content
								align="end"
								class="bg-popover/90 z-50 min-w-[160px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
							>
								<DropdownMenu.Group>
									{#each mobileDropdownButtons as button}
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
	</div>

	{@render mainContent()}

	{#if additionalContent}
		{@render additionalContent()}
	{/if}
</div>

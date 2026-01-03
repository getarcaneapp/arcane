<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import type { Action, ArcaneButtonSize } from '$lib/components/arcane-button/index.js';
	import { EllipsisIcon } from '$lib/icons';
	import { cn } from '$lib/utils';

	export interface ActionButton {
		id: string;
		action: Action;
		label: string;
		loadingLabel?: string;
		loading?: boolean;
		disabled?: boolean;
		onclick: () => void;
		showOnMobile?: boolean;
		badge?: string | number;
	}

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
			const needed = buttonWidths[i] + (i > 0 ? GAP : 0);
			if (usedWidth + needed > containerWidth) {
				return i;
			}
			usedWidth += needed;
		}
		return total;
	});

	const visibleButtons = $derived(buttons.slice(0, visibleCount));
	const overflowButtons = $derived(buttons.slice(visibleCount));

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

{#if buttons.length > 0}
	<div class={cn('flex min-w-0 flex-1 items-center justify-end gap-2', className)} use:observeWidth>
		<div use:measureButtons class="pointer-events-none invisible fixed -left-[9999px] flex items-center gap-2" aria-hidden="true">
			{#each buttons as button (button.id)}
				<ArcaneButton
					action={button.action}
					customLabel={button.label}
					loadingLabel={button.loadingLabel}
					loading={button.loading}
					disabled={button.disabled}
					onclick={() => {}}
					{size}
				>
					{#if button.badge !== undefined}
						<span class="text-muted-foreground rounded-full border px-1 py-0.5 text-[10px]">
							{button.badge}
						</span>
					{/if}
				</ArcaneButton>
			{/each}
		</div>

		<div class="hidden items-center gap-2 lg:flex">
			{#each visibleButtons as button (button.id)}
				<ArcaneButton
					action={button.action}
					customLabel={button.label}
					loadingLabel={button.loadingLabel}
					loading={button.loading}
					disabled={button.disabled}
					onclick={button.onclick}
					{size}
				>
					{#if button.badge !== undefined}
						<span class="text-muted-foreground rounded-full border px-1 py-0.5 text-[10px]">
							{button.badge}
						</span>
					{/if}
				</ArcaneButton>
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
								<DropdownMenu.Item onclick={button.onclick} disabled={button.disabled || button.loading}>
									<div class="flex w-full items-center justify-between gap-2">
										<span>{button.loading ? button.loadingLabel || button.label : button.label}</span>
										{#if button.badge !== undefined}
											<span class="text-muted-foreground text-[10px]">({button.badge})</span>
										{/if}
									</div>
								</DropdownMenu.Item>
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
							<DropdownMenu.Item onclick={button.onclick} disabled={button.disabled || button.loading}>
								<div class="flex w-full items-center justify-between gap-2">
									<span>{button.loading ? button.loadingLabel || button.label : button.label}</span>
									{#if button.badge !== undefined}
										<span class="text-muted-foreground text-[10px]">({button.badge})</span>
									{/if}
								</div>
							</DropdownMenu.Item>
						{/each}
					</DropdownMenu.Group>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>
{/if}

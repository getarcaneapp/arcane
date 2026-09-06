<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { cn } from '#lib/utils.js';
	import { ArrowLeftIcon, EllipsisIcon } from '#lib/icons/index.js';
	import type { Snippet } from 'svelte';
	import type { DetailAction } from './types.js';

	interface Props {
		backUrl?: string;
		backLabel?: string;
		title: string;
		subtitle?: string;
		actions?: DetailAction[];
		badges?: Snippet;
		headerExtra?: Snippet;
		children: Snippet;
		class?: string;
	}

	let {
		backUrl,
		backLabel,
		title,
		subtitle,
		actions = [],
		badges,
		headerExtra,
		children,
		class: className = ''
	}: Props = $props();

	let scrollY = $state(0);
	const showFloatingHeader = $derived(scrollY > 120);

	$effect(() => {
		const onScroll = () => (scrollY = window.scrollY);
		window.addEventListener('scroll', onScroll, { passive: true });
		return () => window.removeEventListener('scroll', onScroll);
	});

	const primaryAction = $derived(actions[0]);
	const secondaryActions = $derived(actions.slice(1));
</script>

{#snippet ActionMenu(items: DetailAction[], tone: 'ghost' | 'outline', sizeClass: string, minWidthClass: string)}
	{#if items.length > 0}
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<ArcaneButton {...props} action="base" {tone} size="icon" class={sizeClass}>
						<EllipsisIcon class="size-4" />
					</ArcaneButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="end" class={minWidthClass}>
				{#each items as act (act.id)}
					<DropdownMenu.Item onclick={act.onclick} disabled={act.disabled || act.loading}>
						{act.loading ? act.loadingLabel || act.label : act.label}
					</DropdownMenu.Item>
				{/each}
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	{/if}
{/snippet}

{#if showFloatingHeader}
	<div
		class="fixed top-4 left-1/2 z-[var(--arcane-z-page-floating)] w-[calc(100%-2rem)] max-w-fit -translate-x-1/2 animate-in px-2 duration-200 fade-in slide-in-from-top-2 sm:w-auto sm:px-0"
	>
		<div
			class="bubble-shadow-lg flex items-center gap-3 rounded-lg border border-border/60 bg-popover/95 px-4 py-2 backdrop-blur-md supports-backdrop-filter:bg-popover/85"
		>
			{#if backUrl}
				<ArcaneButton action="base" tone="ghost" href={backUrl} class="size-8 rounded-md p-0">
					<ArrowLeftIcon class="size-4" />
				</ArcaneButton>

				<div class="h-4 w-px bg-border/60"></div>
			{/if}

			<span class="max-w-[200px] truncate text-sm font-semibold">{title}</span>

			{#if primaryAction}
				<div class="h-4 w-px bg-border/60"></div>
				<div class="flex items-center gap-1.5">
					<ArcaneButton
						action={primaryAction.action}
						customLabel={primaryAction.label}
						loading={primaryAction.loading}
						disabled={primaryAction.disabled}
						onclick={primaryAction.onclick}
					/>
					{@render ActionMenu(secondaryActions, 'ghost', 'size-8', 'min-w-[140px]')}
				</div>
			{/if}
		</div>
	</div>
{/if}

<div class={cn('space-y-6 pb-8', className)}>
	<div class="space-y-4">
		<div class="flex items-center justify-between">
			{#if backUrl}
				<ArcaneButton action="base" tone="ghost" href={backUrl} class="-ml-2">
					<ArrowLeftIcon class="size-4" />
					{backLabel}
				</ArcaneButton>
			{:else}
				<span></span>
			{/if}

			<div class="flex items-center gap-2 sm:hidden">
				{#if actions.length === 1 && primaryAction}
					<ArcaneButton
						action={primaryAction.action}
						customLabel={primaryAction.label}
						loading={primaryAction.loading}
						disabled={primaryAction.disabled}
						onclick={primaryAction.onclick}
					/>
				{:else}
					{@render ActionMenu(actions, 'outline', 'size-9', 'min-w-[160px]')}
				{/if}
			</div>
		</div>

		<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="min-w-0 flex-1 space-y-2">
				<h1 class="text-xl font-semibold tracking-tight break-all sm:text-2xl">{title}</h1>
				{#if subtitle}
					<p class="text-sm text-muted-foreground">{subtitle}</p>
				{/if}
				{#if badges}
					<div class="flex flex-wrap items-center gap-2 pt-1">
						{@render badges()}
					</div>
				{/if}
			</div>

			<div class="hidden shrink-0 items-center gap-2 sm:flex">
				{#each actions as act (act.id)}
					<ArcaneButton
						action={act.action}
						customLabel={act.label}
						loadingLabel={act.loadingLabel}
						loading={act.loading}
						disabled={act.disabled}
						onclick={act.onclick}
					/>
				{/each}
			</div>
		</div>

		{@render headerExtra?.()}
	</div>

	{@render children()}
</div>

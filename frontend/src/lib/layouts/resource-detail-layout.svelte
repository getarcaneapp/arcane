<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { cn } from '$lib/utils';
	import { ArrowLeftIcon, EllipsisIcon } from '$lib/icons';
	import type { Snippet } from 'svelte';
	import type { Action } from '$lib/components/arcane-button/index.js';

	export interface DetailAction {
		id: string;
		action: Action;
		label: string;
		loadingLabel?: string;
		loading?: boolean;
		disabled?: boolean;
		onclick: () => void;
	}

	interface Props {
		backUrl: string;
		backLabel: string;
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

{#if showFloatingHeader}
	<div
		class="animate-in fade-in slide-in-from-top-2 fixed top-4 left-1/2 z-40 w-[calc(100%-2rem)] max-w-fit -translate-x-1/2 px-2 duration-200 sm:w-auto sm:px-0"
	>
		<div
			class="bg-popover/95 supports-backdrop-filter:bg-popover/85 border-border/60 flex items-center gap-3 rounded-full border px-4 py-2 shadow-lg backdrop-blur-lg"
		>
			<ArcaneButton action="base" tone="ghost" href={backUrl} class="size-8 rounded-full p-0">
				<ArrowLeftIcon class="size-4" />
			</ArcaneButton>

			<div class="bg-border/60 h-4 w-px"></div>

			<span class="max-w-[200px] truncate text-sm font-semibold">{title}</span>

			{#if actions.length > 0}
				<div class="bg-border/60 h-4 w-px"></div>
				<div class="flex items-center gap-1.5">
					{#if primaryAction}
						<ArcaneButton
							action={primaryAction.action}
							customLabel={primaryAction.label}
							loading={primaryAction.loading}
							disabled={primaryAction.disabled}
							onclick={primaryAction.onclick}
						/>
					{/if}
					{#if secondaryActions.length > 0}
						<DropdownMenu.Root>
							<DropdownMenu.Trigger>
								{#snippet child({ props })}
									<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
										<EllipsisIcon class="size-4" />
									</ArcaneButton>
								{/snippet}
							</DropdownMenu.Trigger>
							<DropdownMenu.Content align="end" class="min-w-[140px]">
								{#each secondaryActions as act (act.id)}
									<DropdownMenu.Item onclick={act.onclick} disabled={act.disabled || act.loading}>
										{act.loading ? act.loadingLabel || act.label : act.label}
									</DropdownMenu.Item>
								{/each}
							</DropdownMenu.Content>
						</DropdownMenu.Root>
					{/if}
				</div>
			{/if}
		</div>
	</div>
{/if}

<div class={cn('space-y-6 pb-8', className)}>
	<div class="space-y-4">
		<div class="flex items-center justify-between">
			<ArcaneButton action="base" tone="ghost" href={backUrl} class="-ml-2">
				<ArrowLeftIcon class="size-4" />
				{backLabel}
			</ArcaneButton>

			<div class="flex items-center gap-2 sm:hidden">
				{#if actions.length === 1 && primaryAction}
					<ArcaneButton
						action={primaryAction.action}
						customLabel={primaryAction.label}
						loading={primaryAction.loading}
						disabled={primaryAction.disabled}
						onclick={primaryAction.onclick}
					/>
				{:else if actions.length > 1}
					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<ArcaneButton {...props} action="base" tone="outline" size="icon" class="size-9">
									<EllipsisIcon class="size-4" />
								</ArcaneButton>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end" class="min-w-[160px]">
							{#each actions as act (act.id)}
								<DropdownMenu.Item onclick={act.onclick} disabled={act.disabled || act.loading}>
									{act.loading ? act.loadingLabel || act.label : act.label}
								</DropdownMenu.Item>
							{/each}
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				{/if}
			</div>
		</div>

		<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="min-w-0 flex-1 space-y-2">
				<h1 class="text-2xl font-bold tracking-tight break-all sm:text-3xl">{title}</h1>
				{#if subtitle}
					<p class="text-muted-foreground text-sm">{subtitle}</p>
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

		{#if headerExtra}
			{@render headerExtra()}
		{/if}
	</div>

	{@render children()}
</div>

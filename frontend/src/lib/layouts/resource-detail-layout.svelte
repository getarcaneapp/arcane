<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { cn } from '#lib/utils.js';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import type { Snippet } from 'svelte';

	interface Props {
		backUrl?: string;
		backLabel?: string;
		title: string;
		subtitle?: string;
		actions?: ActionButton[];
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
</script>

<svelte:window bind:scrollY />

{#if showFloatingHeader}
	<div
		class="fixed top-4 left-1/2 z-(--arcane-z-page-floating) w-full-inset max-w-fit -translate-x-1/2 animate-in px-2 duration-200 fade-in slide-in-from-top-2 sm:w-auto sm:px-0"
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

			<span class="max-w-50 truncate text-sm font-semibold">{title}</span>

			{#if actions.length > 0}
				<div class="h-4 w-px bg-border/60"></div>
				<ActionButtonGroup buttons={actions} size="sm" />
			{/if}
		</div>
	</div>
{/if}

<div class={cn('space-y-6 pb-8', className)}>
	<div class="space-y-4">
		{#if backUrl}
			<div>
				<ArcaneButton action="base" tone="ghost" href={backUrl} class="-ml-2">
					<ArrowLeftIcon class="size-4" />
					{backLabel}
				</ArcaneButton>
			</div>
		{/if}

		<div class="flex items-start justify-between gap-4">
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

			<ActionButtonGroup buttons={actions} class="shrink-0" />
		</div>

		{@render headerExtra?.()}
	</div>

	{@render children()}
</div>

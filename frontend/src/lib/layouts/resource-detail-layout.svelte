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
	<div class="space-y-3">
		<div class="flex items-start gap-3">
			<div class="flex min-w-0 flex-1 items-start gap-2">
				{#if backUrl}
					<ArcaneButton action="base" tone="ghost" size="icon" href={backUrl} class="size-8 shrink-0" title={backLabel}>
						<ArrowLeftIcon class="size-4" />
					</ArcaneButton>
				{/if}
				<div class="min-w-0 flex-1">
					<div class="flex min-h-8 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
						<h1 class="truncate text-lg font-semibold tracking-tight" {title}>{title}</h1>
						{@render badges?.()}
					</div>
					{#if subtitle}
						<p class="mt-0.5 text-sm text-muted-foreground">{subtitle}</p>
					{/if}
				</div>
			</div>

			<ActionButtonGroup buttons={actions} class="shrink-0" />
		</div>

		{@render headerExtra?.()}
	</div>

	{@render children()}
</div>

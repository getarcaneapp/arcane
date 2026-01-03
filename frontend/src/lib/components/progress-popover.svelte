<script lang="ts">
	import { cn } from '$lib/utils.js';
	import * as Popover from '$lib/components/ui/popover/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Popover as PopoverPrimitive } from 'bits-ui';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import * as Item from '$lib/components/ui/item/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import type { Snippet } from 'svelte';
	import { m } from '$lib/paraglide/messages';
	import {
		type LayerProgress,
		type PullPhase,
		getPullPhase,
		getLayerStats,
		showImageLayersState,
		isIndeterminatePhase
	} from '$lib/utils/pull-progress';
	import { DownloadIcon, BoxIcon, ArrowDownIcon, VerifiedCheckIcon, CloseIcon } from '$lib/icons';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';

	interface Props {
		open?: boolean;
		title?: string;
		completeTitle?: string;
		subtitle?: string;
		progress?: number;
		statusText?: string;
		error?: string;
		loading?: boolean;
		mode?: 'pull' | 'generic';
		align?: 'start' | 'center' | 'end';
		sideOffset?: number;
		class?: string;
		icon?: typeof DownloadIcon;
		iconClass?: string;
		preventCloseWhileLoading?: boolean;
		onCancel?: () => void;
		layers?: Record<string, LayerProgress>;
		triggerClass?: string;
		children: Snippet;
	}

	let {
		open = $bindable(false),
		title = m.progress_title(),
		completeTitle = '',
		subtitle = m.progress_subtitle(),
		progress = $bindable(0),
		statusText = '',
		error = '',
		loading = false,
		mode = 'pull',
		align = 'center',
		sideOffset = 4,
		class: className = '',
		icon,
		iconClass = 'size-5',
		preventCloseWhileLoading = true,
		onCancel,
		layers = {},
		triggerClass,
		children
	}: Props = $props();

	const isMobile = new IsMobile();

	const percent = $derived(Math.round(progress));
	const isComplete = $derived(progress >= 100);

	// Track if we've ever reached complete state to prevent flashing back
	let hasReachedComplete = $state(false);

	// Update complete tracking
	$effect(() => {
		if (isComplete && !error) {
			hasReachedComplete = true;
		}
		// Reset when popover closes
		if (!open) {
			hasReachedComplete = false;
		}
	});

	// Derive layer stats using utility
	const layerStats = $derived(getLayerStats(layers, hasReachedComplete));

	// Check if we're in an indeterminate phase (extracting with no byte progress)
	const isIndeterminate = $derived(isIndeterminatePhase(layers, progress));
	const isIndeterminateGeneric = $derived(mode !== 'pull' && loading && !isComplete && !error);

	// Derive the current phase from status text using utility (pull-mode only)
	const currentPhase = $derived.by((): PullPhase => {
		return getPullPhase(statusText, hasReachedComplete || isComplete, !!error);
	});

	// Get localized title based on phase
	const displayTitle = $derived.by(() => {
		if (mode !== 'pull') {
			if (error) return m.error_occurred();
			if (isComplete && !loading) return completeTitle || title;
			return title;
		}

		switch (currentPhase) {
			case 'error':
				return m.error_occurred();
			case 'complete':
				return m.progress_pull_completed();
			case 'downloading':
				return m.progress_downloading();
			case 'extracting':
				return m.progress_extracting();
			case 'verifying':
				return m.progress_verifying();
			case 'waiting':
				return m.progress_waiting();
			default:
				return title;
		}
	});

	// Get the appropriate icon based on phase
	const PhaseIcon = $derived.by(() => {
		if (currentPhase === 'extracting') return BoxIcon;
		return icon ?? DownloadIcon;
	});

	function handleOpenChange(next: boolean) {
		if (preventCloseWhileLoading && !next && loading) {
			open = true;
			return;
		}
		open = next;
	}

	function getLayerPhase(status: string): PullPhase {
		return getPullPhase(status, false, false);
	}
</script>

{#snippet content()}
	<Item.Root variant={error ? 'outline' : 'default'} class={cn(error && 'border-destructive/50')}>
		<Item.Media
			variant="icon"
			class={cn(
				error && 'bg-destructive/10 text-destructive',
				isComplete && !loading && !error && 'bg-green-500/10 text-green-500'
			)}
		>
			{#if error}
				<CloseIcon class={iconClass} />
			{:else if isComplete && !loading}
				<VerifiedCheckIcon class={iconClass} />
			{:else}
				<PhaseIcon class={cn(iconClass, loading && 'animate-pulse')} />
			{/if}
		</Item.Media>
		<Item.Content>
			<Item.Title class={cn(error && 'text-destructive')}>{displayTitle}</Item.Title>
			<Item.Description class={cn(error && 'line-clamp-none break-words whitespace-pre-wrap')}>
				{#if error}
					{error}
				{:else if mode !== 'pull'}
					{statusText || subtitle}
				{:else if layerStats.total > 0}
					{statusText || subtitle}
					<span class="text-muted-foreground">
						· {m.progress_layers_status({ completed: layerStats.completed, total: layerStats.total })}</span
					>
				{:else}
					{hasReachedComplete ? 100 : percent}% · {statusText || subtitle}
				{/if}
			</Item.Description>
		</Item.Content>
		{#if loading && onCancel}
			<Item.Actions>
				<ArcaneButton action="cancel" size="sm" onclick={onCancel} />
			</Item.Actions>
		{/if}
		{#if !error}
			<Item.Footer>
				<Progress
					value={hasReachedComplete || isIndeterminate ? 100 : progress}
					max={100}
					class="h-1.5 w-full"
					indeterminate={(isIndeterminate && !hasReachedComplete) || isIndeterminateGeneric}
				/>
			</Item.Footer>
		{/if}
	</Item.Root>

	{#if Object.keys(layers).length > 0 && !error}
		<Collapsible.Root bind:open={showImageLayersState.current} class="mt-2">
			<Collapsible.Trigger
				class="text-muted-foreground hover:text-foreground hover:bg-accent flex w-full items-center justify-between rounded-md px-2 py-1.5 text-xs transition-colors"
			>
				{m.progress_show_layers()}
				<ArrowDownIcon class={cn('size-3 transition-transform', showImageLayersState.current && 'rotate-180')} />
			</Collapsible.Trigger>
			<Collapsible.Content>
				<div class="mt-2 max-h-48 space-y-1.5 overflow-y-auto">
					{#each Object.entries(layers) as [id, layer] (id)}
						{@const phase = hasReachedComplete ? 'complete' : getLayerPhase(layer.status)}
						{@const layerPercent =
							phase === 'complete' ? 100 : layer.total > 0 ? Math.round((layer.current / layer.total) * 100) : 0}
						<div class="bg-muted/30 rounded-md px-2 py-1.5">
							<div class="flex items-center justify-between gap-2">
								<span class="text-muted-foreground truncate font-mono text-[10px]">{id.slice(0, 12)}</span>
								<span
									class={cn(
										'text-[10px] font-medium',
										phase === 'complete' && 'text-green-500',
										phase === 'downloading' && 'text-blue-500',
										phase === 'extracting' && 'text-amber-500'
									)}
								>
									{#if phase === 'complete'}
										✓
									{:else if layer.total > 0}
										{layerPercent}%
									{:else}
										{layer.status}
									{/if}
								</span>
							</div>
							<Progress value={layerPercent} max={100} class="mt-1 h-1" />
						</div>
					{/each}
				</div>
			</Collapsible.Content>
		</Collapsible.Root>
	{/if}
{/snippet}

{#if isMobile.current}
	<Dialog.Root bind:open onOpenChange={handleOpenChange}>
		<Dialog.Trigger class={triggerClass}>
			{@render children()}
		</Dialog.Trigger>
		<Dialog.Content class={cn('p-2', error && 'max-w-[600px]', className)} showCloseButton={!loading}>
			{@render content()}
		</Dialog.Content>
	</Dialog.Root>
{:else}
	<Popover.Root bind:open onOpenChange={handleOpenChange}>
		<Popover.Trigger class={triggerClass}>
			{@render children()}
		</Popover.Trigger>

		<Popover.Content class={cn('w-80 p-2', error && 'w-auto max-w-[600px]', className)} {align} {sideOffset}>
			{@render content()}
			<PopoverPrimitive.Arrow class="fill-background stroke-border" />
		</Popover.Content>
	</Popover.Root>
{/if}

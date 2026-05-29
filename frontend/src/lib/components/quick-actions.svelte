<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ActionButtonGroup, type ActionButton } from '$lib/components/action-button-group/index.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { hasAnyPermission, hasPermission } from '$lib/utils/auth';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

	type IsLoadingFlags = {
		starting: boolean;
		stopping: boolean;
		pruning: boolean;
	};

	let {
		stoppedContainers,
		runningContainers,
		isLoading,
		onStartAll,
		onStopAll,
		onOpenPruneDialog,
		onRefresh,
		refreshing = false,
		compact = false,
		class: className
	}: {
		stoppedContainers: number;
		runningContainers: number;
		isLoading: IsLoadingFlags;
		onStartAll: () => void;
		onStopAll: () => void;
		onOpenPruneDialog: () => void;
		onRefresh: () => void;
		refreshing?: boolean;
		compact?: boolean;
		class?: string;
	} = $props();

	const isTablet = new IsTablet();
	const isAnyActionLoading = $derived(isLoading.starting || isLoading.stopping || isLoading.pruning);

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canStartAll = $derived(hasPermission('containers:start', currentEnvId));
	const canStopAll = $derived(hasPermission('containers:stop', currentEnvId));
	const canPrune = $derived(hasAnyPermission(['images:prune', 'volumes:prune', 'networks:prune'], currentEnvId));

	const actionButtons: ActionButton[] = $derived(
		[
			canStartAll
				? {
						id: 'start-all',
						action: 'start_all' as const,
						label: m.quick_actions_start_all(),
						onclick: onStartAll,
						loading: isLoading.starting,
						disabled: stoppedContainers === 0 || isAnyActionLoading,
						badge: stoppedContainers
					}
				: null,
			canStopAll
				? {
						id: 'stop-all',
						action: 'stop_all' as const,
						label: m.quick_actions_stop_all(),
						onclick: onStopAll,
						loading: isLoading.stopping,
						disabled: runningContainers === 0 || isAnyActionLoading,
						badge: runningContainers
					}
				: null,
			canPrune
				? {
						id: 'prune',
						action: 'prune' as const,
						label: m.quick_actions_prune_system(),
						onclick: onOpenPruneDialog,
						loading: isLoading.pruning,
						disabled: isAnyActionLoading
					}
				: null,
			{
				id: 'refresh',
				action: 'refresh' as const,
				label: m.common_refresh(),
				onclick: onRefresh,
				loading: refreshing,
				disabled: isAnyActionLoading || refreshing
			}
		].filter((b) => b !== null) as ActionButton[]
	);
</script>

<section class={cn(compact ? 'flex min-w-0 flex-1 items-center justify-end' : '', className)}>
	{#if compact}
		{#if isTablet.current}
			<div class="flex w-full min-w-0 items-center justify-center gap-2">
				{#each actionButtons as button (button.id)}
					<ArcaneButton
						action={button.action}
						customLabel={button.label}
						loadingLabel={button.loadingLabel}
						loading={button.loading}
						disabled={button.disabled}
						onclick={button.onclick}
						size="icon"
						showLabel={false}
						icon={button.icon}
						class="min-w-0 flex-1"
					/>
				{/each}
			</div>
		{:else}
			<ActionButtonGroup buttons={actionButtons} />
		{/if}
	{/if}
</section>

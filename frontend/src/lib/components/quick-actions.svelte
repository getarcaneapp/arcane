<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ActionButtonGroup, type ActionButton } from '$lib/components/action-button-group/index.js';
	import { IsTablet } from '$lib/hooks/is-tablet.svelte.js';
	import { m } from '$lib/paraglide/messages';
	import { StartIcon, StopIcon, TrashIcon } from '$lib/icons';
	import { cn } from '$lib/utils';
	import type { User } from '$lib/types/user.type';

	type IsLoadingFlags = {
		starting: boolean;
		stopping: boolean;
		pruning: boolean;
	};

	let {
		user = null,
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
		user?: User | null;
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

	const currentUserIsAdmin = $derived(!!user?.roles?.includes('admin'));

	const actionButtons: ActionButton[] = $derived(
		[
			{
				id: 'start-all',
				action: 'start_all' as const,
				label: m.quick_actions_start_all(),
				onclick: onStartAll,
				loading: isLoading.starting,
				disabled: stoppedContainers === 0 || isAnyActionLoading,
				badge: stoppedContainers
			},
			{
				id: 'stop-all',
				action: 'stop_all' as const,
				label: m.quick_actions_stop_all(),
				onclick: onStopAll,
				loading: isLoading.stopping,
				disabled: runningContainers === 0 || isAnyActionLoading,
				badge: runningContainers
			},
			{
				id: 'prune',
				action: 'prune' as const,
				label: m.quick_actions_prune_system(),
				onclick: onOpenPruneDialog,
				loading: isLoading.pruning,
				disabled: isAnyActionLoading
			},
			{
				id: 'refresh',
				action: 'refresh' as const,
				label: m.common_refresh(),
				onclick: onRefresh,
				loading: refreshing,
				disabled: isAnyActionLoading || refreshing
			}
		].filter((b) => currentUserIsAdmin || b.id === 'refresh')
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
	{:else if currentUserIsAdmin}
		<h2 class="mb-3 text-lg font-semibold tracking-tight">{m.quick_actions_title()}</h2>

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
			<div class="group hover-lift rounded-2xl bg-gradient-to-br from-emerald-500/20 to-emerald-500/0 p-[1px]">
				<ArcaneButton
					action="start_all"
					size="card"
					tone="outline-success"
					class="bg-card/90 bubble bubble-shadow backdrop-blur-sm"
					onclick={onStartAll}
					loading={isLoading.starting}
					disabled={stoppedContainers === 0 || isAnyActionLoading}
					icon={null}
					showLabel={false}
				>
					<div class="relative">
						<div class="flex size-10 items-center justify-center rounded-lg bg-emerald-500/10 ring-1 ring-emerald-500/30">
							<StartIcon class="size-5 text-emerald-400" />
						</div>
						<div
							class="pointer-events-none absolute -inset-1 rounded-lg bg-emerald-500/20 opacity-0 blur-lg transition-opacity group-hover:opacity-40"
						></div>
					</div>
					<div class="flex-1 text-left">
						<div class="text-sm font-medium">{m.quick_actions_start_all()}</div>
						<div class="text-muted-foreground text-xs">
							<span class="rounded-full border px-1.5 py-0.5">{m.quick_actions_containers({ count: stoppedContainers })}</span>
						</div>
					</div>
				</ArcaneButton>
			</div>

			<div class="group hover-lift rounded-2xl bg-gradient-to-br from-sky-500/20 to-sky-500/0 p-[1px]">
				<ArcaneButton
					action="stop_all"
					size="card"
					tone="outline-info"
					class="bg-card/90 bubble bubble-shadow backdrop-blur-sm"
					onclick={onStopAll}
					loading={isLoading.stopping}
					disabled={runningContainers === 0 || isAnyActionLoading}
					icon={null}
					showLabel={false}
				>
					<div class="relative">
						<div class="flex size-10 items-center justify-center rounded-lg bg-sky-500/10 ring-1 ring-sky-500/30">
							<StopIcon class="size-5 text-sky-400" />
						</div>
						<div
							class="pointer-events-none absolute -inset-1 rounded-lg bg-sky-500/20 opacity-0 blur-lg transition-opacity group-hover:opacity-40"
						></div>
					</div>
					<div class="flex-1 text-left">
						<div class="text-sm font-medium">{m.quick_actions_stop_all()}</div>
						<div class="text-muted-foreground text-xs">
							<span class="rounded-full border px-1.5 py-0.5">{m.quick_actions_containers({ count: runningContainers })}</span>
						</div>
					</div>
				</ArcaneButton>
			</div>

			<div class="group hover-lift rounded-2xl bg-gradient-to-br from-red-500/20 to-red-500/0 p-[1px]">
				<ArcaneButton
					action="prune"
					size="card"
					tone="outline-destructive"
					class="bg-card/90 bubble bubble-shadow backdrop-blur-sm"
					onclick={onOpenPruneDialog}
					loading={isLoading.pruning}
					disabled={isAnyActionLoading}
					icon={null}
					showLabel={false}
				>
					<div class="relative">
						<div class="flex size-10 items-center justify-center rounded-lg bg-red-500/10 ring-1 ring-red-500/30">
							<TrashIcon class="size-5 text-red-400" />
						</div>
						<div
							class="pointer-events-none absolute -inset-1 rounded-lg bg-red-500/20 opacity-0 blur-lg transition-opacity group-hover:opacity-40"
						></div>
					</div>
					<div class="flex-1 text-left">
						<div class="text-sm font-medium">{m.quick_actions_prune_system()}</div>
						<div class="text-muted-foreground text-xs">{m.quick_actions_prune_description()}</div>
					</div>
				</ArcaneButton>
			</div>
		</div>
	{/if}
</section>

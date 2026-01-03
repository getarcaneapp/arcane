<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ActionButtonGroup, type ActionButton } from '$lib/components/action-button-group/index.js';
	import { m } from '$lib/paraglide/messages';
	import type { DockerInfo } from '$lib/types/docker-info.type';
	import { StartIcon, StopIcon, TrashIcon } from '$lib/icons';
	import { cn } from '$lib/utils';

	type IsLoadingFlags = {
		starting: boolean;
		stopping: boolean;
		pruning: boolean;
	};

	let {
		dockerInfo,
		stoppedContainers,
		runningContainers,
		loadingDockerInfo = false,
		isLoading,
		onStartAll,
		onStopAll,
		onOpenPruneDialog,
		onRefresh,
		refreshing = false,
		compact = false,
		class: className
	}: {
		dockerInfo: DockerInfo | null;
		stoppedContainers: number;
		runningContainers: number;
		loadingDockerInfo?: boolean;
		isLoading: IsLoadingFlags;
		onStartAll: () => void;
		onStopAll: () => void;
		onOpenPruneDialog: () => void;
		onRefresh: () => void;
		refreshing?: boolean;
		compact?: boolean;
		class?: string;
	} = $props();

	const isAnyActionLoading = $derived(isLoading.starting || isLoading.stopping || isLoading.pruning);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'start-all',
			action: 'start_all',
			label: m.quick_actions_start_all(),
			onclick: onStartAll,
			loading: isLoading.starting,
			disabled: !dockerInfo || stoppedContainers === 0 || isAnyActionLoading,
			badge: stoppedContainers
		},
		{
			id: 'stop-all',
			action: 'stop_all',
			label: m.quick_actions_stop_all(),
			onclick: onStopAll,
			loading: isLoading.stopping,
			disabled: !dockerInfo || (dockerInfo?.ContainersRunning ?? 0) === 0 || isAnyActionLoading,
			badge: runningContainers
		},
		{
			id: 'prune',
			action: 'prune',
			label: m.quick_actions_prune_system(),
			onclick: onOpenPruneDialog,
			loading: isLoading.pruning,
			disabled: !dockerInfo || isAnyActionLoading
		},
		{
			id: 'refresh',
			action: 'refresh',
			label: m.common_refresh(),
			onclick: onRefresh,
			loading: refreshing,
			disabled: isAnyActionLoading || refreshing
		}
	]);
</script>

<section class={cn(compact ? 'flex min-w-0 flex-1 items-center justify-end' : '', className)}>
	{#if compact}
		{#if loadingDockerInfo}
			<div class="hidden flex-wrap items-center gap-2 sm:flex">
				{#each Array(4) as _}
					<div class="bg-card/60 h-9 w-28 animate-pulse rounded-lg backdrop-blur-sm"></div>
				{/each}
			</div>
			<div class="bg-card/60 size-9 animate-pulse rounded-lg backdrop-blur-sm sm:hidden"></div>
		{:else}
			<ActionButtonGroup buttons={actionButtons} />
		{/if}
	{:else}
		<h2 class="mb-3 text-lg font-semibold tracking-tight">{m.quick_actions_title()}</h2>

		{#if loadingDockerInfo}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{#each Array(3) as _}
					<div class="bg-card/90 flex items-center rounded-lg border p-3 backdrop-blur-sm">
						<div class="bg-muted mr-3 size-6 animate-pulse rounded-full"></div>
						<div class="flex-1">
							<div class="bg-muted mb-1 h-3 w-16 animate-pulse rounded"></div>
							<div class="bg-muted h-2 w-12 animate-pulse rounded"></div>
						</div>
					</div>
				{/each}
			</div>
		{:else}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				<div class="group hover-lift rounded-2xl bg-gradient-to-br from-emerald-500/20 to-emerald-500/0 p-[1px]">
					<ArcaneButton
						action="start_all"
						size="card"
						tone="outline-success"
						class="bg-card/90 bubble bubble-shadow backdrop-blur-sm"
						onclick={onStartAll}
						loading={isLoading.starting}
						disabled={!dockerInfo || stoppedContainers === 0 || isAnyActionLoading}
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
						disabled={!dockerInfo || (dockerInfo?.ContainersRunning ?? 0) === 0 || isAnyActionLoading}
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
						disabled={!dockerInfo || isAnyActionLoading}
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
	{/if}
</section>

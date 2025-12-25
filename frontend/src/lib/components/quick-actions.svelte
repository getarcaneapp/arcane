<script lang="ts">
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import type { DockerInfo } from '$lib/types/docker-info.type';
	import { RefreshIcon, StartIcon, StopIcon, TrashIcon, EllipsisIcon } from '$lib/icons';

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
</script>

<section class={className}>
	{#if compact}
		{#if loadingDockerInfo}
			<div class="hidden flex-wrap items-center gap-2 sm:flex">
				{#each Array(4) as _}
					<div class="bg-card/60 h-9 w-28 animate-pulse rounded-lg backdrop-blur-sm"></div>
				{/each}
			</div>
			<div class="bg-card/60 size-9 animate-pulse rounded-lg backdrop-blur-sm sm:hidden"></div>
		{:else}
			<div class="hidden flex-wrap items-center gap-2 sm:flex">
				<button
					class="ring-offset-background focus-visible:ring-ring bg-card/60 hover:bg-card/90 group bubble-shadow hover-lift inline-flex h-9 items-center gap-2 rounded-lg border border-emerald-500/20 px-3 py-1.5 text-xs backdrop-blur-sm transition-all hover:border-emerald-500/40 hover:bg-emerald-500/10 hover:backdrop-blur-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					disabled={!dockerInfo || stoppedContainers === 0 || isLoading.starting || isLoading.stopping || isLoading.pruning}
					onclick={onStartAll}
					aria-busy={isLoading.starting}
				>
					{#if isLoading.starting}
						<Spinner class="size-3.5 text-emerald-500" />
					{:else}
						<StartIcon class="size-4 text-emerald-500" />
					{/if}
					<span class="font-medium">{m.quick_actions_start_all()}</span>
					<span class="text-muted-foreground rounded-full border px-1 py-0.5 text-[10px]"
						>{m.quick_actions_containers({ count: stoppedContainers })}</span
					>
				</button>

				<button
					class="ring-offset-background focus-visible:ring-ring bg-card/60 hover:bg-card/90 group bubble-shadow hover-lift inline-flex h-9 items-center gap-2 rounded-lg border border-sky-500/20 px-3 py-1.5 text-xs backdrop-blur-sm transition-all hover:border-sky-500/40 hover:bg-sky-500/10 hover:backdrop-blur-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					disabled={!dockerInfo ||
						(dockerInfo?.ContainersRunning ?? 0) === 0 ||
						isLoading.starting ||
						isLoading.stopping ||
						isLoading.pruning}
					onclick={onStopAll}
					aria-busy={isLoading.stopping}
				>
					{#if isLoading.stopping}
						<Spinner class="size-3.5 text-sky-500" />
					{:else}
						<StopIcon class="size-4 text-sky-500" />
					{/if}
					<span class="font-medium">{m.quick_actions_stop_all()}</span>
					<span class="text-muted-foreground rounded-full border px-1 py-0.5 text-[10px]"
						>{m.quick_actions_containers({ count: runningContainers })}</span
					>
				</button>

				<button
					class="ring-offset-background focus-visible:ring-ring bg-card/60 hover:bg-card/90 group bubble-shadow hover-lift inline-flex h-9 items-center gap-2 rounded-lg border border-red-500/20 px-3 py-1.5 text-xs backdrop-blur-sm transition-all hover:border-red-500/40 hover:bg-red-500/10 hover:backdrop-blur-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					disabled={!dockerInfo || isLoading.starting || isLoading.stopping || isLoading.pruning}
					onclick={onOpenPruneDialog}
					aria-busy={isLoading.pruning}
				>
					{#if isLoading.pruning}
						<Spinner class="size-3.5 text-red-500" />
					{:else}
						<TrashIcon class="size-4 text-red-500" />
					{/if}
					<span class="font-medium">{m.quick_actions_prune_system()}</span>
				</button>

				<button
					class="ring-offset-background focus-visible:ring-ring bg-card/60 hover:bg-card/90 border-primary/20 hover:border-primary/40 hover:bg-primary/10 group bubble-shadow hover-lift inline-flex h-9 items-center gap-2 rounded-lg border px-3 py-1.5 text-xs backdrop-blur-sm transition-all hover:backdrop-blur-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
					disabled={isLoading.starting || isLoading.stopping || isLoading.pruning || refreshing}
					onclick={onRefresh}
					aria-busy={refreshing}
				>
					{#if refreshing}
						<Spinner class="text-primary size-3.5" />
					{:else}
						<RefreshIcon class="text-primary size-4" />
					{/if}
					<span class="font-medium">{m.common_refresh()}</span>
				</button>
			</div>

			<div class="sm:hidden">
				<DropdownMenu.Root>
					<DropdownMenu.Trigger>
						{#snippet child({ props })}
							<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="bg-background/70 size-9 border">
								<span class="sr-only">{m.common_open_menu()}</span>
								<EllipsisIcon class="size-4" />
							</ArcaneButton>
						{/snippet}
					</DropdownMenu.Trigger>

					<DropdownMenu.Content
						align="end"
						class="bg-popover/90 z-50 min-w-[160px] rounded-xl border p-1 shadow-lg backdrop-blur-md"
					>
						<DropdownMenu.Group>
							<DropdownMenu.Item
								onclick={onStartAll}
								disabled={!dockerInfo || stoppedContainers === 0 || isLoading.starting || isLoading.stopping || isLoading.pruning}
							>
								{m.quick_actions_start_all()}
							</DropdownMenu.Item>

							<DropdownMenu.Item
								onclick={onStopAll}
								disabled={!dockerInfo ||
									(dockerInfo?.ContainersRunning ?? 0) === 0 ||
									isLoading.starting ||
									isLoading.stopping ||
									isLoading.pruning}
							>
								{m.quick_actions_stop_all()}
							</DropdownMenu.Item>

							<DropdownMenu.Item
								onclick={onOpenPruneDialog}
								disabled={!dockerInfo || isLoading.starting || isLoading.stopping || isLoading.pruning}
							>
								{m.quick_actions_prune_system()}
							</DropdownMenu.Item>

							<DropdownMenu.Item
								onclick={onRefresh}
								disabled={isLoading.starting || isLoading.stopping || isLoading.pruning || refreshing}
							>
								{m.common_refresh()}
							</DropdownMenu.Item>
						</DropdownMenu.Group>
					</DropdownMenu.Content>
				</DropdownMenu.Root>
			</div>
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
					<button
						class="ring-offset-background focus-visible:ring-ring bg-card/90 bubble bubble-shadow flex min-h-14 w-full items-center gap-3 rounded-2xl border p-3 backdrop-blur-sm transition-all hover:shadow-md focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
						disabled={!dockerInfo || stoppedContainers === 0 || isLoading.starting || isLoading.stopping || isLoading.pruning}
						onclick={onStartAll}
						aria-busy={isLoading.starting}
					>
						<div class="relative">
							<div class="flex size-10 items-center justify-center rounded-lg bg-emerald-500/10 ring-1 ring-emerald-500/30">
								{#if isLoading.starting}
									<Spinner class="size-4 text-emerald-400" />
								{:else}
									<StartIcon class="size-5 text-emerald-400" />
								{/if}
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
					</button>
				</div>

				<div class="group hover-lift rounded-2xl bg-gradient-to-br from-sky-500/20 to-sky-500/0 p-[1px]">
					<button
						class="ring-offset-background focus-visible:ring-ring bg-card/90 bubble bubble-shadow flex min-h-14 w-full items-center gap-3 rounded-2xl border p-3 backdrop-blur-sm transition-all hover:shadow-md focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
						disabled={!dockerInfo ||
							(dockerInfo?.ContainersRunning ?? 0) === 0 ||
							isLoading.starting ||
							isLoading.stopping ||
							isLoading.pruning}
						onclick={onStopAll}
						aria-busy={isLoading.stopping}
					>
						<div class="relative">
							<div class="flex size-10 items-center justify-center rounded-lg bg-sky-500/10 ring-1 ring-sky-500/30">
								{#if isLoading.stopping}
									<Spinner class="size-4 text-sky-400" />
								{:else}
									<StopIcon class="size-5 text-sky-400" />
								{/if}
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
					</button>
				</div>

				<div class="group hover-lift rounded-2xl bg-gradient-to-br from-red-500/20 to-red-500/0 p-[1px]">
					<button
						class="ring-offset-background focus-visible:ring-ring bg-card/90 bubble bubble-shadow flex min-h-14 w-full items-center gap-3 rounded-2xl border p-3 backdrop-blur-sm transition-all hover:shadow-md focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
						disabled={!dockerInfo || isLoading.starting || isLoading.stopping || isLoading.pruning}
						onclick={onOpenPruneDialog}
						aria-busy={isLoading.pruning}
					>
						<div class="relative">
							<div class="flex size-10 items-center justify-center rounded-lg bg-red-500/10 ring-1 ring-red-500/30">
								{#if isLoading.pruning}
									<Spinner class="size-4 text-red-400" />
								{:else}
									<TrashIcon class="size-5 text-red-400" />
								{/if}
							</div>
							<div
								class="pointer-events-none absolute -inset-1 rounded-lg bg-red-500/20 opacity-0 blur-lg transition-opacity group-hover:opacity-40"
							></div>
						</div>
						<div class="flex-1 text-left">
							<div class="text-sm font-medium">{m.quick_actions_prune_system()}</div>
							<div class="text-muted-foreground text-xs">{m.quick_actions_prune_description()}</div>
						</div>
					</button>
				</div>
			</div>
		{/if}
	{/if}
</section>

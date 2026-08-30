<script lang="ts">
	import type { ActionButton } from '#lib/components/action-button-group';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import DashboardEnvironmentUpgradeAction from './dashboard-environment-upgrade-action.svelte';
	import DashboardMetricTile from './dash-metric-tile.svelte';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip';
	import { Badge, badgeVariants } from '#lib/components/ui/badge';
	import * as Card from '#lib/components/ui/card';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu';
	import { Skeleton } from '#lib/components/ui/skeleton';
	import { CpuIcon, EnvironmentsIcon, GpuIcon, MemoryStickIcon, VolumesIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import type { AppVersionInformation } from '#lib/types/settings';
	import type { DashboardEnvironmentOverview, SystemStats } from '#lib/types/shared';
	import { cn } from '#lib/utils';
	import { isEnvironmentOnline } from '#lib/utils/docker';
	import {
		formatPercent,
		getActivityMeta,
		getCapacityLabel,
		getCpuMetric,
		getCpuMetricLabel,
		getDiskMetric,
		getGpuMetric,
		getGpuMetricLabel,
		getMemoryMetric,
		getRoleBadge,
		shouldLoadEnvironment
	} from './dashboard-overview';

	// These bindings target record entries that start undefined; Svelte rejects a bindable fallback here.
	let {
		overview,
		isCurrent,
		systemStats,
		liveStatsLoading,
		snapshotLoading,
		useButton,
		menuButtons,
		debugUpgrade = false,
		debugVersionInfo,
		canUpgrade,
		upgradeOpen = $bindable(undefined),
		upgradeLoading = $bindable(undefined),
		onRefreshRequested
	}: {
		overview: DashboardEnvironmentOverview;
		isCurrent: boolean;
		systemStats: SystemStats | null;
		liveStatsLoading: boolean;
		snapshotLoading: boolean;
		useButton?: ActionButton;
		menuButtons: ActionButton[];
		debugUpgrade?: boolean;
		debugVersionInfo: AppVersionInformation;
		canUpgrade: boolean;
		upgradeOpen?: boolean;
		upgradeLoading?: boolean;
		onRefreshRequested: () => void | Promise<void>;
	} = $props();

	const environment = $derived(overview.environment);
	const roleBadge = $derived(getRoleBadge(environment));
	const activity = $derived(getActivityMeta(environment));
	const versionInfo = $derived(overview.versionInfo ?? (debugUpgrade ? debugVersionInfo : null));
	const newestVersionLabel = $derived(versionInfo?.newestVersion ?? versionInfo?.newestDigest?.slice(0, 12));
	const cpuMetric = $derived(getCpuMetric(systemStats));
	const memoryMetric = $derived(getMemoryMetric(systemStats));
	const diskMetric = $derived(getDiskMetric(systemStats));
	const gpuMetric = $derived(getGpuMetric(systemStats));
</script>

<Card.Root
	variant="outlined"
	class={`dashboard-environment-card [container-type:inline-size] overflow-hidden border transition-[background-color,border-color,box-shadow] hover:shadow-[0_0_24px_-8px_color-mix(in_oklch,var(--primary)_40%,transparent)] ${isCurrent ? 'border-primary/40 bg-primary/5' : 'border-border/60 hover:border-primary/25'}`}
>
	<Card.Content class="space-y-4 p-4 sm:p-5">
		<div class="flex flex-col gap-3 border-b border-border/60 pb-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="min-w-0 space-y-2">
				<div class="flex min-w-0 flex-wrap items-center gap-2">
					<div class="max-w-full min-w-0 text-base font-semibold tracking-tight break-words">{environment.name}</div>
					<Badge variant={roleBadge.variant} size="sm">{roleBadge.text}</Badge>
					{#if versionInfo}
						<div class="flex items-center">
							{#if versionInfo.updateAvailable || debugUpgrade}
								<ArcaneTooltip.Root>
									<ArcaneTooltip.Trigger
										class={cn(badgeVariants({ variant: 'gray', size: 'sm' }), 'font-mono hover:text-foreground')}
									>
										{versionInfo.displayVersion || versionInfo.currentTag || versionInfo.currentVersion || m.common_unknown()}
										<span class="relative ml-1.5 flex h-2 w-2">
											<span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-75"></span>
											<span class="relative inline-flex h-2 w-2 rounded-full bg-amber-500"></span>
										</span>
									</ArcaneTooltip.Trigger>
									<ArcaneTooltip.Content class="flex flex-col items-start gap-2">
										<span
											>{m.sidebar_update_available()}{#if newestVersionLabel}: {newestVersionLabel}{/if}</span
										>
										<DashboardEnvironmentUpgradeAction
											{environment}
											{versionInfo}
											{canUpgrade}
											debug={debugUpgrade}
											{onRefreshRequested}
											render="trigger"
											bind:open={upgradeOpen}
											bind:upgrading={upgradeLoading}
										/>
									</ArcaneTooltip.Content>
								</ArcaneTooltip.Root>
							{:else}
								<Badge variant="gray" size="sm" class="font-mono">
									{versionInfo.displayVersion || versionInfo.currentTag || versionInfo.currentVersion || m.common_unknown()}
								</Badge>
							{/if}
						</div>
						{#if versionInfo.updateAvailable || debugUpgrade}
							<DashboardEnvironmentUpgradeAction
								{environment}
								{versionInfo}
								{canUpgrade}
								debug={debugUpgrade}
								{onRefreshRequested}
								render="dialog"
								bind:open={upgradeOpen}
								bind:upgrading={upgradeLoading}
							/>
						{/if}
					{/if}
				</div>

				<div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-muted-foreground/70">
					<span class="font-mono">{environment.apiUrl}</span>
					<span>•</span>
					<span title={activity.title}>{activity.label}: {activity.value}</span>
				</div>
			</div>

			<div class="flex shrink-0 items-center gap-1 pt-1 sm:pt-0">
				{#if useButton}
					<ArcaneButton
						action="base"
						size="sm"
						tone="ghost"
						icon={EnvironmentsIcon}
						customLabel={isCurrent ? m.common_current() : useButton.label}
						loading={useButton.loading}
						disabled={useButton.disabled}
						onclick={useButton.onclick}
						class={cn(isCurrent && 'disabled:opacity-100 [&_svg]:text-primary!')}
					/>
				{/if}
				<RowActionsMenu>
					{#each menuButtons as button (button.id)}
						<DropdownMenu.Item
							disabled={!!(button.disabled || button.loading)}
							onclick={button.onclick}
							class={cn(button.action === 'prune' && 'text-destructive data-highlighted:text-destructive')}
						>
							{#if button.icon}
								<button.icon class="size-4" />
							{/if}
							{button.label}
						</DropdownMenu.Item>
					{/each}
				</RowActionsMenu>
			</div>
		</div>

		{#if shouldLoadEnvironment(environment) || isEnvironmentOnline(environment)}
			{#if snapshotLoading}
				<Skeleton class="h-4 w-64" />
			{:else}
				<div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm text-muted-foreground">
					<span>
						<span class="font-semibold text-foreground tabular-nums"
							>{overview.containers.runningContainers}/{overview.containers.totalContainers}</span
						>
						{String(m.common_running()).toLowerCase()}
					</span>
					<span class="text-border">·</span>
					<span>
						<span class="font-semibold text-foreground tabular-nums">{overview.imageUsageCounts.totalImages}</span>
						{String(m.images()).toLowerCase()}
					</span>
					<span class="text-border">·</span>
					<span>
						<span class="font-semibold text-foreground tabular-nums">{overview.actionItems.items.length}</span>
						{String(m.dashboard_action_items_title()).toLowerCase()}
					</span>
				</div>
			{/if}
		{:else}
			<div class="border-t border-border/60 pt-3 text-sm">
				<p class="font-medium">{m.dashboard_all_environment_unavailable_title()}</p>
				<p class="mt-1 text-muted-foreground">{m.dashboard_all_environment_unavailable_description()}</p>
			</div>
		{/if}

		{#if shouldLoadEnvironment(environment)}
			<div class="border-t border-border/60 pt-3">
				<div class="grid grid-cols-1 gap-1 {gpuMetric !== null ? 'sm:grid-cols-2 lg:grid-cols-4' : 'sm:grid-cols-3'}">
					{#if liveStatsLoading}
						{#each [1, 2, 3] as tile (tile)}
							<div class="min-w-0 px-2.5 py-2.5">
								<div class="flex items-start justify-between gap-2">
									<Skeleton class="h-3 w-20" />
									<Skeleton class="h-5 w-12" />
								</div>
								<Skeleton class="mt-2 h-3 w-24" />
								<Skeleton class="mt-3 h-1.5 w-full" />
							</div>
						{/each}
					{:else}
						<DashboardMetricTile
							title={m.cpu_usage()}
							icon={CpuIcon}
							value={formatPercent(cpuMetric)}
							label={getCpuMetricLabel(systemStats)}
							meterValue={cpuMetric}
						/>
						<DashboardMetricTile
							title={m.memory_usage()}
							icon={MemoryStickIcon}
							value={formatPercent(memoryMetric)}
							label={getCapacityLabel(systemStats?.memoryUsage, systemStats?.memoryTotal)}
							labelClass="truncate"
							meterValue={memoryMetric}
						/>
						<DashboardMetricTile
							title={m.dashboard_meter_disk()}
							icon={VolumesIcon}
							value={formatPercent(diskMetric)}
							label={getCapacityLabel(systemStats?.diskUsage, systemStats?.diskTotal)}
							labelClass="truncate"
							meterValue={diskMetric}
						/>
						{#if gpuMetric !== null}
							<DashboardMetricTile
								title={m.dashboard_meter_gpu()}
								icon={GpuIcon}
								value={formatPercent(gpuMetric)}
								label={getGpuMetricLabel(systemStats)}
								meterValue={gpuMetric}
							/>
						{/if}
					{/if}
				</div>
			</div>
		{/if}

		{#if overview.snapshotError}
			<div class="rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-xs text-red-700 dark:text-red-300">
				{m.dashboard_all_summary_unavailable({ error: overview.snapshotError })}
			</div>
		{/if}
	</Card.Content>
</Card.Root>

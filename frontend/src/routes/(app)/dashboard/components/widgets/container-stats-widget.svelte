<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';
	import { onDestroy } from 'svelte';

	import MetricRing from '#lib/components/metric-ring.svelte';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { containerDetailQuery } from '#lib/query/containers.js';
	import type { ContainerResourceSample } from '#lib/types/docker.js';
	import { cn } from '#lib/utils.js';
	import type { DashboardWidget } from '#lib/utils/dashboard-layout.js';
	import { rowKey } from '#lib/utils/environments.js';
	import { bytes, formatElapsedTime, instantEpochMilliseconds } from '#lib/utils/formatting.js';

	import { ContainerStatsManager } from '../../../containers/components/container-stats-manager.svelte';
	import ContainerStatsSync from '../../../containers/components/container-stats-sync.svelte';
	import { getContainerStatusLabel, getStateBadgeVariant, getUsageTone } from '../../../containers/container-table.helpers';
	import DashboardPanel from '../dashboard-panel.svelte';
	import WidgetStatus from './widget-status.svelte';
	import { resolveWidgetTarget } from './widget-target';

	type CounterField = 'networkRxBytes' | 'networkTxBytes' | 'blockReadBytes' | 'blockWriteBytes';

	const HEALTH_BADGES: Record<string, { label: () => string; variant: BadgeVariant }> = {
		healthy: { label: m.health_status_healthy, variant: 'green' },
		unhealthy: { label: m.health_status_unhealthy, variant: 'red' },
		starting: { label: m.health_status_starting, variant: 'amber' }
	};
	const TONE_TEXT = {
		default: 'text-foreground',
		warning: 'text-warning',
		destructive: 'text-destructive'
	};
	const SPARK_HEIGHT = 32;

	let { widget }: { widget: DashboardWidget } = $props();

	const statsManager = new ContainerStatsManager();
	onDestroy(() => statsManager.destroy());

	const config = $derived(widget.config ?? {});
	const environmentId = $derived(config.environmentIds?.[0] ?? '');
	const resourceId = $derived(config.resourceId ?? '');
	const target = $derived(resolveWidgetTarget(config, ['containers:read']));
	const key = $derived(rowKey({ id: resourceId, environmentId, environmentTagged: true }));
	const compact = $derived(widget.h < 6);

	const containerQuery = createQuery(() => ({
		...containerDetailQuery(environmentId, resourceId),
		enabled: target.status === 'ready'
	}));

	const targetIds = $derived(new Set(target.status === 'ready' && containerQuery.data?.state.running ? [key] : []));

	const history = $derived(statsManager.getHistory(key));
	const sample = $derived(statsManager.getSample(key));

	function rates(samples: ContainerResourceSample[], field: CounterField): number[] {
		return samples.slice(1).map((current, index) => {
			const previous = samples[index]!;
			const seconds =
				((instantEpochMilliseconds(current.sampleTime) ?? 0) - (instantEpochMilliseconds(previous.sampleTime) ?? 0)) / 1000;
			return seconds > 0 ? Math.max(0, (current[field] - previous[field]) / seconds) : 0;
		});
	}

	function sparkPoints(values: number[], max: number): string {
		const step = 100 / Math.max(1, values.length - 1);
		return values
			.map((value, index) => `${index * step},${SPARK_HEIGHT - (Math.min(value, max) / max) * SPARK_HEIGHT}`)
			.join(' ');
	}

	const cpuDetail = $derived.by(() => {
		const limit = (containerQuery.data?.hostConfig.nanoCpus ?? 0) / 1e9;
		if (limit <= 0) return m.no_cpu_limit();
		const unit = limit === 1 ? m.containers_stats_cpu_unit_singular() : m.common_cpus();
		return `${m.containers_stats_cpu_limit()}: ${limit} ${unit}`;
	});

	const cpuSeries = $derived(history.map((entry) => entry.cpuPercent));
	const memorySeries = $derived(history.map((entry) => entry.memoryUsageBytes));
	const rxSeries = $derived(rates(history, 'networkRxBytes'));
	const txSeries = $derived(rates(history, 'networkTxBytes'));
	const readRate = $derived(rates(history.slice(-2), 'blockReadBytes').at(-1));
	const writeRate = $derived(rates(history.slice(-2), 'blockWriteBytes').at(-1));
</script>

{#snippet containerLink()}
	{#if containerQuery.data}
		<a
			href={resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(environmentId)}`, {
				containerId: containerQuery.data.id
			})}
			class="min-w-0 truncate text-xs text-muted-foreground hover:underline"
		>
			{containerQuery.data.name}
		</a>
	{/if}
{/snippet}

{#snippet sparkline(series: { values: number[]; class: string }[], max: number)}
	<svg
		viewBox="0 0 100 {SPARK_HEIGHT}"
		preserveAspectRatio="none"
		class={cn('h-8 w-full overflow-visible', compact && 'lg:hidden')}
		aria-hidden="true"
	>
		{#each series as line (line.class)}
			{#if line.values.length > 1}
				{@const points = sparkPoints(line.values, max)}
				<polygon points="0,{SPARK_HEIGHT} {points} 100,{SPARK_HEIGHT}" class={cn('fill-current opacity-10', line.class)} />
				<polyline
					{points}
					fill="none"
					stroke="currentColor"
					stroke-width="1.5"
					stroke-linejoin="round"
					vector-effect="non-scaling-stroke"
					class={line.class}
				/>
			{/if}
		{/each}
	</svg>
{/snippet}

{#snippet gauge(
	variant: 'cpu' | 'memory',
	label: string,
	percent: number | undefined,
	detail: string,
	series: number[],
	max: number,
	lineClass: string
)}
	<div class="flex min-w-0 flex-col gap-3">
		<div class="flex min-w-0 flex-col items-center gap-3 @md:flex-row">
			<MetricRing percent={percent ?? 0} {variant} size={80} stroke={6}>
				<span class={cn('text-lg font-semibold tabular-nums', TONE_TEXT[getUsageTone(percent ?? 0)])}>
					{percent === undefined ? '--' : `${percent.toFixed(1)}%`}
				</span>
			</MetricRing>
			<div class="min-w-0 text-center @md:text-left">
				<p class="truncate text-sm font-medium">{label}</p>
				<p class="truncate text-xs text-muted-foreground tabular-nums" title={detail}>{detail}</p>
			</div>
		</div>
		{@render sparkline([{ values: series, class: lineClass }], max)}
	</div>
{/snippet}

{#snippet ioRow(label: string, rate: number | undefined, total: number)}
	<div class="flex min-w-0 items-baseline justify-between gap-2 text-xs">
		<span class="truncate text-muted-foreground">{label}</span>
		<span class="shrink-0 text-right tabular-nums">
			<span class="font-medium">{m.bytes_per_second({ value: bytes.format(rate ?? 0, { unitSeparator: ' ' }) ?? '' })}</span>
			<span class="text-muted-foreground">· {bytes.format(total, { unitSeparator: ' ' })}</span>
		</span>
	</div>
{/snippet}

<ContainerStatsSync {statsManager} {targetIds} />

<DashboardPanel title={widget.title || m.container_stats()} actions={containerLink}>
	<WidgetStatus {target} data={containerQuery.data} error={containerQuery.error} resource={m.container()}>
		{#snippet children(container)}
			{@const health = HEALTH_BADGES[container.state.health?.status ?? '']}
			<div class="@container flex flex-col gap-4 py-1">
				<div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
					<Badge variant={getStateBadgeVariant(container.state.status)} size="sm">
						{getContainerStatusLabel(container.state.status)}
					</Badge>
					{#if health}
						<Badge variant={health.variant} size="sm">{health.label()}</Badge>
					{/if}
					{#if container.state.running}
						<span class="text-xs text-muted-foreground">
							{m.common_uptime()}
							<span class="font-medium text-foreground tabular-nums">
								{formatElapsedTime(container.state.startedAt, { base: sample?.sampleTime })}
							</span>
						</span>
					{/if}
					<span class="min-w-0 flex-1 truncate text-right font-mono text-xs text-muted-foreground" title={container.image}>
						{container.image}
					</span>
				</div>

				{#if !container.state.running}
					<p class="py-2 text-sm text-muted-foreground">{m.containers_stats_unavailable()}</p>
				{:else if !sample && statsManager.hasError(key)}
					<p class="py-2 text-sm text-muted-foreground">{m.containers_stats_stream_failed()}</p>
				{:else if !sample}
					<div class="grid grid-cols-2 gap-3">
						{#each [0, 1] as slot (slot)}
							<div class="flex flex-col items-center gap-3 @md:flex-row">
								<div class="size-20 shrink-0 animate-pulse rounded-full bg-muted"></div>
								<div class="h-4 w-24 animate-pulse rounded bg-muted"></div>
							</div>
						{/each}
					</div>
				{:else}
					{@const memoryPercent = sample.memoryLimitBytes ? (sample.memoryUsageBytes / sample.memoryLimitBytes) * 100 : undefined}
					<div class="grid grid-cols-2 gap-6">
						{@render gauge(
							'cpu',
							m.cpu_usage(),
							sample.cpuPercent,
							cpuDetail,
							cpuSeries,
							Math.max(5, ...cpuSeries),
							'text-orange-500'
						)}
						{@render gauge(
							'memory',
							m.memory_usage(),
							memoryPercent,
							`${bytes.format(sample.memoryUsageBytes, { unitSeparator: ' ' })} / ${bytes.format(sample.memoryLimitBytes, { unitSeparator: ' ' })}`,
							memorySeries,
							Math.max(1, ...memorySeries),
							'text-violet-500'
						)}
					</div>

					<div class="grid grid-cols-2 gap-x-6 gap-y-4 border-t border-border/50 pt-4 @lg:grid-cols-3">
						<div class="col-span-2 flex min-w-0 flex-col gap-2 @lg:col-span-1">
							<p class="text-xs font-medium">{m.containers_network_io()}</p>
							{@render ioRow(m.containers_network_received(), rxSeries.at(-1), sample.networkRxBytes)}
							{@render ioRow(m.containers_network_transmitted(), txSeries.at(-1), sample.networkTxBytes)}
							{@render sparkline(
								[
									{ values: rxSeries, class: 'text-sky-500' },
									{ values: txSeries, class: 'text-emerald-500' }
								],
								Math.max(1024, ...rxSeries, ...txSeries)
							)}
						</div>
						<div class="flex min-w-0 flex-col gap-2">
							<p class="text-xs font-medium">{m.containers_block_io()}</p>
							{@render ioRow(m.containers_block_io_read(), readRate, sample.blockReadBytes)}
							{@render ioRow(m.containers_block_io_write(), writeRate, sample.blockWriteBytes)}
						</div>
						<div class="flex min-w-0 flex-col gap-1">
							<p class="text-xs font-medium">{m.podman_pids()}</p>
							<p class="text-2xl font-semibold tabular-nums">{sample.pids}</p>
						</div>
					</div>
				{/if}
			</div>
		{/snippet}
	</WidgetStatus>
</DashboardPanel>

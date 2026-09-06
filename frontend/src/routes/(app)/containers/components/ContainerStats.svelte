<script lang="ts">
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { bytes } from '#lib/utils/formatting.js';
	import type { ContainerDetailsDto, ContainerStats as ContainerStatsType } from '#lib/types/docker.js';
	import { DetailMetaStrip, DetailSection, type DetailMetaItem } from '#lib/components/resource-detail/index.js';

	interface Props {
		container: ContainerDetailsDto;
		stats: ContainerStatsType | null;
		cpuUsagePercent: number;
		cpuLimit: number;
		memoryUsageFormatted: string;
		memoryLimitFormatted: string;
		memoryUsagePercent: number;
		loading?: boolean;
	}

	let {
		container,
		stats,
		cpuUsagePercent,
		cpuLimit,
		memoryUsageFormatted,
		memoryLimitFormatted,
		memoryUsagePercent,
		loading = false
	}: Props = $props();

	const networkInterfaces = $derived(stats?.networks ? Object.entries(stats.networks) : []);

	const totalNetworkRx = $derived.by(() => {
		if (!stats?.networks) return 0;
		return Object.values(stats.networks).reduce((acc, net) => acc + (net.rx_bytes || 0), 0);
	});

	const totalNetworkTx = $derived.by(() => {
		if (!stats?.networks) return 0;
		return Object.values(stats.networks).reduce((acc, net) => acc + (net.tx_bytes || 0), 0);
	});

	const totalNetworkRxPackets = $derived.by(() => {
		if (!stats?.networks) return 0;
		return Object.values(stats.networks).reduce((acc, net) => acc + (net.rx_packets || 0), 0);
	});

	const totalNetworkTxPackets = $derived.by(() => {
		if (!stats?.networks) return 0;
		return Object.values(stats.networks).reduce((acc, net) => acc + (net.tx_packets || 0), 0);
	});

	const blockIoRead = $derived.by(() => {
		if (!stats?.blkio_stats?.io_service_bytes_recursive) return 0;
		return stats.blkio_stats.io_service_bytes_recursive
			.filter((item) => item.op.toLowerCase() === 'read')
			.reduce((acc, item) => acc + item.value, 0);
	});

	const blockIoWrite = $derived.by(() => {
		if (!stats?.blkio_stats?.io_service_bytes_recursive) return 0;
		return stats.blkio_stats.io_service_bytes_recursive
			.filter((item) => item.op.toLowerCase() === 'write')
			.reduce((acc, item) => acc + item.value, 0);
	});

	const memoryCacheBytes = $derived(stats?.memory_stats?.stats?.file ?? stats?.memory_stats?.stats?.['cache'] ?? 0);
	const memoryActiveBytes = $derived(stats?.memory_stats?.stats?.active_anon || 0);
	const memoryInactiveBytes = $derived(stats?.memory_stats?.stats?.inactive_anon || 0);

	const hasBlockIo = $derived(!!stats?.blkio_stats?.io_service_bytes_recursive?.length);

	const statItems = $derived.by(() => {
		const items: DetailMetaItem[] = [];
		if (stats?.pids_stats?.current !== undefined) {
			let value = String(stats.pids_stats.current);
			if (stats.pids_stats.limit && stats.pids_stats.limit < Number.MAX_SAFE_INTEGER) {
				value += ` / ${stats.pids_stats.limit}`;
			}
			items.push({ label: m.containers_process_count(), value });
		}
		items.push({
			label: m.containers_network_received(),
			value: `${bytes.format(totalNetworkRx)} · ${totalNetworkRxPackets} ${m.containers_stats_packets()}`
		});
		items.push({
			label: m.containers_network_transmitted(),
			value: `${bytes.format(totalNetworkTx)} · ${totalNetworkTxPackets} ${m.containers_stats_packets()}`
		});
		if (hasBlockIo) {
			items.push({ label: m.containers_block_io_read(), value: bytes.format(blockIoRead) ?? '' });
			items.push({ label: m.containers_block_io_write(), value: bytes.format(blockIoWrite) ?? '' });
		}
		return items;
	});
</script>

{#snippet statRow(label: string, value: string | null)}
	<div class="flex justify-between">
		<span class="text-muted-foreground">{label}:</span>
		<span class="font-medium text-foreground">{value}</span>
	</div>
{/snippet}

{#snippet progressBarSkeleton()}
	<div class="space-y-3">
		<div class="flex items-start justify-between">
			<Skeleton class="h-5 w-20" />
			<Skeleton class="h-5 w-12" />
		</div>
		<Skeleton class="h-3 w-full" />
		<div class="grid grid-cols-2 gap-x-4 gap-y-2">
			{#each Array(4) as _, i (i)}
				<div class="flex justify-between">
					<Skeleton class="h-3 w-24" />
					<Skeleton class="h-3 w-16" />
				</div>
			{/each}
		</div>
	</div>
{/snippet}

{#if loading}
	<div class="space-y-6">
		<div class="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-lg bg-muted/40 px-4 py-3">
			{#each Array(3) as _, i (i)}
				<Skeleton class="h-4 w-32" />
			{/each}
		</div>
		{#each Array(2) as _, i (i)}
			<div class="space-y-4 border-t pt-6">
				{@render progressBarSkeleton()}
			</div>
		{/each}
		<div class="space-y-4 border-t pt-6">
			<Skeleton class="h-4 w-36" />
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				{#each Array(4) as _, i (i)}
					<div class="space-y-2 rounded-lg border border-border/50 p-3">
						<Skeleton class="h-4 w-16" />
						<Skeleton class="h-3 w-full" />
						<Skeleton class="h-3 w-full" />
					</div>
				{/each}
			</div>
		</div>
	</div>
{:else if stats && container.state?.running}
	<div class="space-y-6">
		<DetailMetaStrip items={statItems} />

		<DetailSection title={m.cpu_usage()}>
			<div class="space-y-3">
				<div class="text-right text-sm font-semibold text-muted-foreground">
					{cpuUsagePercent.toFixed(2)}%
				</div>
				<Progress
					value={cpuUsagePercent}
					max={100}
					class="h-3 {cpuUsagePercent > 80 ? '[&>div]:bg-destructive' : cpuUsagePercent > 60 ? '[&>div]:bg-warning' : ''}"
				/>
				{#if stats.cpu_stats}
					<div class="flex items-center justify-between text-xs text-muted-foreground">
						{#if cpuLimit > 0}
							<span
								>{m.containers_stats_cpu_limit()}: {cpuLimit}
								{cpuLimit === 1 ? m.containers_stats_cpu_unit_singular() : m.common_cpus()}</span
							>
						{:else}
							<span>{m.containers_stats_online_cpus()}: {stats.cpu_stats.online_cpus}</span>
						{/if}
						<span>
							{m.system()}: {((stats.cpu_stats.system_cpu_usage || 0) / 1e9).toFixed(2)}s
						</span>
					</div>
				{/if}
			</div>
			{#if stats.cpu_stats}
				<div class="grid grid-cols-1 gap-x-4 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-3">
					{@render statRow(m.containers_stats_online_cpus(), String(stats.cpu_stats.online_cpus))}
					{@render statRow(
						m.containers_stats_cpu_user_mode(),
						`${(stats.cpu_stats.cpu_usage.usage_in_usermode / 1000000000).toFixed(2)}s`
					)}
					{@render statRow(
						m.containers_stats_cpu_kernel_mode(),
						`${(stats.cpu_stats.cpu_usage.usage_in_kernelmode / 1000000000).toFixed(2)}s`
					)}
					{#if stats.cpu_stats.throttling_data}
						{@render statRow(
							m.containers_stats_cpu_throttled_periods(),
							String(stats.cpu_stats.throttling_data.throttled_periods)
						)}
						{@render statRow(
							m.containers_stats_cpu_throttled_time(),
							`${(stats.cpu_stats.throttling_data.throttled_time / 1000000000).toFixed(2)}s`
						)}
					{/if}
				</div>
			{/if}
		</DetailSection>

		<DetailSection title={m.memory_usage()}>
			<div class="space-y-3">
				<div class="text-right">
					<div class="text-sm font-semibold text-muted-foreground">
						{memoryUsagePercent.toFixed(1)}%
					</div>
					<div class="text-xs text-muted-foreground">
						{memoryUsageFormatted} / {memoryLimitFormatted}
					</div>
				</div>
				<Progress
					value={memoryUsagePercent}
					max={100}
					class="h-3 {memoryUsagePercent > 80 ? '[&>div]:bg-destructive' : memoryUsagePercent > 60 ? '[&>div]:bg-warning' : ''}"
				/>
			</div>
			<div class="grid grid-cols-1 gap-x-4 gap-y-2 text-xs sm:grid-cols-2 lg:grid-cols-3">
				{@render statRow(m.containers_stats_cache(), bytes.format(memoryCacheBytes))}
				{@render statRow(m.common_active(), bytes.format(memoryActiveBytes))}
				{@render statRow(m.inactive(), bytes.format(memoryInactiveBytes))}
				{#if stats.memory_stats?.stats}
					{@render statRow(m.containers_stats_memory_rss(), bytes.format(stats.memory_stats.stats.anon || 0))}
					{@render statRow(m.containers_stats_memory_active_file(), bytes.format(stats.memory_stats.stats.active_file || 0))}
					{@render statRow(m.containers_stats_memory_inactive_file(), bytes.format(stats.memory_stats.stats.inactive_file || 0))}
					{#if stats.memory_stats.stats.pgfault}
						{@render statRow(m.containers_stats_memory_page_faults(), stats.memory_stats.stats.pgfault.toLocaleString())}
					{/if}
					{#if stats.memory_stats.stats.pgmajfault}
						{@render statRow(m.containers_stats_memory_major_faults(), stats.memory_stats.stats.pgmajfault.toLocaleString())}
					{/if}
				{/if}
			</div>
		</DetailSection>

		{#if networkInterfaces.length > 0}
			<DetailSection title={m.containers_stats_network_interfaces()}>
				<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
					{#each networkInterfaces as [interfaceName, interfaceStats] (interfaceName)}
						<div class="rounded-lg border border-border/50 p-3">
							<div class="mb-2 text-sm font-semibold text-foreground">
								{interfaceName}
							</div>
							<div class="space-y-1 text-xs">
								{@render statRow('RX', bytes.format(interfaceStats.rx_bytes))}
								{@render statRow('TX', bytes.format(interfaceStats.tx_bytes))}
								{#if interfaceStats.rx_errors > 0 || interfaceStats.tx_errors > 0}
									<div class="mt-1 text-xs text-destructive">
										{m.containers_stats_errors()}: {interfaceStats.rx_errors + interfaceStats.tx_errors}
									</div>
								{/if}
								{#if interfaceStats.rx_dropped > 0 || interfaceStats.tx_dropped > 0}
									<div class="text-warning mt-1 text-xs">
										{m.containers_stats_dropped()}: {interfaceStats.rx_dropped + interfaceStats.tx_dropped}
									</div>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			</DetailSection>
		{/if}
	</div>
{:else if !container.state?.running}
	<div class="rounded-lg border border-dashed py-12 text-center text-muted-foreground">
		<div class="text-sm">{m.containers_stats_unavailable()}</div>
	</div>
{:else}
	<div class="rounded-lg border border-dashed py-12 text-center text-muted-foreground">
		<div class="text-sm">{m.containers_stats_loading()}</div>
	</div>
{/if}

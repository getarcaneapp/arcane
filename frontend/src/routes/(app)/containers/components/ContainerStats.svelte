<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import ActivityIcon from '@lucide/svelte/icons/activity';
	import { Progress } from '$lib/components/ui/progress';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { m } from '$lib/paraglide/messages';
	import bytes from 'bytes';
	import type { ContainerDetailsDto, ContainerStats as ContainerStatsType } from '$lib/types/container.type';

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

	const memoryCacheBytes = $derived(stats?.memory_stats?.stats?.file ?? stats?.memory_stats?.stats?.cache ?? 0);
	const memoryActiveBytes = $derived(stats?.memory_stats?.stats?.active_anon || 0);
	const memoryInactiveBytes = $derived(stats?.memory_stats?.stats?.inactive_anon || 0);
</script>

{#snippet progressBarSkeleton()}
	<div class="space-y-3">
		<div class="flex min-h-[44px] items-start justify-between">
			<Skeleton class="h-6 w-20" />
			<div class="text-right">
				<Skeleton class="mb-1 h-5 w-12" />
				<Skeleton class="h-4 w-24" />
			</div>
		</div>
		<Skeleton class="h-3 w-full" />
		<div class="mt-2 flex items-center justify-between">
			<Skeleton class="h-3 w-24" />
			<Skeleton class="h-3 w-28" />
		</div>
	</div>
{/snippet}

{#snippet statCardSkeleton()}
	<Card.Root variant="subtle" class="flex flex-col">
		<Card.Content class="flex flex-col p-4">
			<Skeleton class="mb-3 h-3 w-24" />
			<div class="grid flex-1 grid-cols-2 gap-3">
				<div class="space-y-1">
					<Skeleton class="h-3 w-16" />
					<Skeleton class="h-5 w-20" />
					<Skeleton class="h-3 w-24" />
				</div>
				<div class="space-y-1">
					<Skeleton class="h-3 w-16" />
					<Skeleton class="h-5 w-20" />
					<Skeleton class="h-3 w-24" />
				</div>
			</div>
		</Card.Content>
	</Card.Root>
{/snippet}

{#snippet processCardSkeleton()}
	<Card.Root variant="subtle" class="flex flex-col justify-center">
		<Card.Content class="flex flex-col justify-center p-4">
			<Skeleton class="mb-2 h-3 w-32" />
			<Skeleton class="h-8 w-16" />
			<Skeleton class="mt-1 h-3 w-24" />
		</Card.Content>
	</Card.Root>
{/snippet}

{#snippet networkInterfacesSkeleton()}
	<Card.Root variant="subtle">
		<Card.Content class="p-4">
			<Skeleton class="mb-3 h-3 w-36" />
			<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
				{#each Array(4) as _}
					<Card.Root class="flex flex-col">
						<Card.Content class="flex flex-col p-3">
							<Skeleton class="mb-2 h-4 w-16" />
							<div class="flex-1 space-y-1">
								<div class="flex justify-between">
									<Skeleton class="h-3 w-8" />
									<Skeleton class="h-3 w-16" />
								</div>
								<div class="flex justify-between">
									<Skeleton class="h-3 w-8" />
									<Skeleton class="h-3 w-16" />
								</div>
							</div>
						</Card.Content>
					</Card.Root>
				{/each}
			</div>
		</Card.Content>
	</Card.Root>
{/snippet}

{#snippet detailsSkeleton()}
	<Card.Root variant="subtle" class="flex flex-col">
		<Card.Content class="flex flex-col p-4">
			<Skeleton class="mb-3 h-3 w-28" />
			<div class="grid flex-1 grid-cols-2 gap-x-4 gap-y-2">
				{#each Array(8) as _}
					<div class="flex justify-between">
						<Skeleton class="h-3 w-24" />
						<Skeleton class="h-3 w-16" />
					</div>
				{/each}
			</div>
		</Card.Content>
	</Card.Root>
{/snippet}

<Card.Root>
	<Card.Header icon={ActivityIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>
					{m.containers_resource_metrics()}
				</h2>
			</Card.Title>
			<Card.Description>{m.containers_resource_metrics_description()}</Card.Description>
		</div>
	</Card.Header>
	<Card.Content class="p-4">
		{#if loading}
			<div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
				<div class="lg:col-span-2">
					{@render progressBarSkeleton()}
				</div>
				<div class="lg:col-span-2">
					{@render progressBarSkeleton()}
				</div>
			</div>

			<div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{@render processCardSkeleton()}
				{@render statCardSkeleton()}
				{@render statCardSkeleton()}
			</div>

			<div class="mt-4">
				{@render networkInterfacesSkeleton()}
			</div>

			<div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
				{@render detailsSkeleton()}
				{@render detailsSkeleton()}
			</div>
		{:else if stats && container.state?.running}
			<div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
				<!-- CPU Usage -->
				<div class="lg:col-span-2">
					<div class="space-y-3">
						<div class="flex min-h-[44px] items-start justify-between">
							<span class="text-foreground text-base font-bold">{m.dashboard_meter_cpu()}</span>
							<div class="text-right">
								<div class="text-muted-foreground text-sm font-semibold">
									{cpuUsagePercent.toFixed(2)}%
								</div>
							</div>
						</div>
						<Progress
							value={cpuUsagePercent}
							max={100}
							class="h-3 {cpuUsagePercent > 80 ? '[&>div]:bg-destructive' : cpuUsagePercent > 60 ? '[&>div]:bg-warning' : ''}"
						/>
						{#if stats.cpu_stats}
							<div class="text-muted-foreground mt-2 flex items-center justify-between text-xs">
								{#if cpuLimit > 0}
									<span
										>{m.containers_stats_cpu_limit()}: {cpuLimit}
										{cpuLimit === 1 ? m.containers_stats_cpu_unit_singular() : m.common_cpus()}</span
									>
								{:else}
									<span>{m.containers_stats_online_cpus()}: {stats.cpu_stats.online_cpus}</span>
								{/if}
								<span>
									{m.containers_stats_system_cpu()}: {((stats.cpu_stats.system_cpu_usage || 0) / 1e9).toFixed(2)}s
								</span>
							</div>
						{/if}
					</div>
				</div>

				<!-- Memory Usage -->
				<div class="lg:col-span-2">
					<div class="space-y-3">
						<div class="flex min-h-[44px] items-start justify-between">
							<span class="text-foreground text-base font-bold">{m.dashboard_meter_memory()}</span>
							<div class="text-right">
								<div class="text-muted-foreground text-sm font-semibold">
									{memoryUsagePercent.toFixed(1)}%
								</div>
								<div class="text-muted-foreground text-xs">
									{memoryUsageFormatted} / {memoryLimitFormatted}
								</div>
							</div>
						</div>
						<Progress
							value={memoryUsagePercent}
							max={100}
							class="h-3 {memoryUsagePercent > 80
								? '[&>div]:bg-destructive'
								: memoryUsagePercent > 60
									? '[&>div]:bg-warning'
									: ''}"
						/>
						<div class="text-muted-foreground mt-2 grid grid-cols-3 gap-2 text-xs">
							<div>
								<div class="font-medium">{m.containers_stats_cache()}</div>
								<div>{bytes.format(memoryCacheBytes)}</div>
							</div>
							<div>
								<div class="font-medium">{m.containers_stats_active()}</div>
								<div>{bytes.format(memoryActiveBytes)}</div>
							</div>
							<div>
								<div class="font-medium">{m.containers_stats_inactive()}</div>
								<div>{bytes.format(memoryInactiveBytes)}</div>
							</div>
						</div>
					</div>
				</div>
			</div>

			<div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				<!-- Process Count -->
				{#if stats.pids_stats && stats.pids_stats.current !== undefined}
					<Card.Root variant="subtle" class="flex flex-col justify-center">
						<Card.Content class="flex flex-col justify-center p-4">
							<div class="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase">
								{m.containers_process_count()}
							</div>
							<div class="text-foreground text-2xl font-bold">{stats.pids_stats.current}</div>
							{#if stats.pids_stats.limit && stats.pids_stats.limit < Number.MAX_SAFE_INTEGER}
								<div class="text-muted-foreground mt-1 text-xs">
									{m.common_limit()}: {stats.pids_stats.limit}
								</div>
							{/if}
						</Card.Content>
					</Card.Root>
				{/if}

				<!-- Network I/O Summary -->
				<Card.Root variant="subtle" class="flex flex-col">
					<Card.Content class="flex flex-col p-4">
						<div class="text-muted-foreground mb-3 text-xs font-semibold tracking-wide uppercase">
							{m.containers_network_io()}
						</div>
						<div class="grid flex-1 grid-cols-2 gap-3">
							<div>
								<div class="text-muted-foreground text-xs font-medium">
									{m.containers_network_received()}
								</div>
								<div class="text-foreground text-sm font-bold">
									{bytes.format(totalNetworkRx)}
								</div>
								<div class="text-muted-foreground text-xs">
									{totalNetworkRxPackets}
									{m.containers_stats_packets()}
								</div>
							</div>
							<div>
								<div class="text-muted-foreground text-xs font-medium">
									{m.containers_network_transmitted()}
								</div>
								<div class="text-foreground text-sm font-bold">
									{bytes.format(totalNetworkTx)}
								</div>
								<div class="text-muted-foreground text-xs">
									{totalNetworkTxPackets}
									{m.containers_stats_packets()}
								</div>
							</div>
						</div>
					</Card.Content>
				</Card.Root>

				<!-- Block I/O -->
				{#if stats.blkio_stats && stats.blkio_stats.io_service_bytes_recursive && stats.blkio_stats.io_service_bytes_recursive.length > 0}
					<Card.Root variant="subtle" class="flex flex-col">
						<Card.Content class="flex flex-col p-4">
							<div class="text-muted-foreground mb-3 text-xs font-semibold tracking-wide uppercase">
								{m.containers_block_io()}
							</div>
							<div class="grid flex-1 grid-cols-2 gap-3">
								<div>
									<div class="text-muted-foreground text-xs font-medium">
										{m.containers_block_io_read()}
									</div>
									<div class="text-foreground text-sm font-bold">
										{bytes.format(blockIoRead)}
									</div>
								</div>
								<div>
									<div class="text-muted-foreground text-xs font-medium">
										{m.containers_block_io_write()}
									</div>
									<div class="text-foreground text-sm font-bold">
										{bytes.format(blockIoWrite)}
									</div>
								</div>
							</div>
						</Card.Content>
					</Card.Root>
				{/if}
			</div>

			<!-- Network Interfaces Details -->
			{#if networkInterfaces.length > 0}
				<div class="mt-4">
					<Card.Root variant="subtle">
						<Card.Content class="p-4">
							<div class="text-muted-foreground mb-3 text-xs font-semibold tracking-wide uppercase">
								{m.containers_stats_network_interfaces()}
							</div>
							<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
								{#each networkInterfaces as [interfaceName, interfaceStats]}
									<Card.Root variant="outlined" class="flex flex-col">
										<Card.Content class="flex flex-col p-3">
											<div class="text-foreground mb-2 text-sm font-semibold">
												{interfaceName}
											</div>
											<div class="flex-1 space-y-1 text-xs">
												<div class="flex justify-between">
													<span class="text-muted-foreground">RX:</span>
													<span class="text-foreground font-medium">{bytes.format(interfaceStats.rx_bytes)}</span>
												</div>
												<div class="flex justify-between">
													<span class="text-muted-foreground">TX:</span>
													<span class="text-foreground font-medium">{bytes.format(interfaceStats.tx_bytes)}</span>
												</div>
												{#if interfaceStats.rx_errors > 0 || interfaceStats.tx_errors > 0}
													<div class="text-destructive mt-1 text-xs">
														{m.containers_stats_errors()}: {interfaceStats.rx_errors + interfaceStats.tx_errors}
													</div>
												{/if}
												{#if interfaceStats.rx_dropped > 0 || interfaceStats.tx_dropped > 0}
													<div class="text-warning mt-1 text-xs">
														{m.containers_stats_dropped()}: {interfaceStats.rx_dropped + interfaceStats.tx_dropped}
													</div>
												{/if}
											</div>
										</Card.Content>
									</Card.Root>
								{/each}
							</div>
						</Card.Content>
					</Card.Root>
				</div>
			{/if}

			<div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
				<!-- Memory Details -->
				{#if stats.memory_stats?.stats}
					<Card.Root variant="subtle" class="flex flex-col">
						<Card.Content class="flex flex-col p-4">
							<div class="text-muted-foreground mb-3 text-xs font-semibold tracking-wide uppercase">
								{m.containers_stats_memory_details()}
							</div>
							<div class="grid flex-1 grid-cols-2 gap-x-4 gap-y-2 text-xs">
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.common_usage()}:</span>
									<span class="text-foreground font-medium">{memoryUsageFormatted}</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.common_limit()}:</span>
									<span class="text-foreground font-medium">{memoryLimitFormatted}</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_cache()}:</span>
									<span class="text-foreground font-medium">{bytes.format(memoryCacheBytes)}</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_memory_rss()}:</span>
									<span class="text-foreground font-medium">{bytes.format(stats.memory_stats.stats.anon || 0)}</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_memory_active_file()}:</span>
									<span class="text-foreground font-medium">{bytes.format(stats.memory_stats.stats.active_file || 0)}</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_memory_inactive_file()}:</span>
									<span class="text-foreground font-medium">{bytes.format(stats.memory_stats.stats.inactive_file || 0)}</span>
								</div>
								{#if stats.memory_stats.stats.pgfault}
									<div class="flex justify-between">
										<span class="text-muted-foreground">{m.containers_stats_memory_page_faults()}:</span>
										<span class="text-foreground font-medium">{stats.memory_stats.stats.pgfault.toLocaleString()}</span>
									</div>
								{/if}
								{#if stats.memory_stats.stats.pgmajfault}
									<div class="flex justify-between">
										<span class="text-muted-foreground">{m.containers_stats_memory_major_faults()}:</span>
										<span class="text-foreground font-medium">{stats.memory_stats.stats.pgmajfault.toLocaleString()}</span>
									</div>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/if}

				<!-- CPU Details -->
				{#if stats.cpu_stats}
					<Card.Root variant="subtle" class="flex flex-col">
						<Card.Content class="flex flex-col p-4">
							<div class="text-muted-foreground mb-3 text-xs font-semibold tracking-wide uppercase">
								{m.containers_stats_cpu_details()}
							</div>
							<div class="grid flex-1 grid-cols-2 gap-x-4 gap-y-2 text-xs">
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_online_cpus()}:</span>
									<span class="text-foreground font-medium">{stats.cpu_stats.online_cpus}</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.common_usage()}:</span>
									<span class="text-foreground font-medium">{cpuUsagePercent.toFixed(2)}%</span>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_cpu_user_mode()}:</span>
									<span class="text-foreground font-medium"
										>{(stats.cpu_stats.cpu_usage.usage_in_usermode / 1000000000).toFixed(2)}s</span
									>
								</div>
								<div class="flex justify-between">
									<span class="text-muted-foreground">{m.containers_stats_cpu_kernel_mode()}:</span>
									<span class="text-foreground font-medium"
										>{(stats.cpu_stats.cpu_usage.usage_in_kernelmode / 1000000000).toFixed(2)}s</span
									>
								</div>
								{#if stats.cpu_stats.throttling_data}
									<div class="flex justify-between">
										<span class="text-muted-foreground">{m.containers_stats_cpu_throttled_periods()}:</span>
										<span class="text-foreground font-medium">{stats.cpu_stats.throttling_data.throttled_periods}</span>
									</div>
									<div class="flex justify-between">
										<span class="text-muted-foreground">{m.containers_stats_cpu_throttled_time()}:</span>
										<span class="text-foreground font-medium"
											>{(stats.cpu_stats.throttling_data.throttled_time / 1000000000).toFixed(2)}s</span
										>
									</div>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/if}
			</div>
		{:else if !container.state?.running}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="text-sm">{m.containers_stats_unavailable()}</div>
			</div>
		{:else}
			<div class="text-muted-foreground rounded-lg border border-dashed py-12 text-center">
				<div class="text-sm">{m.containers_stats_loading()}</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>

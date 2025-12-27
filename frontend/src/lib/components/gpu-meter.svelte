<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { Progress } from '$lib/components/ui/progress/index.js';
	import { m } from '$lib/paraglide/messages';
	import bytes from 'bytes';
	import type { GPUStats } from '$lib/types/system-stats.type';
	import { GpuIcon } from '$lib/icons';

	interface Props {
		gpus?: GPUStats[];
		loading?: boolean;
	}

	let { gpus, loading = false }: Props = $props();

	function formatBytes(bytesValue: number): string {
		return bytes.format(bytesValue, { unitSeparator: ' ' }) ?? '-';
	}

	function getPercentage(used: number, total: number): number {
		if (total === 0) return 0;
		return Math.min(100, (used / total) * 100);
	}
</script>

<Card.Root class="flex h-full flex-col">
	{#snippet children()}
		<Card.Header icon={GpuIcon} iconVariant="primary" compact {loading}>
			{#snippet children()}
				<div class="min-w-0 flex-1">
					<div class="text-foreground text-sm font-semibold">{m.dashboard_meter_gpu()}</div>
					{#if gpus && gpus.length > 0}
						<div class="text-muted-foreground text-xs">
							{gpus.length}
							{gpus.length === 1 ? m.dashboard_meter_gpu_device() : m.dashboard_meter_gpu_devices()}
						</div>
					{/if}
				</div>
			{/snippet}
		</Card.Header>

		<Card.Content class="flex flex-1 flex-col justify-center gap-3 p-3 sm:p-4">
			{#if loading}
				<div class="space-y-3">
					<div class="flex items-center gap-3">
						<div class="bg-muted h-2.5 flex-1 animate-pulse rounded-full"></div>
						<div class="bg-muted h-4 w-12 animate-pulse rounded"></div>
					</div>
					<div class="flex justify-between gap-4">
						<div class="bg-muted h-4 w-16 animate-pulse rounded"></div>
						<div class="bg-muted h-4 w-20 animate-pulse rounded"></div>
					</div>
				</div>
			{:else if !gpus || gpus.length === 0}
				<div class="text-muted-foreground text-center text-xs">
					{m.common_na()}
				</div>
			{:else}
				<div class="space-y-4">
					{#each gpus as gpu, i}
						{@const percentage = getPercentage(gpu.memoryUsed, gpu.memoryTotal)}
						<div class="space-y-2">
							{#if gpus.length > 1}
								<div class="flex items-center justify-between">
									<span class="text-foreground truncate text-xs font-medium">{gpu.name}</span>
									<span class="text-muted-foreground shrink-0 font-mono text-[10px]">GPU {gpu.index}</span>
								</div>
							{/if}

							<div class="flex items-center gap-3">
								<Progress value={percentage} max={100} class="h-2.5 flex-1 bg-emerald-500/20 [&>div]:bg-emerald-500" />
								<span class="text-foreground min-w-12 text-right text-sm font-bold tabular-nums">
									{percentage.toFixed(1)}%
								</span>
							</div>

							<div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
								<div class="flex items-center gap-2 whitespace-nowrap">
									<div class="size-2 shrink-0 rounded-full bg-emerald-500"></div>
									<span class="text-muted-foreground text-xs">{m.dashboard_meter_disk_used()}</span>
									<span class="text-foreground text-sm font-semibold tabular-nums">{formatBytes(gpu.memoryUsed)}</span>
								</div>
								<div class="flex items-center gap-2 whitespace-nowrap">
									<div class="size-2 shrink-0 rounded-full bg-emerald-500/20"></div>
									<span class="text-muted-foreground text-xs">{m.dashboard_meter_disk_free()}</span>
									<span class="text-foreground text-sm font-semibold tabular-nums"
										>{formatBytes(gpu.memoryTotal - gpu.memoryUsed)}</span
									>
								</div>
							</div>
						</div>

						{#if i < gpus.length - 1}
							<div class="border-border/50 border-t"></div>
						{/if}
					{/each}
				</div>
			{/if}
		</Card.Content>
	{/snippet}
</Card.Root>

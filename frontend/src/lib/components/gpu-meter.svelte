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

<Card.Root>
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

		<Card.Content class="flex flex-col justify-center p-3">
			{#if loading}
				<div class="w-full space-y-3">
					<div class="bg-muted h-16 w-full animate-pulse rounded"></div>
				</div>
			{:else if !gpus || gpus.length === 0}
				<div class="text-muted-foreground text-center text-xs">
					{m.common_na()}
				</div>
			{:else}
				<div class="w-full space-y-3">
					{#each gpus as gpu}
						<div class="space-y-1.5">
							<div class="flex items-center justify-between">
								<span class="text-foreground text-xs font-medium">{gpu.name}</span>
								<span class="text-muted-foreground font-mono text-[10px]">
									GPU {gpu.index}
								</span>
							</div>
							<div class="text-center">
								<div class="text-foreground text-sm font-bold">
									{formatBytes(gpu.memoryUsed)}
								</div>
							</div>
							<Progress value={getPercentage(gpu.memoryUsed, gpu.memoryTotal)} max={100} class="h-1.5" />
							<div class="flex items-center justify-between text-xs">
								<span class="text-muted-foreground font-medium">
									{getPercentage(gpu.memoryUsed, gpu.memoryTotal).toFixed(1)}%
								</span>
								<span class="text-muted-foreground/70 font-mono text-[10px]">
									{formatBytes(gpu.memoryUsed)} / {formatBytes(gpu.memoryTotal)}
								</span>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</Card.Content>
	{/snippet}
</Card.Root>

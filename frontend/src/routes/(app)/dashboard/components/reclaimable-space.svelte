<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { environmentColor } from '#lib/stores/environment-colors.store.svelte.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { SystemDiskUsage } from '#lib/types/shared.js';
	import { bytes } from '#lib/utils/formatting.js';

	import { totalReclaimableBytes } from '../overview.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	const ENVIRONMENT_LIMIT = 3;

	let {
		usage,
		loading,
		failed,
		onPrune
	}: {
		usage: { environment: EnvironmentRef; data: SystemDiskUsage }[];
		loading: boolean;
		failed: boolean;
		onPrune?: () => void;
	} = $props();

	const RESOURCE_TYPES: { key: string; label: () => string; bytes: (data: SystemDiskUsage) => number }[] = [
		{ key: 'images', label: m.prune_unused_images, bytes: (data) => data.unusedImagesBytes },
		{ key: 'buildCache', label: m.build_cache, bytes: (data) => data.buildCacheBytes },
		{ key: 'volumes', label: m.unused_volumes, bytes: (data) => data.unusedVolumesBytes },
		{ key: 'containers', label: m.prune_stopped_containers, bytes: (data) => data.stoppedContainersBytes }
	];

	const resources = $derived(
		RESOURCE_TYPES.map((type) => {
			const segments = usage
				.map((entry) => ({ environment: entry.environment, bytes: type.bytes(entry.data) }))
				.filter((segment) => segment.bytes > 0);
			return {
				key: type.key,
				label: type.label(),
				total: segments.reduce((sum, segment) => sum + segment.bytes, 0),
				segments
			};
		}).filter((resource) => resource.total > 0)
	);
	const total = $derived(resources.reduce((sum, resource) => sum + resource.total, 0));
	const topEnvironments = $derived(
		usage
			.map((entry) => ({ environment: entry.environment, bytes: totalReclaimableBytes(entry.data) }))
			.filter((entry) => entry.bytes > 0)
			.sort((a, b) => b.bytes - a.bytes)
			.slice(0, ENVIRONMENT_LIMIT)
	);

	function formatBytes(value: number) {
		return bytes.format(value, { unitSeparator: ' ' }) ?? '-';
	}
</script>

<DashboardPanel title={m.reclaimable_space()}>
	{#snippet actions()}
		{#if onPrune}
			<ArcaneButton action="prune" size="sm" customLabel={m.prune()} onclick={onPrune} />
		{/if}
	{/snippet}
	{#if loading && usage.length === 0}
		<div class="space-y-3 py-2">
			<Skeleton class="h-7 w-24" />
			<Skeleton class="h-2 w-full" />
		</div>
	{:else if total === 0}
		<p class="py-2 text-sm text-muted-foreground">{failed ? m.common_unavailable() : m.nothing_to_reclaim()}</p>
	{:else}
		<div class="space-y-4">
			<p class="text-2xl font-semibold tabular-nums">{formatBytes(total)}</p>
			<ul class="space-y-3">
				{#each resources as resource (resource.key)}
					<li class="space-y-1.5">
						<div class="flex items-center justify-between gap-3 text-sm">
							<span class="truncate text-muted-foreground">{resource.label}</span>
							<span class="font-medium whitespace-nowrap tabular-nums">{formatBytes(resource.total)}</span>
						</div>
						<div class="flex h-2 gap-0.5">
							{#each resource.segments as segment (segment.environment.id)}
								<div
									class="h-full grow-(--segment-grow) rounded-full bg-(--segment-color)"
									style:--segment-grow={segment.bytes}
									style:--segment-color={environmentColor(segment.environment.id) ?? 'var(--color-primary)'}
									title={`${segment.environment.name}: ${formatBytes(segment.bytes)}`}
								></div>
							{/each}
						</div>
					</li>
				{/each}
			</ul>
			{#if usage.length > 1}
				<ul class="divide-y divide-border/40">
					{#each topEnvironments as entry (entry.environment.id)}
						<li class="flex items-center justify-between gap-3 py-2.5 text-sm">
							<span class="flex min-w-0 items-center gap-2">
								<span
									class="size-2 shrink-0 rounded-full bg-(--segment-color)"
									style:--segment-color={environmentColor(entry.environment.id) ?? 'var(--color-primary)'}
									aria-hidden="true"
								></span>
								<span class="truncate font-medium">{entry.environment.name}</span>
							</span>
							<span class="whitespace-nowrap text-muted-foreground tabular-nums">{formatBytes(entry.bytes)}</span>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/if}
</DashboardPanel>

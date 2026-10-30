<script lang="ts">
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import ShareBar from '#lib/components/vulnerability/share-bar.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { SystemDiskUsage } from '#lib/types/shared.js';
	import { bytes } from '#lib/utils/formatting.js';
	import type { ShareBarSegment } from '#lib/utils/vulnerability.js';

	import { totalReclaimableBytes } from '../overview.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	const ENVIRONMENT_LIMIT = 3;

	let {
		usage,
		loading,
		failed
	}: {
		usage: { environment: EnvironmentRef; data: SystemDiskUsage }[];
		loading: boolean;
		failed: boolean;
	} = $props();

	const segments = $derived<ShareBarSegment[]>([
		{
			key: 'images',
			label: m.prune_unused_images(),
			count: usage.reduce((sum, entry) => sum + entry.data.unusedImagesBytes, 0),
			barClass: 'bg-primary'
		},
		{
			key: 'buildCache',
			label: m.build_cache(),
			count: usage.reduce((sum, entry) => sum + entry.data.buildCacheBytes, 0),
			barClass: 'bg-primary/60'
		},
		{
			key: 'volumes',
			label: m.unused_volumes(),
			count: usage.reduce((sum, entry) => sum + entry.data.unusedVolumesBytes, 0),
			barClass: 'bg-primary/35'
		},
		{
			key: 'containers',
			label: m.prune_stopped_containers(),
			count: usage.reduce((sum, entry) => sum + entry.data.stoppedContainersBytes, 0),
			barClass: 'bg-muted-foreground/40'
		}
	]);
	const total = $derived(segments.reduce((sum, segment) => sum + segment.count, 0));
	const topEnvironments = $derived(
		usage
			.map((entry) => ({ environment: entry.environment, bytes: totalReclaimableBytes(entry.data) }))
			.filter((entry) => entry.bytes > 0)
			.sort((a, b) => b.bytes - a.bytes)
			.slice(0, ENVIRONMENT_LIMIT)
	);
</script>

<DashboardPanel title={m.reclaimable_space()}>
	{#if loading && usage.length === 0}
		<div class="space-y-3 py-2">
			<Skeleton class="h-7 w-24" />
			<Skeleton class="h-2 w-full" />
		</div>
	{:else if total === 0}
		<p class="py-2 text-sm text-muted-foreground">{failed ? m.common_unavailable() : m.nothing_to_reclaim()}</p>
	{:else}
		<p class="text-2xl font-semibold tabular-nums">{bytes.format(total, { unitSeparator: ' ' }) ?? '-'}</p>
		<ShareBar {segments} formatCount={(value) => bytes.format(value, { unitSeparator: ' ' }) ?? '-'} />
		{#if usage.length > 1}
			<ul class="divide-y divide-border/40">
				{#each topEnvironments as entry (entry.environment.id)}
					<li class="flex items-center justify-between gap-3 py-2.5 text-sm">
						<span class="truncate font-medium">{entry.environment.name}</span>
						<span class="whitespace-nowrap text-muted-foreground tabular-nums">
							{bytes.format(entry.bytes, { unitSeparator: ' ' }) ?? '-'}
						</span>
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</DashboardPanel>

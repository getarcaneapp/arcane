<script lang="ts">
	import { resolve } from '$app/paths';

	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerSummaryDto } from '#lib/types/docker.js';
	import type { EnvironmentRow } from '#lib/utils/environments.js';
	import { bytes } from '#lib/utils/formatting.js';

	import { getContainerDisplayName } from '../../containers/container-table.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	const CONTAINER_LIMIT = 5;

	let {
		containers,
		loading,
		failed
	}: {
		containers: EnvironmentRow<ContainerSummaryDto>[];
		loading: boolean;
		failed: boolean;
	} = $props();

	const topContainers = $derived(
		containers
			.filter((container) => (container.resourceSample?.memoryUsageBytes ?? 0) > 0)
			.sort((a, b) => (b.resourceSample?.memoryUsageBytes ?? 0) - (a.resourceSample?.memoryUsageBytes ?? 0))
			.slice(0, CONTAINER_LIMIT)
	);
</script>

<DashboardPanel title={m.top_memory()}>
	{#if loading && containers.length === 0}
		<div class="space-y-3 py-2">
			<Skeleton class="h-4 w-3/4" />
			<Skeleton class="h-4 w-1/2" />
		</div>
	{:else if topContainers.length === 0}
		<p class="py-2 text-sm text-muted-foreground">{failed ? m.common_unavailable() : m.no_running_containers()}</p>
	{:else}
		<ul class="divide-y divide-border/40">
			{#each topContainers as container (`${container.environmentId}:${container.id}`)}
				<li>
					<a
						href={resolve(`/(app)/containers/[containerId]?env=${encodeURIComponent(container.environmentId)}`, {
							containerId: container.id
						})}
						class="-mx-2 grid grid-cols-content-action items-center gap-3 rounded-md px-2 py-2.5 transition-colors hover:bg-muted/40"
					>
						<span class="min-w-0 space-y-1">
							<span class="block truncate text-sm font-medium">{getContainerDisplayName(container)}</span>
							<EnvironmentCell value={container.environmentId} />
						</span>
						<span class="text-sm whitespace-nowrap text-muted-foreground tabular-nums">
							{bytes.format(container.resourceSample?.memoryUsageBytes ?? 0, { unitSeparator: ' ' }) ?? '-'}
						</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</DashboardPanel>

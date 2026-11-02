<script lang="ts">
	import type { Snippet } from 'svelte';

	import type { Environment } from '#lib/types/environment.js';
	import type { DashboardWidget } from '#lib/utils/dashboard-layout.js';

	import ContainerLogsWidget from './container-logs-widget.svelte';
	import ContainerStatsWidget from './container-stats-widget.svelte';
	import EnvironmentWidget from './environment-widget.svelte';
	import ResourceQueryWidget from './resource-query-widget.svelte';
	import ResourceWidget from './resource-widget.svelte';

	let { widget, environmentCard }: { widget: DashboardWidget; environmentCard: Snippet<[Environment]> } = $props();
</script>

{#if widget.type === 'resourceQuery'}
	<ResourceQueryWidget {widget} />
{:else if widget.type === 'resource'}
	<ResourceWidget {widget} />
{:else if widget.type === 'containerStats'}
	<ContainerStatsWidget {widget} />
{:else if widget.type === 'containerLogs'}
	<ContainerLogsWidget {widget} />
{:else if widget.type === 'environment'}
	<EnvironmentWidget {widget} {environmentCard} />
{/if}

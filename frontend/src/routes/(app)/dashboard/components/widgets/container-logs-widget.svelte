<script lang="ts">
	import { resolve } from '$app/paths';
	import { createQuery } from '@tanstack/svelte-query';

	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { containerDetailQuery } from '#lib/query/containers.js';
	import type { DashboardWidget } from '#lib/utils/dashboard-layout.js';
	import { startLogViewerStream } from '#lib/utils/log-viewer.js';

	import DashboardPanel from '../dashboard-panel.svelte';
	import WidgetStatus from './widget-status.svelte';
	import { resolveWidgetTarget } from './widget-target';

	let { widget }: { widget: DashboardWidget } = $props();

	let viewer = $state<ReturnType<typeof LogViewer>>();
	let autoScroll = $state(true);
	let startedViewer: ReturnType<typeof LogViewer> | undefined;

	const config = $derived(widget.config ?? {});
	const environmentId = $derived(config.environmentIds?.[0] ?? '');
	const resourceId = $derived(config.resourceId ?? '');
	const target = $derived(resolveWidgetTarget(config, ['containers:read', 'containers:logs']));

	const containerQuery = createQuery(() => ({
		...containerDetailQuery(environmentId, resourceId),
		enabled: target.status === 'ready'
	}));

	$effect(() => {
		if (viewer && viewer !== startedViewer) {
			startedViewer = viewer;
			startLogViewerStream(viewer);
		}
	});
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

<DashboardPanel title={widget.title || m.containers_logs_title()} actions={containerLink}>
	<WidgetStatus {target} data={containerQuery.data} error={containerQuery.error} resource={m.container()}>
		{#snippet children(container)}
			<div class="h-full min-h-48 overflow-hidden rounded-lg border border-border/70 bg-card/10">
				<LogViewer
					bind:this={viewer}
					bind:autoScroll
					type="container"
					containerId={container.id}
					{environmentId}
					tailLines={100}
					maxLines={200}
					groupAdjacentLines={true}
					height="100%"
				/>
			</div>
		{/snippet}
	</WidgetStatus>
</DashboardPanel>

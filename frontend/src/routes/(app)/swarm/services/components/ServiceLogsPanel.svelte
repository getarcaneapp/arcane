<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import LogViewer from '$lib/components/logs/log-viewer.svelte';
	import LogControls from '$lib/components/logs/log-controls.svelte';
	import LogPanelTitle from '$lib/components/logs/log-panel-title.svelte';
	import { m } from '$lib/paraglide/messages';
	import { refreshLogViewerStream, startLogViewerStream, stopLogViewerStream } from '$lib/utils/log-viewer';
	import { FileTextIcon } from '$lib/icons';

	let {
		serviceId
	}: {
		serviceId: string | undefined;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<ReturnType<typeof LogViewer>>();
	let autoScroll = $state(true);
	let autoStartLogs = $state(false);
	let showParsedJson = $state(false);

	function handleStart() {
		startLogViewerStream(viewer);
	}

	function handleStop() {
		stopLogViewerStream(viewer);
	}

	async function handleRefresh() {
		await refreshLogViewerStream(viewer);
	}

	function handleStreamStart() {
		isStreaming = true;
	}

	function handleStreamStop() {
		isStreaming = false;
	}

	$effect(() => {
		if (autoStartLogs && !isStreaming && serviceId && viewer) {
			viewer.startLogStream();
		}
	});
</script>

<Card.Root>
	<Card.Header icon={FileTextIcon}>
		<div class="flex flex-1 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex flex-col gap-1.5">
				<LogPanelTitle title={m.swarm_service_logs_title()} live={isStreaming} />
			</div>
			<LogControls
				bind:autoScroll
				bind:autoStartLogs
				bind:showParsedJson
				{isStreaming}
				disabled={!serviceId}
				onStart={handleStart}
				onStop={handleStop}
				onRefresh={handleRefresh}
			/>
		</div>
	</Card.Header>
	<Card.Content class="p-0">
		<div class="bg-card/90 rounded-lg border p-0 backdrop-blur-sm">
			<LogViewer
				bind:this={viewer}
				bind:autoScroll
				type="service"
				{serviceId}
				{showParsedJson}
				maxLines={500}
				showTimestamps={true}
				height="calc(100vh - 320px)"
				onStart={handleStreamStart}
				onStop={handleStreamStop}
			/>
		</div>
	</Card.Content>
</Card.Root>

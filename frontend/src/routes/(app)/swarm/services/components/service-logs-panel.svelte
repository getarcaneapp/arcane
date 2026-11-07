<script lang="ts">
	import { untrack } from 'svelte';

	import LogControls from '#lib/components/logs/log-controls.svelte';
	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	import { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import { refreshLogViewerStream, startLogViewerStream, stopLogViewerStream } from '#lib/utils/log-viewer.js';

	let {
		environmentId,
		serviceId
	}: {
		environmentId: string;
		serviceId: string | undefined;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<ReturnType<typeof LogViewer>>();
	let autoScroll = $state(true);
	const preferences = new UseLogPreferences();
	let logSearchTerm = $state('');
	let hasAutoStarted = false;

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
		if (preferences.autoStartLogs && !hasAutoStarted && !isStreaming && serviceId && viewer) {
			hasAutoStarted = true;
			untrack(handleStart);
		}
	});
</script>

<div class="flex flex-col overflow-hidden rounded-xl bg-table-head">
	<div class="flex h-11 shrink-0 items-center gap-2 pr-1.5 pl-3">
		<h2 class="text-sm font-medium">{m.common_logs()}</h2>
		<div class="ml-auto min-w-0">
			<LogControls
				bind:searchTerm={logSearchTerm}
				bind:autoScroll
				{preferences}
				{isStreaming}
				disabled={!serviceId}
				onStart={handleStart}
				onStop={handleStop}
				onRefresh={handleRefresh}
			/>
		</div>
	</div>
	<LogViewer
		searchTerm={logSearchTerm}
		bind:this={viewer}
		bind:autoScroll
		type="service"
		{environmentId}
		{serviceId}
		bind:showParsedJson={preferences.showParsedJson}
		showStreamLabels={preferences.showStreamLabels}
		tailLines={preferences.tailLines}
		maxLines={500}
		showTimestamps={preferences.showTimestamps}
		height="calc(100vh - 320px)"
		onStart={handleStreamStart}
		onStop={handleStreamStop}
	/>
</div>

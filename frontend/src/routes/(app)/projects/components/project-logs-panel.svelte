<script lang="ts">
	import { onMount } from 'svelte';

	import LogControls from '#lib/components/logs/log-controls.svelte';
	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	import { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';
	import { m } from '#lib/paraglide/messages.js';

	let {
		projectId,
		environmentId,
		autoScroll = $bindable()
	}: {
		projectId: string;
		environmentId: string;
		autoScroll: boolean;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<ReturnType<typeof LogViewer>>();
	const preferences = new UseLogPreferences();
	let logSearchTerm = $state('');

	function handleStart() {
		viewer?.startLogStream();
	}

	function handleStop() {
		viewer?.stopLogStream();
	}

	async function handleRefresh() {
		await viewer?.clearLogs({ hard: true, restart: true });
	}

	onMount(() => {
		if (preferences.autoStartLogs && projectId) handleStart();
	});
</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden rounded-xl bg-table-head">
	<div class="flex h-11 shrink-0 items-center gap-2 pr-1.5 pl-3">
		<h2 class="text-sm font-medium">{m.common_logs()}</h2>
		<div class="ml-auto min-w-0">
			<LogControls
				bind:searchTerm={logSearchTerm}
				bind:autoScroll
				{preferences}
				serviceLabels
				{isStreaming}
				disabled={!projectId}
				onStart={handleStart}
				onStop={handleStop}
				onRefresh={handleRefresh}
			/>
		</div>
	</div>
	<LogViewer
		class="min-h-0 flex-1"
		searchTerm={logSearchTerm}
		bind:this={viewer}
		bind:autoScroll
		{projectId}
		{environmentId}
		tailLines={preferences.tailLines}
		bind:showParsedJson={preferences.showParsedJson}
		showStreamLabels={preferences.showStreamLabels}
		type="project"
		maxLines={500}
		showTimestamps={preferences.showTimestamps}
		height="100%"
		onStart={() => (isStreaming = true)}
		onStop={() => (isStreaming = false)}
	/>
</div>

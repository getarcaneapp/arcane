<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import LogViewer from '$lib/components/logs/log-viewer.svelte';
	import LogControls from '$lib/components/logs/log-controls.svelte';
	import { m } from '$lib/paraglide/messages';
	import { FileTextIcon } from '$lib/icons';

	let {
		serviceId
	}: {
		serviceId: string | undefined;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<LogViewer>();
	let autoScroll = $state(true);
	let autoStartLogs = $state(false);
	let hasAutoStarted = $state(false);
	let showParsedJson = $state(false);

	function handleStart() {
		viewer?.startLogStream();
	}

	function handleStop() {
		viewer?.stopLogStream();
	}

	function handleClear() {
		viewer?.clearLogs();
	}

	async function handleRefresh() {
		await viewer?.clearLogs({ hard: true, restart: true });
	}

	function handleStreamStart() {
		isStreaming = true;
	}

	function handleStreamStop() {
		isStreaming = false;
	}

	$effect(() => {
		if (autoStartLogs && !hasAutoStarted && !isStreaming && serviceId && viewer) {
			hasAutoStarted = true;
			viewer.startLogStream();
		}
	});
</script>

<Card.Root>
	<Card.Header icon={FileTextIcon}>
		<div class="flex flex-1 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex flex-col gap-1.5">
				<div class="flex items-center gap-2">
					<Card.Title>
						<h2>{m.swarm_service_logs_title()}</h2>
					</Card.Title>
					{#if isStreaming}
						<div class="flex items-center gap-2">
							<div class="size-2 animate-pulse rounded-full bg-green-500"></div>
							<span class="text-xs font-semibold text-green-600 sm:text-sm">{m.common_live()}</span>
						</div>
					{/if}
				</div>
			</div>
			<LogControls
				bind:autoScroll
				bind:autoStartLogs
				bind:showParsedJson
				{isStreaming}
				disabled={!serviceId}
				onStart={handleStart}
				onStop={handleStop}
				onClear={handleClear}
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
				onClear={handleClear}
			/>
		</div>
	</Card.Content>
</Card.Root>

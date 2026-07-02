<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import LogViewer from '$lib/components/logs/log-viewer.svelte';
	import LogControls from '$lib/components/logs/log-controls.svelte';
	import LogPanelTitle from '$lib/components/logs/log-panel-title.svelte';
	import { m } from '$lib/paraglide/messages';
	import { TerminalIcon } from '$lib/icons';

	let {
		projectId,
		autoScroll = $bindable()
	}: {
		projectId: string;
		autoScroll: boolean;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<ReturnType<typeof LogViewer>>();
	let tailLines = $state(100);
	let autoStartLogs = $state(false);
	let hasAutoStarted = $state(false);
	let showParsedJson = $state(false);

	function handleStart() {
		isStreaming = true;
		viewer?.startLogStream();
	}

	function handleStop() {
		isStreaming = false;
		viewer?.stopLogStream();
	}

	async function handleRefresh() {
		await viewer?.clearLogs({ hard: true, restart: true });
	}

	$effect(() => {
		if (projectId) {
			hasAutoStarted = false;
		}
	});

	$effect(() => {
		if (autoStartLogs && !hasAutoStarted && !isStreaming && projectId) {
			hasAutoStarted = true;
			handleStart();
		}
	});
</script>

<Card.Root>
	<Card.Header icon={TerminalIcon}>
		<div class="flex flex-1 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex flex-col gap-1.5">
				<div class="flex items-start justify-between gap-3 lg:block">
					<LogPanelTitle title={m.compose_logs_title()} live={isStreaming} />
					<LogControls
						bind:autoScroll
						bind:tailLines
						bind:autoStartLogs
						bind:showParsedJson
						mobileLayout="full"
						showDesktop={false}
						{isStreaming}
						disabled={!projectId}
						onStart={handleStart}
						onStop={handleStop}
						onRefresh={handleRefresh}
					/>
				</div>
				<Card.Description>{m.project_logs_realtime_desc()}</Card.Description>
			</div>
			<LogControls
				bind:autoScroll
				bind:tailLines
				bind:autoStartLogs
				bind:showParsedJson
				mobileLayout="none"
				{isStreaming}
				disabled={!projectId}
				onStart={handleStart}
				onStop={handleStop}
				onRefresh={handleRefresh}
			/>
		</div>
	</Card.Header>
	<Card.Content class="p-0">
		<LogViewer
			bind:this={viewer}
			bind:autoScroll
			{projectId}
			{tailLines}
			{showParsedJson}
			type="project"
			maxLines={500}
			showTimestamps={true}
			height="calc(100vh - 320px)"
			onStart={handleStart}
			onStop={handleStop}
		/>
	</Card.Content>
</Card.Root>

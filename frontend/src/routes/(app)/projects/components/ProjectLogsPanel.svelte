<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	import LogControls from '#lib/components/logs/log-controls.svelte';
	import LogPanelTitle from '#lib/components/logs/log-panel-title.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { TerminalIcon } from '#lib/icons/index.js';

	let {
		projectId,
		autoScroll = $bindable(),
		isRunning = true
	}: {
		projectId: string;
		autoScroll: boolean;
		isRunning?: boolean;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<ReturnType<typeof LogViewer>>();
	let tailLines = $state(100);
	let autoStartLogs = $state(false);
	let logSearchTerm = $state('');
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

	// The panel stays visible while the project is stopped; the stream pauses and
	// picks back up (via auto-start) once the project is running again.
	$effect(() => {
		if (!isRunning) {
			hasAutoStarted = false;
			if (isStreaming) handleStop();
		}
	});

	$effect(() => {
		if (autoStartLogs && !hasAutoStarted && !isStreaming && projectId && isRunning) {
			hasAutoStarted = true;
			handleStart();
		}
	});
</script>

<Card.Root class="flex h-full min-h-0 flex-col">
	<Card.Header icon={TerminalIcon}>
		<div class="flex flex-1 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex flex-col gap-1.5">
				<div class="flex items-start justify-between gap-3 lg:block">
					<LogPanelTitle title={m.compose_logs_title()} live={isStreaming} />
					<LogControls
						bind:searchTerm={logSearchTerm}
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
				bind:searchTerm={logSearchTerm}
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
	<Card.Content class="flex min-h-0 flex-1 flex-col p-0">
		<LogViewer
			class="min-h-0 flex-1"
			searchTerm={logSearchTerm}
			bind:this={viewer}
			bind:autoScroll
			{projectId}
			{tailLines}
			bind:showParsedJson
			type="project"
			maxLines={500}
			showTimestamps={true}
			height="100%"
			onStart={handleStart}
			onStop={handleStop}
		/>
	</Card.Content>
</Card.Root>

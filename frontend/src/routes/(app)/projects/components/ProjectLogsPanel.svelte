<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import LogViewer from '#lib/components/logs/log-viewer.svelte';
	import LogControls from '#lib/components/logs/log-controls.svelte';
	import { UseLogPreferences } from '#lib/hooks/use-log-preferences.svelte.js';
	import LogPanelTitle from '#lib/components/logs/log-panel-title.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { TerminalIcon } from '#lib/icons/index.js';

	let {
		projectId,
		autoScroll = $bindable()
	}: {
		projectId: string;
		autoScroll: boolean;
	} = $props();

	let isStreaming = $state(false);
	let viewer = $state<ReturnType<typeof LogViewer>>();
	const preferences = new UseLogPreferences();
	let logSearchTerm = $state('');
	// Plain guard, not $state: it is only written from the auto-start effect and never rendered.
	let hasAutoStarted = false;

	function handleStart() {
		viewer?.startLogStream();
	}

	function handleStop() {
		viewer?.stopLogStream();
	}

	async function handleRefresh() {
		await viewer?.clearLogs({ hard: true, restart: true });
	}

	$effect(() => {
		if (preferences.autoStartLogs && !hasAutoStarted && !isStreaming && projectId && viewer) {
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
						{preferences}
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
				{preferences}
				mobileLayout="none"
				{isStreaming}
				disabled={!projectId}
				onStart={handleStart}
				onStop={handleStop}
				onRefresh={handleRefresh}
			/>
		</div>
	</Card.Header>
	<div class="flex min-h-0 flex-1 flex-col">
		<LogViewer
			class="min-h-0 flex-1"
			searchTerm={logSearchTerm}
			bind:this={viewer}
			bind:autoScroll
			{projectId}
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
</Card.Root>

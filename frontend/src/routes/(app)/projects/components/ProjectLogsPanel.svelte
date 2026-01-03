<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import LogViewer from '$lib/components/logs/log-viewer.svelte';
	import LogControls from '$lib/components/logs/log-controls.svelte';
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
	let viewer = $state<LogViewer>();
	let tailLines = $state(100);
	let autoStartLogs = $state(false);
	let hasAutoStarted = $state(false);

	function handleStart() {
		isStreaming = true;
		viewer?.startLogStream();
	}

	function handleStop() {
		isStreaming = false;
		viewer?.stopLogStream();
	}

	function handleClear() {
		viewer?.clearLogs();
	}

	function handleRefresh() {
		viewer?.stopLogStream();
		viewer?.startLogStream();
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
				<div class="flex items-center gap-2">
					<Card.Title>
						<h2>{m.compose_logs_title()}</h2>
					</Card.Title>
					{#if isStreaming}
						<div class="flex items-center gap-2">
							<div class="size-2 animate-pulse rounded-full bg-green-500"></div>
							<span class="text-xs font-semibold text-green-600 sm:text-sm">{m.common_live()}</span>
						</div>
					{/if}
				</div>
				<Card.Description>Real-time project logs</Card.Description>
			</div>
			<LogControls
				bind:autoScroll
				bind:tailLines
				bind:autoStartLogs
				{isStreaming}
				disabled={!projectId}
				onStart={handleStart}
				onStop={handleStop}
				onClear={handleClear}
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
			type="project"
			maxLines={500}
			showTimestamps={true}
			height="calc(100vh - 320px)"
			onStart={handleStart}
			onStop={handleStop}
			onClear={handleClear}
		/>
	</Card.Content>
</Card.Root>

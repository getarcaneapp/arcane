<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import LogViewer from '$lib/components/logs/log-viewer.svelte';
	import LogControls from '$lib/components/logs/log-controls.svelte';
	import { m } from '$lib/paraglide/messages';

	let {
		open = $bindable(false),
		serviceId,
		serviceName = ''
	}: {
		open: boolean;
		serviceId: string | null;
		serviceName?: string;
	} = $props();

	let autoScroll = $state(true);
	let isStreaming = $state(false);
	let viewer = $state<LogViewer>();
	let autoStartLogs = $state(true);
	let showParsedJson = $state(false);
	let hasAutoStarted = $state(false);

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
		if (open && serviceId && autoStartLogs && !hasAutoStarted) {
			hasAutoStarted = true;
			handleStart();
		}
	});

	$effect(() => {
		if (!open) {
			viewer?.stopLogStream(false);
			hasAutoStarted = false;
		}
	});

	function handleOpenChange(newOpenState: boolean) {
		open = newOpenState;
	}
</script>

<ResponsiveDialog
	bind:open
	onOpenChange={handleOpenChange}
	variant="sheet"
	title="{m.swarm_service_logs_title()} — {serviceName}"
	description=""
	contentClass="sm:max-w-[800px]"
>
	{#snippet children()}
		<div class="flex flex-col gap-4 py-4">
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
			<div class="bg-card/90 rounded-lg border p-0 backdrop-blur-sm">
				<LogViewer
					bind:this={viewer}
					bind:autoScroll
					type="service"
					{serviceId}
					{showParsedJson}
					maxLines={500}
					showTimestamps={true}
					height="calc(70vh - 200px)"
					onStart={handleStreamStart}
					onStop={handleStreamStop}
					onClear={handleClear}
				/>
			</div>
		</div>
	{/snippet}
</ResponsiveDialog>

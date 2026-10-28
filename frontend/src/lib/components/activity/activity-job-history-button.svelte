<script lang="ts">
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import JobRunHistory from '#lib/components/job-card/job-run-history.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Activity } from '#lib/types/activity.type.js';

	let { activity }: { activity: Activity } = $props();
	let historyOpen = $state(false);

	const jobId = $derived(typeof activity.metadata?.['jobId'] === 'string' ? activity.metadata['jobId'] : undefined);
	const runId = $derived(typeof activity.metadata?.['runId'] === 'string' ? activity.metadata['runId'] : undefined);
	const environmentId = $derived(
		typeof activity.metadata?.['environmentId'] === 'string'
			? activity.metadata['environmentId']
			: activity.sourceEnvironmentId || activity.environmentId
	);
</script>

{#if activity.type === 'job_run' && jobId && runId}
	<IfPermitted perm="jobs:manage" envId={environmentId}>
		<Button variant="outline" size="sm" onclick={() => (historyOpen = true)}>
			<ClockIcon aria-hidden="true" />
			{m.jobs_run_history()}
		</Button>
		<Dialog.Root bind:open={historyOpen}>
			<Dialog.Content class="max-h-(--max-height-screen-85) overflow-y-auto sm:max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{m.jobs_run_history()}</Dialog.Title>
					<Dialog.Description>{jobId}</Dialog.Description>
				</Dialog.Header>
				{#if historyOpen}
					<JobRunHistory {jobId} {environmentId} initialRunId={runId} onOpenActivity={() => (historyOpen = false)} />
				{/if}
			</Dialog.Content>
		</Dialog.Root>
	</IfPermitted>
{/if}

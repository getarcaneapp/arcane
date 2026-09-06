<script lang="ts">
	import { StartIcon, EditIcon, ClockIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';
	import { Badge } from '#lib/components/ui/badge';
	import { Button } from '#lib/components/ui/button';
	import { Spinner } from '#lib/components/ui/spinner';
	import { jobScheduleService } from '#lib/services/job-schedule-service';
	import { formatDateTimeShort, formatRelativeTime, parseInstant } from '#lib/utils/formatting';
	import type { Snippet } from 'svelte';
	import type { JobStatus } from '#lib/types/settings';
	import JobScheduleDialog from './job-schedule-dialog.svelte';
	import JobRunHistory from './job-run-history.svelte';
	import { jobStatusLabel, jobNameLabel } from './job-status';
	import { toast } from 'svelte-sonner';
	import { createMutation } from '@tanstack/svelte-query';

	let {
		job,
		environmentId = '0',
		isAgent = false,
		durableRuns = false,
		onScheduleUpdate,
		children,
		enabledOverride,
		headerAccessory,
		collapsibleSettings = false
	}: {
		job: JobStatus;
		environmentId?: string;
		isAgent?: boolean;
		durableRuns?: boolean;
		onScheduleUpdate?: () => void;
		children?: Snippet;
		enabledOverride?: boolean;
		headerAccessory?: Snippet;
		collapsibleSettings?: boolean;
	} = $props();

	let requestId: string | undefined;
	let showScheduleDialog = $state(false);
	const runJobMutation = createMutation(() => ({
		mutationFn: () => {
			requestId ??= crypto.randomUUID();
			return jobScheduleService.runJob(job.id, environmentId, requestId);
		},
		onSuccess: () => {
			requestId = undefined;
			toast.success(m.jobs_run_queued());
			onScheduleUpdate?.();
		}
	}));
	const restartMutation = createMutation(() => ({
		mutationFn: () => jobScheduleService.restartWorker(job.id, environmentId),
		onSuccess: () => {
			toast.success(m.jobs_worker_restarted());
			onScheduleUpdate?.();
		}
	}));
	const run = $derived(job.currentRun ?? job.lastRun);
	const isRunning = $derived(runJobMutation.isPending);

	const nextRunText = $derived.by(() => {
		if (!job.nextRun) return null;
		const nextRun = parseInstant(job.nextRun);
		if (!nextRun) return null;
		const relative = formatRelativeTime(nextRun);
		const absolute = formatDateTimeShort(nextRun);
		return `${relative} (${absolute})`;
	});

	const isEnabled = $derived.by(() => enabledOverride ?? job.enabled);

	const canRun = $derived(durableRuns && isEnabled && job.canRunManually && !isRunning && !(isAgent && job.managerOnly));

	function runJobNow() {
		if (!canRun) return;
		runJobMutation.mutate();
	}

	function openScheduleDialog() {
		showScheduleDialog = true;
	}

	function handleScheduleUpdated() {
		showScheduleDialog = false;
		onScheduleUpdate?.();
	}
</script>

<article class="flex min-w-0 flex-col gap-3 py-5">
	<div class="flex min-w-0 flex-wrap items-start justify-between gap-x-6 gap-y-3">
		<div class="flex min-w-0 flex-[1_1_20rem] flex-col gap-1.5">
			<div class="flex flex-wrap items-center gap-2">
				{#if headerAccessory}<div class="mr-1 flex items-center">{@render headerAccessory()}</div>{/if}
				<h4 class="text-base font-semibold">{jobNameLabel(job)}</h4>
				{#if !isEnabled}
					<Badge variant="secondary" size="sm">{m.common_disabled()}</Badge>
				{:else if run}
					<Badge variant={run.status === 'failed' || run.status === 'needs_attention' ? 'destructive' : 'outline'} size="sm"
						>{jobStatusLabel(run.status)}</Badge
					>
				{:else if job.isContinuous}
					<Badge variant="outline" size="sm">{m.jobs_continuous()}</Badge>
				{/if}
			</div>
			{#if job.id.startsWith('environment-health:')}
				<p class="text-sm leading-relaxed text-muted-foreground">{m.jobs_health_scope_description()}</p>
			{:else if job.description}<p class="text-sm leading-relaxed text-muted-foreground">{job.description}</p>{/if}
			{#if job.lastSuccess}<p class="text-xs text-muted-foreground">
					{m.jobs_last_success()}: {formatDateTimeShort(job.lastSuccess)}
				</p>{/if}

			<div class="mt-1 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
				{#if isEnabled}
					{#if job.schedule && (!job.isContinuous || job.settingsKey)}
						<div class="flex items-center gap-2">
							<ClockIcon class="size-3.5 shrink-0" />
							<span class="font-medium">{m.jobs_schedule()}</span>
							<code class="break-all text-foreground">{job.schedule}</code>
						</div>
						{#if nextRunText}<p>{m.jobs_next_run()}: {nextRunText}</p>{/if}
					{:else if job.isContinuous}<p>{m.jobs_continuous()}</p>{/if}
					{#if run?.nextAttempt}<p>{m.jobs_next_retry()}: {formatDateTimeShort(run.nextAttempt)}</p>{/if}
				{/if}
				{#if job.workerHealth}<p>{m.jobs_worker_health()}: {jobStatusLabel(job.workerHealth.status)}</p>{/if}
			</div>
		</div>
		<div class="flex max-w-full flex-wrap items-center gap-1">
			{#if isEnabled && job.settingsKey}
				<Button
					variant="ghost"
					size="icon"
					onclick={openScheduleDialog}
					disabled={isAgent && job.managerOnly}
					aria-label={m.jobs_edit_schedule()}
					title={m.jobs_edit_schedule()}
				>
					<EditIcon />
				</Button>
			{/if}
			{#if durableRuns && !job.children?.length}<JobRunHistory jobId={job.id} {environmentId} onUpdate={onScheduleUpdate} />{/if}
			{#if isEnabled && job.canRunManually}
				<Button variant="outline" size="sm" onclick={runJobNow} disabled={!canRun}>
					{#if isRunning}<Spinner data-icon="inline-start" />{m.jobs_submitting()}
					{:else}<StartIcon data-icon="inline-start" />{job.children?.length ? m.jobs_run_all() : m.jobs_run_now()}{/if}
				</Button>
			{/if}
			{#if durableRuns && job.isContinuous && isEnabled}
				<Button variant="ghost" size="sm" disabled={restartMutation.isPending} onclick={() => restartMutation.mutate()}
					>{m.jobs_restart_worker()}</Button
				>
			{/if}
		</div>
	</div>

	{#if runJobMutation.error}<p class="text-xs text-destructive">{runJobMutation.error.message}</p>{/if}
	{#if restartMutation.error}<p class="text-xs text-destructive">{restartMutation.error.message}</p>{/if}
	{#if job.lastError}<p class="text-xs break-words text-destructive">{m.jobs_last_error()}: {job.lastError}</p>{/if}
	{#if job.workerHealth?.lastError}<p class="text-xs break-words text-destructive">{job.workerHealth.lastError}</p>{/if}
	{#if job.workerHealth?.nextRetry}<p class="text-xs text-muted-foreground">
			{m.jobs_next_retry()}: {formatDateTimeShort(job.workerHealth.nextRetry)}
		</p>{/if}
	{#if run?.status === 'waiting' && run.remoteOutcome}
		<p class="text-xs text-muted-foreground">
			{m.jobs_remote_status()}: {jobStatusLabel(run.remoteOutcome.status)}
			{#if run.lastConfirmedAt}<span class="ml-2">{m.jobs_last_confirmed()}: {formatDateTimeShort(run.lastConfirmedAt)}</span
				>{/if}
		</p>
	{/if}
	{#if children}
		{#if collapsibleSettings}
			<details class="group">
				<summary
					class="w-fit cursor-pointer rounded-sm text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
					>{m.jobs_settings_details()}</summary
				>
				<div class="mt-4 max-w-3xl">{@render children()}</div>
			</details>
		{:else}
			{@render children()}
		{/if}
	{/if}
</article>

{#if showScheduleDialog}
	<JobScheduleDialog {job} {environmentId} bind:open={showScheduleDialog} onUpdate={handleScheduleUpdated} />
{/if}

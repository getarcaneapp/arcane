<script lang="ts">
	import { StartIcon, EditIcon, ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { formatDateTimeShort, formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';
	import type { Snippet } from 'svelte';
	import type { JobStatus } from '#lib/types/settings.js';
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

	const statusBadge = $derived(
		!isEnabled
			? { variant: 'secondary' as const, label: m.common_disabled() }
			: run
				? {
						variant: ['failed', 'needs_attention'].includes(run.status) ? ('destructive' as const) : ('outline' as const),
						label: jobStatusLabel(run.status)
					}
				: job.isContinuous
					? { variant: 'outline' as const, label: m.jobs_continuous() }
					: null
	);
	const description = $derived(job.id.startsWith('environment-health:') ? m.jobs_health_scope_description() : job.description);
	const showSchedule = $derived(isEnabled && job.schedule && (!job.isContinuous || job.settingsKey));
	const scheduleMetadata = $derived(
		[
			{ id: 'next-run', visible: showSchedule && nextRunText, label: m.jobs_next_run(), value: nextRunText },
			{ id: 'continuous', visible: isEnabled && !showSchedule && job.isContinuous, label: '', value: m.jobs_continuous() },
			{
				id: 'next-attempt',
				visible: isEnabled && run?.nextAttempt,
				label: m.jobs_next_retry(),
				value: formatDateTimeShort(run?.nextAttempt)
			},
			{
				id: 'worker-health',
				visible: job.workerHealth,
				label: m.jobs_worker_health(),
				value: jobStatusLabel(job.workerHealth?.status ?? '')
			}
		].filter((item) => item.visible)
	);
	const errors = $derived(
		[
			{ id: 'run', visible: runJobMutation.error, text: runJobMutation.error?.message, class: 'text-xs text-destructive' },
			{ id: 'restart', visible: restartMutation.error, text: restartMutation.error?.message, class: 'text-xs text-destructive' },
			{
				id: 'last',
				visible: job.lastError,
				text: `${m.jobs_last_error()}: ${job.lastError}`,
				class: 'text-xs break-words text-destructive'
			},
			{
				id: 'worker',
				visible: job.workerHealth?.lastError,
				text: job.workerHealth?.lastError,
				class: 'text-xs break-words text-destructive'
			}
		].filter((error) => error.visible)
	);

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
			<div class="flex min-h-9 flex-wrap items-center gap-2">
				{@render headerAccessory?.()}
				<h4 class="text-base font-semibold">{jobNameLabel(job)}</h4>
				{#if statusBadge}<Badge variant={statusBadge.variant} size="sm">{statusBadge.label}</Badge>{/if}
			</div>
			{#if description}<p class="text-sm leading-relaxed text-muted-foreground">{description}</p>{/if}
			{#if job.lastSuccess}<p class="text-xs text-muted-foreground">
					{m.jobs_last_success()}: {formatDateTimeShort(job.lastSuccess)}
				</p>{/if}

			<div class="mt-1 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
				{#if showSchedule}
					<div class="flex items-center gap-2">
						<ClockIcon class="size-3.5 shrink-0" />
						<span class="font-medium">{m.jobs_schedule()}</span>
						<code class="break-all text-foreground">{job.schedule}</code>
					</div>
				{/if}
				{#each scheduleMetadata as item (item.id)}
					<p>{item.label ? `${item.label}: ` : ''}{item.value}</p>
				{/each}
			</div>
		</div>
		<div class="flex min-h-9 max-w-full flex-wrap items-center gap-1">
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

	{#each errors as error (error.id)}<p class={error.class}>{error.text}</p>{/each}
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
	{#if children && collapsibleSettings}
		<details class="group">
			<summary
				class="w-fit cursor-pointer rounded-sm text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
				>{m.jobs_settings_details()}</summary
			>
			<div class="mt-4 max-w-3xl">{@render children()}</div>
		</details>
	{:else}
		{@render children?.()}
	{/if}
</article>

{#if showScheduleDialog}
	<JobScheduleDialog {job} {environmentId} bind:open={showScheduleDialog} onUpdate={handleScheduleUpdated} />
{/if}

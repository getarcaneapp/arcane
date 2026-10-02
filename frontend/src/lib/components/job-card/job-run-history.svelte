<script lang="ts">
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createQuery, createMutation } from '@tanstack/svelte-query';
	import { m } from '#lib/paraglide/messages.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import { cn } from '#lib/utils.js';
	import { jobStatusLabel, jobStatusTone } from './job-status';

	// Renders the run list and selected-run detail; the host decides where it lives (panel or dialog).
	let {
		jobId,
		environmentId,
		initialRunId,
		onUpdate
	}: { jobId: string; environmentId: string; initialRunId?: string; onUpdate?: () => void } = $props();
	let page = $state(1);
	// Hosts re-key this component per job, so the initial run only seeds state once.
	// svelte-ignore state_referenced_locally
	let selected = $state<string | undefined>(initialRunId);
	const runs = createQuery(() => ({
		queryKey: queryKeys.jobs.runs(environmentId, jobId, page),
		queryFn: () => jobScheduleService.listRuns(jobId, environmentId, page),
		refetchInterval: 5000
	}));
	const detail = createQuery(() => ({
		queryKey: queryKeys.jobs.run(environmentId, jobId, selected),
		queryFn: () => jobScheduleService.getRun(jobId, selected!, environmentId),
		enabled: !!selected,
		refetchInterval: 5000
	}));
	const action = createMutation(() => ({
		mutationFn: ({ runId, action }: { runId: string; action: 'retry' | 'cancel' }) =>
			jobScheduleService.updateRun(jobId, runId, action, environmentId),
		onSuccess: () => {
			void runs.refetch();
			void detail.refetch();
			void activityStore.refresh();
			onUpdate?.();
		}
	}));
	const selectedRun = $derived(selected ? detail.data : undefined);
	const hasPrev = $derived(page > 1);
	const hasNext = $derived(!!runs.data && page * runs.data.limit < runs.data.total);
	const metadata = $derived(
		[
			{
				id: 'remote',
				visible: selectedRun?.status === 'waiting' && selectedRun.remoteOutcome,
				label: m.jobs_remote_status(),
				value: jobStatusLabel(selectedRun?.remoteOutcome?.status ?? '')
			},
			{
				id: 'message',
				visible: selectedRun?.outcome.message,
				label: '',
				value: selectedRun?.outcome.message,
				class: 'break-words whitespace-pre-wrap'
			},
			{
				id: 'confirmed',
				visible: selectedRun?.lastConfirmedAt,
				label: m.jobs_last_confirmed(),
				value: formatDateTimeShort(selectedRun?.lastConfirmedAt)
			},
			{
				id: 'retry',
				visible: selectedRun?.nextAttempt,
				label: m.jobs_next_retry(),
				value: formatDateTimeShort(selectedRun?.nextAttempt)
			}
		].filter((item) => item.visible)
	);
	const availableActions = $derived(
		[
			{
				id: 'retry' as const,
				visible: ['failed', 'partial', 'needs_attention'].includes(selectedRun?.status ?? ''),
				variant: 'default' as const,
				label: m.jobs_retry_run()
			},
			{
				id: 'cancel' as const,
				visible: selectedRun?.status === 'queued' && selectedRun.attemptCount === 0 && !selectedRun.remoteDeliveryAttempted,
				variant: 'outline' as const,
				label: m.jobs_cancel_run()
			}
		].filter((item) => item.visible)
	);

	function changePage(delta: number) {
		page += delta;
		selected = undefined;
	}
</script>

{#snippet outcomeMessage(message: string | undefined)}
	{#if message}<p class="break-words whitespace-pre-wrap text-muted-foreground">{message}</p>{/if}
{/snippet}

{#snippet outcomeActivity(
	activityId: string | undefined,
	label = m.jobs_activity_output(),
	activityEnvironmentId = environmentId
)}
	{#if activityId}<Button
			variant="link"
			size="inline"
			onclick={() => activityStore.openCenter(activityId, undefined, activityEnvironmentId)}>{label}</Button
		>{/if}
{/snippet}

<div class="flex flex-col gap-4">
	{#if runs.error}
		<p class="text-sm text-destructive">{extractApiErrorMessage(runs.error)}</p>
	{/if}
	<div class="divide-y divide-border/50 overflow-hidden rounded-lg border border-border/60">
		{#each runs.data?.runs ?? [] as run (run.id)}
			<button
				type="button"
				class={cn(
					'flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm transition-colors hover:bg-muted/40',
					selected === run.id && 'bg-muted/50'
				)}
				aria-pressed={selected === run.id}
				onclick={() => (selected = run.id)}
			>
				<span class="flex min-w-0 flex-col">
					<span class="truncate tabular-nums">{formatDateTimeShort(run.createdAt)}</span>
					<span class="truncate text-xs text-muted-foreground">{m.volume_backup_trigger()}: {run.trigger}</span>
				</span>
				<Badge variant={jobStatusTone(run.status)} size="sm">{jobStatusLabel(run.status)}</Badge>
			</button>
		{:else}
			<p class="px-3 py-6 text-center text-sm text-muted-foreground">
				{#if runs.isPending}<Spinner class="mx-auto size-4" />{:else}{m.jobs_run_empty()}{/if}
			</p>
		{/each}
	</div>
	{#if hasPrev || hasNext}
		<div class="flex justify-between gap-2">
			<Button variant="outline" size="sm" disabled={!hasPrev || runs.isFetching} onclick={() => changePage(-1)}
				>{m.jobs_run_previous()}</Button
			>
			<Button variant="outline" size="sm" disabled={!hasNext || runs.isFetching} onclick={() => changePage(1)}
				>{m.jobs_run_next()}</Button
			>
		</div>
	{/if}
	{#if detail.error}<p class="text-sm text-destructive">{extractApiErrorMessage(detail.error)}</p>{/if}
	{#if selectedRun}
		{@const run = selectedRun}
		<div class="space-y-3 border-t border-border/50 pt-4 text-sm">
			<div class="flex flex-wrap items-center gap-2">
				<Badge variant={jobStatusTone(run.status)}>{jobStatusLabel(run.status)}</Badge>
				<span class="font-mono text-xs break-all text-muted-foreground">{run.id}</span>
			</div>
			{#each metadata as item (item.id)}<p class={item.class}>{item.label ? `${item.label}: ` : ''}{item.value}</p>{/each}
			<div class="flex flex-wrap gap-4">
				{@render outcomeActivity(run.activityId, m.jobs_activity_summary(), run.activityEnvironmentId ?? environmentId)}
				{@render outcomeActivity(run.outcome.activityId)}
			</div>
			{#if run.resolution && run.resolution.resolvedBy !== 'System'}
				<p>
					{m.jobs_resolution_details({ user: run.resolution.resolvedBy, date: formatDateTimeShort(run.resolution.resolvedAt) })}
				</p>
				<p>{m.jobs_resolution_reason()}: {run.resolution.reason}</p>
			{/if}
			<h4 class="font-medium">{m.jobs_run_attempts()}: {run.attemptCount}</h4>
			{#each run.attempts ?? [] as attempt (attempt.number)}
				<div class="space-y-1 rounded-lg border border-border/60 p-2">
					<p>{attempt.number} · {formatDateTimeShort(attempt.startedAt)} · {jobStatusLabel(attempt.outcome.status)}</p>
					{@render outcomeMessage(attempt.outcome.message)}
				</div>
			{/each}
			{#if run.outcome.targets?.length}
				<h4 class="font-medium">{m.jobs_run_targets()}</h4>
				{#each run.outcome.targets as target (target.id)}
					<div class="space-y-1 rounded-lg border border-border/60 p-2">
						<p class="break-all">{target.id} · {jobStatusLabel(target.status)}</p>
						{@render outcomeMessage(target.message)}
						{@render outcomeActivity(target.activityId)}
					</div>
				{/each}
			{/if}
			{#if action.error}<p class="text-destructive">{extractApiErrorMessage(action.error)}</p>{/if}
			<IfPermitted perm="jobs:manage" envId={environmentId}>
				<div class="flex flex-wrap gap-2">
					{#each availableActions as item (item.id)}
						<Button
							variant={item.variant}
							size="sm"
							disabled={action.isPending}
							onclick={() => action.mutate({ runId: run.id, action: item.id })}>{item.label}</Button
						>
					{/each}
				</div>
			</IfPermitted>
		</div>
	{/if}
</div>

<script lang="ts">
	import { queryKeys } from '#lib/query/query-keys';
	import { createQuery, createMutation } from '@tanstack/svelte-query';
	import { m } from '#lib/paraglide/messages';
	import { Button } from '#lib/components/ui/button';
	import * as Dialog from '#lib/components/ui/dialog';
	import { jobScheduleService } from '#lib/services/job-schedule-service';
	import { activityStore } from '#lib/stores/activity.store.svelte';
	import { formatDateTimeShort } from '#lib/utils/formatting';
	import { jobStatusLabel } from './job-status';
	let { jobId, environmentId, onUpdate }: { jobId: string; environmentId: string; onUpdate?: () => void } = $props();
	let open = $state(false);
	let page = $state(1);
	let selected = $state<string | undefined>();
	const runs = createQuery(() => ({
		queryKey: queryKeys.jobs.runs(environmentId, jobId, page),
		queryFn: () => jobScheduleService.listRuns(jobId, environmentId, page),
		enabled: open,
		refetchInterval: 5000
	}));
	const detail = createQuery(() => ({
		queryKey: queryKeys.jobs.run(environmentId, jobId, selected),
		queryFn: () => jobScheduleService.getRun(jobId, selected!, environmentId),
		enabled: open && !!selected,
		refetchInterval: 5000
	}));
	const action = createMutation(() => ({
		mutationFn: ({ runId, action }: { runId: string; action: 'retry' | 'cancel' }) =>
			jobScheduleService.updateRun(jobId, runId, action, environmentId),
		onSuccess: () => {
			void runs.refetch();
			void detail.refetch();
			onUpdate?.();
		}
	}));
</script>

<Button
	variant="ghost"
	size="sm"
	onclick={() => {
		open = true;
		page = 1;
		selected = undefined;
	}}>{m.jobs_run_history()}</Button
>
<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
		<Dialog.Header
			><Dialog.Title>{m.jobs_run_history()}</Dialog.Title><Dialog.Description>{jobId}</Dialog.Description></Dialog.Header
		>
		{#if runs.error}<p class="text-sm text-destructive">{runs.error.message}</p>{/if}
		{#each runs.data?.runs ?? [] as run (run.id)}
			<Button
				variant="outline"
				class="h-auto w-full justify-between gap-2 text-left"
				onclick={() => {
					selected = run.id;
				}}
			>
				<span class="truncate">{formatDateTimeShort(run.createdAt)}</span><span>{jobStatusLabel(run.status)}</span>
			</Button>
		{:else}
			<p class="text-sm text-muted-foreground">{runs.isPending ? m.common_loading() : m.jobs_run_empty()}</p>
		{/each}
		<div class="flex justify-between gap-2">
			<Button
				variant="outline"
				size="sm"
				disabled={page <= 1 || runs.isFetching}
				onclick={() => {
					page--;
					selected = undefined;
				}}>{m.jobs_run_previous()}</Button
			>
			<Button
				variant="outline"
				size="sm"
				disabled={!runs.data || page * runs.data.limit >= runs.data.total || runs.isFetching}
				onclick={() => {
					page++;
					selected = undefined;
				}}>{m.jobs_run_next()}</Button
			>
		</div>
		{#if detail.error}<p class="text-sm text-destructive">{detail.error.message}</p>{/if}
		{#if selected && detail.data}
			{@const run = detail.data}
			<div class="space-y-3 border-t pt-4 text-sm">
				<p class="break-all"><strong>{m.jobs_run_id()}:</strong> {run.id}</p>
				<p>{jobStatusLabel(run.status)}</p>
				{#if run.status === 'waiting' && run.remoteOutcome}<p>
						{m.jobs_remote_status()}: {jobStatusLabel(run.remoteOutcome.status)}
					</p>{/if}
				{#if run.outcome.message}<p class="break-words whitespace-pre-wrap">{run.outcome.message}</p>{/if}
				{#if run.lastConfirmedAt}<p>{m.jobs_last_confirmed()}: {formatDateTimeShort(run.lastConfirmedAt)}</p>{/if}
				{#if run.nextAttempt}<p>{m.jobs_next_retry()}: {formatDateTimeShort(run.nextAttempt)}</p>{/if}
				{#if run.outcome.activityId}<Button variant="link" onclick={() => activityStore.openCenter(run.outcome.activityId)}
						>{m.activity()}</Button
					>{/if}
				<h4 class="font-medium">{m.jobs_run_attempts()}: {run.attemptCount}</h4>
				{#each run.attempts ?? [] as attempt (attempt.number)}
					<div class="space-y-1 rounded border p-2">
						<p>{attempt.number} · {formatDateTimeShort(attempt.startedAt)} · {jobStatusLabel(attempt.outcome.status)}</p>
						{#if attempt.outcome.message}<p class="break-words whitespace-pre-wrap">{attempt.outcome.message}</p>{/if}
					</div>
				{/each}
				{#if run.outcome.targets?.length}
					<h4 class="font-medium">{m.jobs_run_targets()}</h4>
					{#each run.outcome.targets as target (target.id)}
						<div class="space-y-1 rounded border p-2">
							<p class="break-all">{target.id} · {jobStatusLabel(target.status)}</p>
							{#if target.message}<p class="break-words whitespace-pre-wrap">{target.message}</p>{/if}
							{#if target.activityId}<Button variant="link" onclick={() => activityStore.openCenter(target.activityId)}
									>{m.activity()}</Button
								>{/if}
						</div>
					{/each}
				{/if}
				{#if action.error}<p class="text-destructive">{action.error.message}</p>{/if}
				{#if ['failed', 'partial', 'needs_attention'].includes(run.status)}
					<Button disabled={action.isPending} onclick={() => action.mutate({ runId: run.id, action: 'retry' })}
						>{m.jobs_retry_run()}</Button
					>
				{/if}
				{#if run.status === 'queued' && run.attemptCount === 0 && !run.remoteDeliveryAttempted}
					<Button variant="outline" disabled={action.isPending} onclick={() => action.mutate({ runId: run.id, action: 'cancel' })}
						>{m.jobs_cancel_run()}</Button
					>
				{/if}
			</div>
		{/if}
	</Dialog.Content>
</Dialog.Root>

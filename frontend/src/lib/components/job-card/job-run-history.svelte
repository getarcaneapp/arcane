<script lang="ts">
	import { queryKeys } from '#lib/query/query-keys.js';
	import { createQuery, createMutation } from '@tanstack/svelte-query';
	import { m } from '#lib/paraglide/messages.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
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
	const selectedRun = $derived(selected ? detail.data : undefined);
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
</script>

{#snippet outcomeMessage(message: string | undefined)}
	{#if message}<p class="break-words whitespace-pre-wrap">{message}</p>{/if}
{/snippet}

{#snippet outcomeActivity(activityId: string | undefined)}
	{#if activityId}<Button variant="link" onclick={() => activityStore.openCenter(activityId)}>{m.activity()}</Button>{/if}
{/snippet}

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
		{#if selectedRun}
			{@const run = selectedRun}
			<div class="space-y-3 border-t pt-4 text-sm">
				<p class="break-all"><strong>{m.jobs_run_id()}:</strong> {run.id}</p>
				<p>{jobStatusLabel(run.status)}</p>
				{#each metadata as item (item.id)}<p class={item.class}>{item.label ? `${item.label}: ` : ''}{item.value}</p>{/each}
				{@render outcomeActivity(run.outcome.activityId)}
				<h4 class="font-medium">{m.jobs_run_attempts()}: {run.attemptCount}</h4>
				{#each run.attempts ?? [] as attempt (attempt.number)}
					<div class="space-y-1 rounded border p-2">
						<p>{attempt.number} · {formatDateTimeShort(attempt.startedAt)} · {jobStatusLabel(attempt.outcome.status)}</p>
						{@render outcomeMessage(attempt.outcome.message)}
					</div>
				{/each}
				{#if run.outcome.targets?.length}
					<h4 class="font-medium">{m.jobs_run_targets()}</h4>
					{#each run.outcome.targets as target (target.id)}
						<div class="space-y-1 rounded border p-2">
							<p class="break-all">{target.id} · {jobStatusLabel(target.status)}</p>
							{@render outcomeMessage(target.message)}
							{@render outcomeActivity(target.activityId)}
						</div>
					{/each}
				{/if}
				{#if action.error}<p class="text-destructive">{action.error.message}</p>{/if}
				{#each availableActions as item (item.id)}
					<Button
						variant={item.variant}
						disabled={action.isPending}
						onclick={() => action.mutate({ runId: run.id, action: item.id })}>{item.label}</Button
					>
				{/each}
			</div>
		{/if}
	</Dialog.Content>
</Dialog.Root>

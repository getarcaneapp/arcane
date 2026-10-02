<script lang="ts">
	import { AlertTriangleIcon, EditIcon, EllipsisIcon, RestartIcon, StartIcon, ClockIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { formatDateTimeShort, formatRelativeTime } from '#lib/utils/formatting.js';
	import type { Snippet } from 'svelte';
	import type { JobStatus } from '#lib/types/settings.js';
	import JobScheduleDialog from './job-schedule-dialog.svelte';
	import { jobStatusLabel, jobNameLabel, jobStatusTone } from './job-status';
	import { toast } from 'svelte-sonner';
	import { createMutation } from '@tanstack/svelte-query';

	let {
		job,
		environmentId = '0',
		isAgent = false,
		durableRuns = false,
		enabledOverride,
		headerAccessory,
		onSelect,
		onScheduleUpdate
	}: {
		job: JobStatus;
		environmentId?: string;
		isAgent?: boolean;
		durableRuns?: boolean;
		enabledOverride?: boolean;
		headerAccessory?: Snippet;
		onSelect?: (job: JobStatus, section: 'overview' | 'history') => void;
		onScheduleUpdate?: () => void;
	} = $props();

	let showScheduleDialog = $state(false);
	const runJobMutation = createMutation(() => ({
		mutationFn: () => jobScheduleService.runJob(job.id, environmentId),
		retry: false,
		onSuccess: () => {
			toast.success(m.jobs_run_queued());
			onScheduleUpdate?.();
		},
		onError: (err) => toast.error(m.jobs_run_now(), { description: extractApiErrorMessage(err) })
	}));
	const restartMutation = createMutation(() => ({
		mutationFn: () => jobScheduleService.restartWorker(job.id, environmentId),
		onSuccess: () => {
			toast.success(m.jobs_worker_restarted());
			onScheduleUpdate?.();
		},
		onError: (err) => toast.error(m.jobs_restart_worker(), { description: extractApiErrorMessage(err) })
	}));

	const run = $derived(job.currentRun ?? job.lastRun);
	const isEnabled = $derived(enabledOverride ?? job.enabled);
	const isSubmitting = $derived(runJobMutation.isPending);
	const canRun = $derived(durableRuns && isEnabled && job.canRunManually && !isSubmitting && !(isAgent && job.managerOnly));
	const canEditSchedule = $derived(isEnabled && !!job.settingsKey && !(isAgent && job.managerOnly));
	const canRestartWorker = $derived(durableRuns && job.isContinuous && isEnabled);
	const hasError = $derived(!!job.lastError || !!job.workerHealth?.lastError);
	const isActive = $derived(['running', 'retrying', 'queued'].includes(run?.status ?? ''));

	const statusBadge = $derived.by(() => {
		if (!isEnabled) return { tone: 'gray' as const, label: m.common_disabled() };
		if (run) return { tone: jobStatusTone(run.status), label: jobStatusLabel(run.status) };
		if (job.isContinuous) return { tone: jobStatusTone(job.workerHealth?.status), label: m.jobs_continuous() };
		return null;
	});
	const description = $derived(job.id.startsWith('environment-health:') ? m.jobs_health_scope_description() : job.description);
	const lastRunAt = $derived(run?.finishedAt ?? run?.updatedAt ?? job.lastSuccess);
	const showNextRun = $derived(isEnabled && !!job.nextRun && (!job.isContinuous || !!job.settingsKey));

	function select(section: 'overview' | 'history' = 'overview') {
		onSelect?.(job, section);
	}

	// Controls inside the row opt out of row selection via data-row-control.
	function handleRowClick(event: MouseEvent) {
		if ((event.target as HTMLElement | null)?.closest('[data-row-control]')) return;
		select();
	}

	function handleRowKeydown(event: KeyboardEvent) {
		if (event.target !== event.currentTarget) return;
		if (event.key !== 'Enter' && event.key !== ' ') return;
		event.preventDefault();
		select();
	}
</script>

<div
	role="button"
	tabindex="0"
	class="group flex min-w-0 cursor-pointer items-center gap-3 px-4 py-3 transition-colors outline-none hover:bg-muted/40 focus-visible:bg-muted/40 sm:gap-4"
	onclick={handleRowClick}
	onkeydown={handleRowKeydown}
>
	{#if headerAccessory}
		<div class="flex shrink-0 items-center" data-row-control>
			{@render headerAccessory()}
		</div>
	{/if}

	<div class="flex min-w-0 flex-1 flex-col gap-0.5">
		<div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
			<span class="truncate text-sm font-medium">{jobNameLabel(job)}</span>
			{#if statusBadge}
				<Badge variant={statusBadge.tone} size="sm">
					{#if isActive}<Spinner class="size-2.5" />{/if}
					{statusBadge.label}
				</Badge>
			{/if}
			{#if hasError}
				<ArcaneTooltip.Root>
					<ArcaneTooltip.Trigger>
						<AlertTriangleIcon class="size-3.5 text-destructive" aria-label={m.jobs_last_error()} />
					</ArcaneTooltip.Trigger>
					<ArcaneTooltip.Content class="max-w-80 break-words"
						>{job.lastError ?? job.workerHealth?.lastError}</ArcaneTooltip.Content
					>
				</ArcaneTooltip.Root>
			{/if}
			{#if job.children?.length}
				<Badge variant="gray" size="sm">{m.jobs_target_runs({ count: job.children.length })}</Badge>
			{/if}
		</div>
		{#if description}
			<p class="truncate text-xs text-muted-foreground">{description}</p>
		{/if}
	</div>

	<div class="hidden shrink-0 flex-col items-end gap-0.5 text-xs text-muted-foreground tabular-nums md:flex">
		{#if lastRunAt}
			<span title={formatDateTimeShort(lastRunAt)}>{m.jobs_last_run()}: {formatRelativeTime(lastRunAt)}</span>
		{:else if isEnabled}
			<span>{m.jobs_never_run()}</span>
		{/if}
		{#if showNextRun}
			<span class="flex items-center gap-1" title={formatDateTimeShort(job.nextRun)}>
				<ClockIcon class="size-3" />
				{m.jobs_next_run()}: {formatRelativeTime(job.nextRun)}
			</span>
		{/if}
	</div>

	<div class="flex shrink-0 items-center gap-1" data-row-control>
		{#if isEnabled && job.canRunManually}
			<Button variant="outline" size="sm" onclick={() => runJobMutation.mutate()} disabled={!canRun}>
				{#if isSubmitting}<Spinner data-icon="inline-start" />{:else}<StartIcon data-icon="inline-start" />{/if}
				<span class="hidden sm:inline">{job.children?.length ? m.jobs_run_all() : m.jobs_run_now()}</span>
			</Button>
		{/if}
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="ghost" size="icon" class="size-8" aria-label={m.common_actions()}>
						<EllipsisIcon class="size-4" />
					</Button>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content align="end" class="min-w-44">
				<DropdownMenu.Item onclick={() => select('overview')}>{m.common_view_details()}</DropdownMenu.Item>
				{#if durableRuns}
					<DropdownMenu.Item onclick={() => select('history')}>{m.jobs_run_history()}</DropdownMenu.Item>
				{/if}
				{#if canEditSchedule || canRestartWorker}
					<DropdownMenu.Separator />
				{/if}
				{#if canEditSchedule}
					<DropdownMenu.Item onclick={() => (showScheduleDialog = true)}>
						<EditIcon class="size-4" />
						{m.jobs_edit_schedule()}
					</DropdownMenu.Item>
				{/if}
				{#if canRestartWorker}
					<DropdownMenu.Item disabled={restartMutation.isPending} onclick={() => restartMutation.mutate()}>
						<RestartIcon class="size-4" />
						{m.jobs_restart_worker()}
					</DropdownMenu.Item>
				{/if}
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</div>
</div>

{#if showScheduleDialog}
	<JobScheduleDialog
		{job}
		{environmentId}
		bind:open={showScheduleDialog}
		onUpdate={() => {
			showScheduleDialog = false;
			onScheduleUpdate?.();
		}}
	/>
{/if}

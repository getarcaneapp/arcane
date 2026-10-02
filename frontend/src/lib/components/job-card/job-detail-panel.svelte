<script lang="ts">
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { TabBar, type TabItem } from '#lib/components/tab-bar/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { AlertTriangleIcon, CheckIcon, ClockIcon, CloseIcon, EditIcon, RestartIcon, StartIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { formatDateTimeShort, formatRelativeTime } from '#lib/utils/formatting.js';
	import type { JobStatus } from '#lib/types/settings.js';
	import type { Snippet } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { createMutation } from '@tanstack/svelte-query';
	import JobRunHistory from './job-run-history.svelte';
	import JobScheduleDialog from './job-schedule-dialog.svelte';
	import { jobNameLabel, jobStatusLabel, jobStatusTone, type JobPanelSection } from './job-status';

	let {
		job,
		environmentId,
		isAgent = false,
		durableRuns = false,
		enabledOverride,
		open = $bindable(false),
		section = $bindable<JobPanelSection>('overview'),
		settings,
		enableControl,
		onScheduleUpdate
	}: {
		job: JobStatus;
		environmentId: string;
		isAgent?: boolean;
		durableRuns?: boolean;
		enabledOverride?: boolean;
		open?: boolean;
		section?: JobPanelSection;
		settings?: Snippet<[JobStatus]>;
		/** The job's enable switch, mirrored from the row so it can be toggled from here. */
		enableControl?: Snippet;
		onScheduleUpdate?: () => void;
	} = $props();

	let showScheduleDialog = $state(false);
	// History can show a target's runs instead of the parent's; cleared when another job is shown.
	let historyJobId = $state<string | null>(null);
	let historyOwnerId = $state<string | null>(null);

	const runMutation = createMutation(() => ({
		mutationFn: (jobId: string) => jobScheduleService.runJob(jobId, environmentId),
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
	const managerLocked = $derived(isAgent && job.managerOnly);
	const canRun = $derived(durableRuns && isEnabled && job.canRunManually && !runMutation.isPending && !managerLocked);
	const canEditSchedule = $derived(isEnabled && !!job.settingsKey && !managerLocked);
	const canRestartWorker = $derived(durableRuns && job.isContinuous && isEnabled);
	const description = $derived(job.id.startsWith('environment-health:') ? m.jobs_health_scope_description() : job.description);
	const showSchedule = $derived(isEnabled && !!job.schedule && (!job.isContinuous || !!job.settingsKey));
	const statusLabel = $derived.by(() => {
		if (!isEnabled) return m.common_disabled();
		if (run) return jobStatusLabel(run.status);
		if (job.isContinuous) return m.jobs_continuous();
		return m.jobs_never_run();
	});
	const statusTone = $derived(!isEnabled ? 'gray' : run ? jobStatusTone(run.status) : jobStatusTone(job.workerHealth?.status));
	const errors = $derived([job.lastError, job.workerHealth?.lastError].filter((text): text is string => !!text));
	const activeHistoryJobId = $derived(historyOwnerId === job.id && historyJobId ? historyJobId : job.id);
	const activeHistoryJob = $derived(job.children?.find((child) => child.id === activeHistoryJobId));

	// Past runs stay readable while a job is disabled.
	const historyAvailable = $derived(durableRuns);
	const sectionItems = $derived.by((): TabItem[] => {
		const items: TabItem[] = [{ value: 'overview', label: m.common_overview() }];
		if (historyAvailable) items.push({ value: 'history', label: m.history() });
		return items;
	});
	const activeSection = $derived(section === 'history' && historyAvailable ? 'history' : 'overview');

	function showHistory(jobId: string | null) {
		historyJobId = jobId;
		historyOwnerId = job.id;
		section = 'history';
	}
</script>

{#snippet metaRow(label: string, value: string, title?: string)}
	<div class="flex items-baseline justify-between gap-4 text-sm">
		<span class="text-muted-foreground">{label}</span>
		<span class="text-right tabular-nums" {title}>{value}</span>
	</div>
{/snippet}

{#snippet sectionTitle(text: string)}
	<h3 class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{text}</h3>
{/snippet}

<ResponsiveDialog bind:open variant="sheet" title={jobNameLabel(job)} {description} contentClass="sm:max-w-xl">
	<div class="flex flex-col gap-5 pb-6">
		<div class="flex flex-wrap items-center gap-2">
			{@render enableControl?.()}
			<Badge variant={statusTone}>{statusLabel}</Badge>
			{#if job.workerHealth}
				<Badge variant={jobStatusTone(job.workerHealth.status)} size="sm">
					{m.jobs_worker_health()}: {jobStatusLabel(job.workerHealth.status, true)}
				</Badge>
			{/if}
			<div class="ml-auto flex items-center gap-1">
				{#if canRestartWorker}
					<Button variant="ghost" size="sm" disabled={restartMutation.isPending} onclick={() => restartMutation.mutate()}>
						<RestartIcon data-icon="inline-start" />
						{m.jobs_restart_worker()}
					</Button>
				{/if}
				{#if isEnabled && job.canRunManually}
					<Button size="sm" disabled={!canRun} onclick={() => runMutation.mutate(job.id)}>
						{#if runMutation.isPending}<Spinner data-icon="inline-start" />{:else}<StartIcon data-icon="inline-start" />{/if}
						{job.children?.length ? m.jobs_run_all() : m.jobs_run_now()}
					</Button>
				{/if}
			</div>
		</div>

		{#if sectionItems.length > 1}
			<!-- Own Tabs.Root: the sheet sits inside the page's tabs context and must not drive it. -->
			<Tabs.Root value={activeSection}>
				<TabBar items={sectionItems} value={activeSection} onValueChange={(value) => (section = value as JobPanelSection)} />
			</Tabs.Root>
		{/if}

		{#if activeSection === 'history'}
			{#if activeHistoryJob}
				<div class="flex items-center justify-between gap-3 text-sm">
					<span class="truncate">{jobNameLabel(activeHistoryJob)}</span>
					<ArcaneButton
						action="base"
						tone="ghost"
						size="sm"
						icon={CloseIcon}
						customLabel={jobNameLabel(job)}
						onclick={() => showHistory(null)}
					/>
				</div>
			{/if}
			{#key activeHistoryJobId}
				<JobRunHistory jobId={activeHistoryJobId} {environmentId} onUpdate={onScheduleUpdate} />
			{/key}
		{:else}
			{#each errors as text (text)}
				<Alert.Root variant="destructive-subtle" size="sm">
					<AlertTriangleIcon class="size-4" />
					<Alert.Description class="break-words">{text}</Alert.Description>
				</Alert.Root>
			{/each}

			{#if isEnabled}
				<section class="flex flex-col gap-2">
					{@render sectionTitle(m.jobs_schedule())}
					{#if showSchedule}
						<div class="flex items-center justify-between gap-3">
							<code class="rounded-md bg-muted/60 px-2 py-1 text-xs break-all">{job.schedule}</code>
							{#if canEditSchedule}
								<Button variant="ghost" size="sm" onclick={() => (showScheduleDialog = true)}>
									<EditIcon data-icon="inline-start" />
									{m.jobs_edit_schedule()}
								</Button>
							{/if}
						</div>
						{#if job.nextRun}
							{@render metaRow(m.jobs_next_run(), `${formatRelativeTime(job.nextRun)} · ${formatDateTimeShort(job.nextRun)}`)}
						{/if}
					{:else if job.isContinuous}
						<p class="flex items-center gap-2 text-sm text-muted-foreground">
							<ClockIcon class="size-3.5" />
							{m.jobs_continuous()}
						</p>
					{/if}
					{#if job.lastSuccess}
						{@render metaRow(m.jobs_last_success(), formatRelativeTime(job.lastSuccess), formatDateTimeShort(job.lastSuccess))}
					{/if}
					{#if run?.nextAttempt}
						{@render metaRow(m.jobs_next_retry(), formatDateTimeShort(run.nextAttempt))}
					{/if}
					{#if job.workerHealth?.nextRetry}
						{@render metaRow(m.jobs_next_retry(), formatDateTimeShort(job.workerHealth.nextRetry))}
					{/if}
					{#if run?.status === 'waiting' && run.remoteOutcome}
						{@render metaRow(m.jobs_remote_status(), jobStatusLabel(run.remoteOutcome.status))}
					{/if}
					{#if run?.lastConfirmedAt}
						{@render metaRow(m.jobs_last_confirmed(), formatDateTimeShort(run.lastConfirmedAt))}
					{/if}
					{#if run?.status === 'waiting' && run.outcome.message}
						<p class="text-sm break-words text-muted-foreground">{run.outcome.message}</p>
					{/if}
				</section>
			{/if}

			{#if job.prerequisites?.length}
				<section class="flex flex-col gap-2">
					{@render sectionTitle(m.jobs_prerequisites())}
					<ul class="flex flex-col gap-1.5">
						{#each job.prerequisites as prereq (prereq.settingKey)}
							<li class="flex items-center gap-2 text-sm">
								{#if prereq.isMet}
									<CheckIcon class="size-4 shrink-0 text-success" aria-label={m.jobs_prerequisite_met()} />
								{:else}
									<CloseIcon class="size-4 shrink-0 text-destructive" aria-label={m.jobs_prerequisite_unmet()} />
								{/if}
								{#if prereq.settingsUrl && !prereq.isMet}
									<a href={prereq.settingsUrl} class="underline-offset-4 hover:underline" onclick={() => (open = false)}>
										{prereq.label}
									</a>
								{:else}
									<span>{prereq.label}</span>
								{/if}
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			{#if settings}
				<section class="flex flex-col gap-3">
					{@render sectionTitle(m.settings())}
					{@render settings(job)}
				</section>
			{/if}

			{#if job.children?.length}
				<section class="flex flex-col gap-2">
					{@render sectionTitle(m.jobs_target_runs({ count: job.children.length }))}
					<div class="divide-y divide-border/50 overflow-hidden rounded-lg border border-border/60">
						{#each job.children as child (child.id)}
							{@const childRun = child.currentRun ?? child.lastRun}
							<div class="flex items-center gap-3 px-3 py-2 text-sm">
								<div class="flex min-w-0 flex-1 flex-col">
									<span class="truncate">{jobNameLabel(child)}</span>
									{#if childRun}
										<span class="text-xs text-muted-foreground tabular-nums">
											{formatRelativeTime(childRun.finishedAt ?? childRun.updatedAt)}
										</span>
									{/if}
								</div>
								{#if childRun}
									<Badge variant={jobStatusTone(childRun.status)} size="sm">{jobStatusLabel(childRun.status)}</Badge>
								{/if}
								{#if durableRuns}
									<Button variant="ghost" size="sm" onclick={() => showHistory(child.id)}>{m.history()}</Button>
								{/if}
								{#if child.canRunManually}
									<Button
										variant="outline"
										size="icon"
										class="size-8"
										aria-label={m.jobs_run_now()}
										disabled={!durableRuns || runMutation.isPending || managerLocked}
										onclick={() => runMutation.mutate(child.id)}
									>
										<StartIcon class="size-4" />
									</Button>
								{/if}
							</div>
						{/each}
					</div>
				</section>
			{/if}
		{/if}
	</div>
</ResponsiveDialog>

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

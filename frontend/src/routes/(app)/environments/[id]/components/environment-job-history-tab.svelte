<script lang="ts">
	import { page } from '$app/state';
	import { createQuery } from '@tanstack/svelte-query';

	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import JobRunHistory from '#lib/components/job-card/job-run-history.svelte';
	import { jobNameLabel } from '#lib/components/job-card/job-status.js';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { EmptyState } from '#lib/components/states/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertTriangleIcon, InfoIcon, JobsIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { jobScheduleService } from '#lib/services/job-schedule-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';

	let { environmentId }: { environmentId: string } = $props();

	const jobsQuery = createQuery(() => ({
		queryKey: queryKeys.jobs.list(environmentId),
		queryFn: ({ signal }) => jobScheduleService.listJobs(environmentId, { signal, suppressAccessDeniedToast: true })
	}));
	const jobs = $derived(
		(jobsQuery.data?.jobs ?? [])
			.filter((job) => !job.managerOnly || environmentId === '0')
			.flatMap((job) => [job, ...(job.children ?? [])])
	);
	let selectedJobId = $state(page.url.searchParams.get('job') ?? '');
	const activeJobId = $derived(jobs.some((job) => job.id === selectedJobId) ? selectedJobId : (jobs[0]?.id ?? ''));
</script>

{#if jobsQuery.error}
	<Alert.Root variant="destructive-subtle" size="sm">
		<AlertTriangleIcon class="size-4" />
		<Alert.Description>{extractApiErrorMessage(jobsQuery.error)}</Alert.Description>
	</Alert.Root>
{:else if jobsQuery.isPending}
	<div class="flex h-32 items-center justify-center">
		<Spinner class="size-8" />
	</div>
{:else if jobs.length === 0}
	<EmptyState icon={JobsIcon} title={m.jobs_empty_title()} />
{:else if !jobsQuery.data.durableRuns}
	<Alert.Root size="sm">
		<InfoIcon class="size-4" />
		<Alert.Description>{m.jobs_upgrade_required()}</Alert.Description>
	</Alert.Root>
{:else}
	<SettingsSection title={m.jobs_run_history()} variant="plain">
		{#snippet actions()}
			<div class="w-64">
				<SelectWithLabel
					id="job-history-job"
					label={m.jobs()}
					hideLabel
					value={activeJobId}
					onValueChange={(value) => (selectedJobId = value)}
					options={jobs.map((job) => ({ value: job.id, label: jobNameLabel(job) }))}
				/>
			</div>
		{/snippet}
		{#key activeJobId}
			<JobRunHistory jobId={activeJobId} {environmentId} />
		{/key}
	</SettingsSection>
{/if}

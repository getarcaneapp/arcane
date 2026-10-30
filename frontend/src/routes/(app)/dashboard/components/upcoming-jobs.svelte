<script lang="ts">
	import { resolve } from '$app/paths';

	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import { jobNameLabel } from '#lib/components/job-card/job-status.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDateTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting.js';

	import type { EnvironmentJob } from '../overview.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	const JOB_LIMIT = 5;
	const USER_JOB_GROUPS: ReadonlySet<string> = new Set([
		'auto-update',
		'image-polling',
		'vulnerability-scan',
		'scheduled-prune',
		'volume-backup',
		'system-backup',
		'gitops-sync',
		'auto-heal',
		'auto-patch'
	]);

	let {
		jobs,
		loading,
		failed
	}: {
		jobs: EnvironmentJob[];
		loading: boolean;
		failed: boolean;
	} = $props();

	const upcomingJobs = $derived.by(() => {
		const now = Temporal.Now.instant();
		return jobs
			.flatMap((entry) => {
				const [group = ''] = entry.job.id.split(':');
				const nextRun = parseInstant(entry.job.nextRun);
				return USER_JOB_GROUPS.has(group) && entry.job.enabled && nextRun && Temporal.Instant.compare(nextRun, now) > 0
					? [{ ...entry, nextRun }]
					: [];
			})
			.sort((a, b) => Temporal.Instant.compare(a.nextRun, b.nextRun))
			.slice(0, JOB_LIMIT);
	});
</script>

<DashboardPanel title={m.upcoming_jobs()}>
	{#if loading && jobs.length === 0}
		<div class="space-y-3 py-2">
			<Skeleton class="h-4 w-3/4" />
			<Skeleton class="h-4 w-1/2" />
		</div>
	{:else if upcomingJobs.length === 0}
		<p class="py-2 text-sm text-muted-foreground">{failed ? m.common_unavailable() : m.no_upcoming_jobs()}</p>
	{:else}
		<ul class="divide-y divide-border/40">
			{#each upcomingJobs as entry (`${entry.environmentId}:${entry.job.id}`)}
				<li>
					<a
						href={resolve('/(app)/environments/[id]?tab=jobs', { id: entry.environmentId })}
						class="-mx-2 grid grid-cols-content-action items-center gap-3 rounded-md px-2 py-2.5 transition-colors hover:bg-muted/40"
					>
						<span class="min-w-0 space-y-1">
							<span class="block truncate text-sm font-medium">{jobNameLabel(entry.job)}</span>
							<EnvironmentCell value={entry.environmentId} />
						</span>
						<span class="text-xs whitespace-nowrap text-muted-foreground tabular-nums" title={formatDateTime(entry.nextRun)}>
							{formatRelativeTime(entry.nextRun)}
						</span>
					</a>
				</li>
			{/each}
		</ul>
	{/if}
</DashboardPanel>

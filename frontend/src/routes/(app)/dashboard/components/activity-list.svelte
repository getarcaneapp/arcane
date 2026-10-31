<script lang="ts">
	import { activityStatusAccentClass, activityTitle } from '#lib/components/activity/activity-labels.js';
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import SegmentToggle from '#lib/components/segment-toggle.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { cn } from '#lib/utils.js';
	import { formatRelativeTime } from '#lib/utils/formatting.js';

	import { isDeployActivity, type CollapsedActivity } from '../overview.helpers';
	import DashboardPanel from './dashboard-panel.svelte';

	const ACTIVITY_LIMIT = 5;

	let {
		groups,
		loading,
		unavailable
	}: {
		groups: CollapsedActivity[];
		loading: boolean;
		unavailable: boolean;
	} = $props();

	let filter = $state<'all' | 'deploys' | 'failures'>('all');

	const visibleActivities = $derived(
		groups
			.filter(({ activity }) => {
				if (filter === 'deploys') return isDeployActivity(activity);
				if (filter === 'failures') return activity.status === 'failed';
				return true;
			})
			.slice(0, ACTIVITY_LIMIT)
	);
</script>

<DashboardPanel title={m.recent_activity()}>
	{#snippet actions()}
		<SegmentToggle
			label={m.recent_activity()}
			options={[
				{ value: 'all', label: m.common_all() },
				{ value: 'deploys', label: m.deploys() },
				{ value: 'failures', label: m.failures() }
			]}
			current={filter}
			onSelect={(value) => (filter = value === 'deploys' || value === 'failures' ? value : 'all')}
		/>
	{/snippet}
	{#if loading && groups.length === 0}
		<div class="space-y-3 py-2">
			<Skeleton class="h-4 w-3/4" />
			<Skeleton class="h-4 w-1/2" />
		</div>
	{:else if visibleActivities.length === 0}
		<p class="py-2 text-sm text-muted-foreground">{unavailable ? m.common_unavailable() : m.no_recent_activity()}</p>
	{:else}
		<ul class="divide-y divide-border/40">
			{#each visibleActivities as { key, activity, ids } (key)}
				{@const environmentId = activity.sourceEnvironmentId || activity.environmentId}
				{@const title = activityTitle(activity)}
				<li>
					<button
						type="button"
						class="-mx-2 grid w-full-outset grid-cols-icon-content-action items-start gap-3 rounded-md hover-glow px-2 py-2.5 text-left"
						onclick={() => activityStore.openCenter(activity.id, undefined, environmentId)}
					>
						<span class={cn('mt-1.5 size-2 shrink-0 rounded-full', activityStatusAccentClass(activity.status))}></span>
						<span class="min-w-0 space-y-1">
							<span class="flex min-w-0 items-center gap-1.5 text-sm">
								<span class="truncate">
									<span class="font-medium">{activity.status === 'failed' ? m.job_failed({ job: title }) : title}</span>
									{#if activity.resourceName}<span class="text-muted-foreground"> {activity.resourceName}</span>{/if}
								</span>
								{#if ids.length > 1}
									<Badge variant="gray" size="xs">{m.times_count({ count: ids.length })}</Badge>
								{/if}
							</span>
							<EnvironmentCell value={environmentId} />
						</span>
						<span class="text-xs whitespace-nowrap text-muted-foreground tabular-nums">
							{formatRelativeTime(activity.endedAt || activity.startedAt)}
						</span>
					</button>
				</li>
			{/each}
		</ul>
	{/if}
	<button
		type="button"
		class="mt-auto self-start text-xs font-medium text-primary hover:underline"
		onclick={() => activityStore.openCenter()}
	>
		{m.common_view_all()}
	</button>
</DashboardPanel>

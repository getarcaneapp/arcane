<script lang="ts">
	import { ActivityIcon, ArrowLeftIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { Activity, ActivityBatchGroup } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';

	import ActivityBatchSteps from './activity-batch-steps.svelte';
	import ActivityDetailHeader from './activity-detail-header.svelte';
	import ActivityDetailPanel from './activity-detail-panel.svelte';
	import { activityGroupLead, activityGroupTarget, activityGroupTitle, findActivityGroup } from './activity-groups';

	let {
		focusKey,
		compact = false
	}: {
		focusKey: string;
		compact?: boolean;
	} = $props();

	type Resolved = { kind: 'batch'; group: ActivityBatchGroup } | { kind: 'single'; activity: Activity };

	// Lookups use the unfiltered groups so a focused item survives search and filters.
	const resolved = $derived.by((): Resolved | null => {
		if (focusKey.startsWith('batch:')) {
			const group = findActivityGroup(activityStore.allGroups, focusKey);
			if (group?.kind === 'batch') return { kind: 'batch', group };
			// A batch with a single member renders as a plain activity.
			const batchId = focusKey.slice('batch:'.length);
			const only = activityStore.activities.find((item) => item.batchId === batchId);
			return only ? { kind: 'single', activity: only } : null;
		}
		const activity = activityStore.getActivity(focusKey) ?? activityStore.getDetail(focusKey)?.activity;
		return activity ? { kind: 'single', activity } : null;
	});

	const parent = $derived.by(() => {
		if (resolved?.kind !== 'single' || !resolved.activity.batchId) return undefined;
		const group = findActivityGroup(activityStore.allGroups, `batch:${resolved.activity.batchId}`);
		return group?.kind === 'batch' ? group : undefined;
	});

	const summary = $derived(resolved?.kind === 'batch' ? resolved.group.items.find((item) => item.type === 'job_run') : undefined);
</script>

{#if resolved}
	<div data-activity-detail class="flex min-h-0 flex-1 flex-col">
		{#if parent}
			<button
				type="button"
				onclick={() => activityStore.focus(`batch:${parent.batchId}`)}
				class={cn(
					'flex min-w-0 cursor-pointer items-center gap-1 pt-3 text-xs text-muted-foreground hover:text-foreground',
					compact ? 'px-3' : 'px-5'
				)}
			>
				<ArrowLeftIcon class="size-4 shrink-0" aria-hidden="true" />
				<span class="truncate">{activityGroupTitle(parent)} {activityGroupTarget(parent)}</span>
			</button>
		{/if}

		{#if resolved.kind === 'batch'}
			<ActivityDetailHeader
				activity={activityGroupLead(resolved.group)}
				status={resolved.group.status}
				target={activityGroupTarget(resolved.group)}
				batch
				{compact}
			/>
		{:else}
			<ActivityDetailHeader
				activity={resolved.activity}
				status={resolved.activity.status}
				target={resolved.activity.resourceName || resolved.activity.resourceId || ''}
				{compact}
			/>
		{/if}

		<div class={cn('flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto', compact ? 'p-4' : 'px-6 py-5')}>
			{#if resolved.kind === 'batch'}
				{#if summary}
					<ActivityDetailPanel activity={summary} />
				{/if}
				<ActivityBatchSteps group={resolved.group} />
			{:else}
				<ActivityDetailPanel activity={resolved.activity} fill={!compact} />
			{/if}
		</div>
	</div>
{:else}
	<div class="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center">
		<ActivityIcon class="size-6 animate-pulse text-muted-foreground" aria-hidden="true" />
		<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
	</div>
{/if}

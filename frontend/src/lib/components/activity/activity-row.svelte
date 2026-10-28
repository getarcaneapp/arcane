<script lang="ts">
	import type { ActivityGroup } from '#lib/types/activity.type.js';
	import { cn } from '#lib/utils.js';
	import { formatRelativeTime, formatTime } from '#lib/utils/formatting.js';

	import ActivityElapsed from './activity-elapsed.svelte';
	import {
		activityGroupEnvironment,
		activityGroupKey,
		activityGroupLead,
		activityGroupMembers,
		activityGroupStatus,
		activityGroupTarget,
		activityGroupTitle
	} from './activity-groups';
	import ActivityStatusGlyph from './activity-status-glyph.svelte';
	import ActivityStatusStrip from './activity-status-strip.svelte';

	let {
		group,
		selected = false,
		time = 'relative',
		onSelect
	}: {
		group: ActivityGroup;
		selected?: boolean;
		/** `relative` reads "5m ago" for glanceable lists; `clock` shows the start time beside day headers. */
		time?: 'relative' | 'clock';
		onSelect: (key: string) => void;
	} = $props();

	const lead = $derived(activityGroupLead(group));
	const status = $derived(activityGroupStatus(group));
	const target = $derived(activityGroupTarget(group));
</script>

<button
	type="button"
	data-activity-row
	aria-current={selected ? 'true' : undefined}
	onclick={() => onSelect(activityGroupKey(group))}
	class={cn(
		'relative grid w-full cursor-pointer grid-cols-icon-content-action items-start gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-foreground/5 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
		selected && 'bg-primary/10 hover:bg-primary/15'
	)}
>
	{#if selected}
		<span aria-hidden="true" class="absolute inset-y-2 left-0 w-0.5 rounded-full bg-primary"></span>
	{/if}
	<ActivityStatusGlyph {status} class="mt-px" />
	<span class="flex min-w-0 flex-col gap-1">
		<span class="truncate text-sm">
			<span class="font-semibold text-foreground">{activityGroupTitle(group)}</span>
			{#if target}<span class="text-muted-foreground">{target}</span>{/if}
		</span>
		{#if group.kind === 'batch'}
			<ActivityStatusStrip items={activityGroupMembers(group)} done={group.done} total={group.total} />
		{:else}
			<span class="truncate text-xs text-muted-foreground">{activityGroupEnvironment(group)}</span>
		{/if}
	</span>
	<span class="pt-px text-2xs whitespace-nowrap text-muted-foreground/80 tabular-nums">
		{#if status === 'running'}
			<ActivityElapsed activity={lead} />
		{:else if time === 'clock'}
			{formatTime(lead.startedAt, { includeSeconds: false })}
		{:else}
			{formatRelativeTime(lead.endedAt || lead.startedAt)}
		{/if}
	</span>
</button>

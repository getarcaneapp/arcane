<script lang="ts">
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { ActivityGroup } from '#lib/types/activity.type.js';

	import { confirmCancelActivity } from './activity-cancel';
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
	import ActivityIconButton from './activity-icon-button.svelte';
	import ActivityStatusGlyph from './activity-status-glyph.svelte';
	import ActivityStatusStrip from './activity-status-strip.svelte';

	let { group }: { group: ActivityGroup } = $props();

	const lead = $derived(activityGroupLead(group));
	const target = $derived(activityGroupTarget(group));
	const cancelable = $derived(group.kind === 'single' && lead.type !== 'job_run');
	const message = $derived(
		group.kind === 'batch'
			? (activityGroupMembers(group).find((item) => item.status === 'running' && item.latestMessage)?.latestMessage ??
					lead.latestMessage)
			: lead.latestMessage
	);
</script>

<div
	data-activity-row
	class="group/track flex flex-col gap-2 rounded-xl border border-border/50 bg-foreground/3 px-3 py-2.5 transition-colors hover:border-border"
>
	<div class="flex items-center gap-2.5">
		<ActivityStatusGlyph status={activityGroupStatus(group)} />
		<button
			type="button"
			onclick={() => activityStore.focus(activityGroupKey(group))}
			class="flex min-w-0 flex-1 cursor-pointer flex-col text-left focus-visible:underline focus-visible:outline-hidden"
		>
			<span class="truncate text-sm">
				<span class="font-semibold">{activityGroupTitle(group)}</span>
				{#if target}<span class="text-muted-foreground">{target}</span>{/if}
			</span>
			<span class="truncate text-2xs text-muted-foreground">{activityGroupEnvironment(group)}</span>
		</button>
		<span class="text-2xs text-muted-foreground"><ActivityElapsed activity={lead} /></span>
		{#if cancelable}
			<IfPermitted perm="activities:cancel">
				<ActivityIconButton
					label={m.activity_cancel()}
					tone="danger"
					disabled={activityStore.isCancelling(lead.id)}
					onclick={() => confirmCancelActivity(lead.id)}
					class="size-7 opacity-0 group-focus-within/track:opacity-100 group-hover/track:opacity-100"
				>
					<CloseIcon class="size-3.5" aria-hidden="true" />
				</ActivityIconButton>
			</IfPermitted>
		{/if}
	</div>

	{#if group.kind === 'batch'}
		<Progress value={group.done} max={Math.max(group.total, 1)} class="h-1" />
		<div class="flex items-center justify-between gap-3">
			<ActivityStatusStrip items={activityGroupMembers(group)} done={group.done} total={group.total} />
			<span class="shrink-0 text-2xs text-muted-foreground tabular-nums">
				{m.activity_batch_done_of_total({ done: group.done, total: group.total })}
			</span>
		</div>
	{:else}
		<Progress value={100} indeterminate class="h-1" />
	{/if}

	{#if message}
		<p class="truncate font-mono text-2xs text-muted-foreground">{message}</p>
	{/if}
</div>

<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { ActivityBatchGroup } from '#lib/types/activity.type.js';

	import { activityGroupMembers } from './activity-groups';
	import ActivityRow from './activity-row.svelte';

	let { group }: { group: ActivityBatchGroup } = $props();

	const members = $derived(activityGroupMembers(group));
</script>

<section class="flex flex-col gap-2">
	<div class="flex items-baseline gap-2">
		<h4 class="text-xs font-semibold text-muted-foreground">{m.activity_steps()}</h4>
		<span class="text-2xs text-muted-foreground/80 tabular-nums">
			{m.activity_batch_done_of_total({ done: group.done, total: group.total })}
		</span>
		{#if group.failed > 0}
			<span class="text-2xs text-destructive tabular-nums">{m.activity_batch_failed_count({ count: group.failed })}</span>
		{/if}
	</div>
	<div class="flex flex-col rounded-lg border border-border/50 p-1">
		{#each members as activity (activity.id)}
			<ActivityRow group={{ kind: 'single', activity }} onSelect={(key) => activityStore.focus(key)} />
		{/each}
	</div>
</section>

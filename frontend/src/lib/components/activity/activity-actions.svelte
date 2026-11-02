<script lang="ts">
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import { CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import type { Activity, ActivityStatus } from '#lib/types/activity.type.js';

	import { confirmCancelActivity } from './activity-cancel';
	import { isActiveActivityStatus } from './activity-groups';
	import ActivityJobHistoryButton from './activity-job-history-button.svelte';

	let {
		activity,
		status,
		canCancel = true
	}: {
		activity: Activity;
		status: ActivityStatus;
		/** Batches are cancelled per member, so their summary header omits cancel. */
		canCancel?: boolean;
	} = $props();

	const cancelable = $derived(canCancel && activity.type !== 'job_run' && isActiveActivityStatus(status));
</script>

{#if cancelable || activity.type === 'job_run'}
	<div class="flex flex-wrap items-center gap-2 empty:hidden">
		{#if cancelable}
			<IfPermitted perm="activities:cancel">
				<Button
					variant="outline"
					size="sm"
					disabled={activityStore.isCancelling(activity.id)}
					onclick={() => confirmCancelActivity(activity.id)}
				>
					<CloseIcon aria-hidden="true" />
					{m.activity_cancel()}
				</Button>
			</IfPermitted>
		{/if}
		<ActivityJobHistoryButton {activity} />
	</div>
{/if}

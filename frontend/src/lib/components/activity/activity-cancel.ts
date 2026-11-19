import { toast } from 'svelte-sonner';

import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { activityStore } from '#lib/stores/activity.store.svelte.js';
import { extractApiErrorStatus } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export function confirmCancelActivity(activityId: string) {
	openConfirmDialog({
		title: m.activity_cancel_title(),
		message: m.activity_cancel_message(),
		confirm: {
			label: m.activity_cancel_confirm(),
			destructive: true,
			action: async () => {
				const cancelled = await tryCatch(activityStore.cancelActivity(activityId));
				if (cancelled.error === null) {
					toast.success(m.activity_cancel_success());
					return;
				}
				if (extractApiErrorStatus(cancelled.error) === 409) {
					toast.info(m.activity_cancel_already_finished());
					await activityStore.refresh();
					return;
				}
				console.error('Failed to cancel activity:', cancelled.error);
				toast.error(m.activity_cancel_failed());
			}
		}
	});
}

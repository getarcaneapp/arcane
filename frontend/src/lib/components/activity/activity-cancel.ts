import { toast } from 'svelte-sonner';

import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { activityStore } from '#lib/stores/activity.store.svelte.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export function confirmCancelActivity(activityId: string) {
	openConfirmDialog({
		title: m.activity_cancel_title(),
		message: m.activity_cancel_message(),
		confirm: {
			label: m.activity_cancel_confirm(),
			destructive: true,
			action: async () => {
				const operationResult1 = await tryCatch(
					(async () => {
						await activityStore.cancelActivity(activityId);
						toast.success(m.activity_cancel_success());
					})()
				);
				if (operationResult1.error !== null) {
					const error = operationResult1.error;

					if ((error as { response?: { status?: number } })?.response?.status === 409) {
						toast.info(m.activity_cancel_already_finished());
						await activityStore.refresh();
						return;
					}
					console.error('Failed to cancel activity:', error);
					toast.error(m.activity_cancel_failed());
				}
			}
		}
	});
}

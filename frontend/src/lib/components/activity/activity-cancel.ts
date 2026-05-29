import { openConfirmDialog } from '$lib/components/confirm-dialog';
import { toast } from 'svelte-sonner';
import { m } from '$lib/paraglide/messages';
import { activityStore } from '$lib/stores/activity.store.svelte';

/**
 * Opens a confirmation dialog and, on confirm, requests cancellation of the
 * activity. Shared by the activity center row action and the detail panel so the
 * confirm copy, toasts, and the already-finished (409) handling stay in one place.
 */
export function confirmCancelActivity(activityId: string) {
	openConfirmDialog({
		title: m.activity_cancel_title(),
		message: m.activity_cancel_message(),
		confirm: {
			label: m.activity_cancel_confirm(),
			destructive: true,
			action: async () => {
				try {
					await activityStore.cancelActivity(activityId);
					toast.success(m.activity_cancel_success());
				} catch (error) {
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

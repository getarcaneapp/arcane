import { toast } from 'svelte-sonner';

import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { podmanService, type PodmanPodAction } from '#lib/services/podman-service.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

type PodTarget = { id: string; name: string; environmentId: string };

export const podActionLabels: Record<PodmanPodAction, () => string> = {
	start: m.common_start,
	stop: m.common_stop,
	restart: m.common_restart,
	pause: m.common_pause,
	unpause: m.common_unpause,
	kill: m.common_kill
};

export async function runPodAction(
	pod: PodTarget,
	action: PodmanPodAction,
	onDone: () => void | Promise<void>,
	setLoading: (loading: boolean) => void = () => {}
) {
	setLoading(true);
	await handleApiResultWithCallbacks({
		result: await tryCatch(podmanService.podAction(pod.id, action, pod.environmentId)),
		message: m.common_action_failed_with_type({ action: podActionLabels[action](), type: pod.name }),
		setLoadingState: setLoading,
		onSuccess: async () => {
			toast.success(m.podman_action_success({ action: podActionLabels[action](), name: pod.name }));
			await onDone();
		}
	});
}

export function confirmRemovePod(pod: PodTarget, onDone: () => void | Promise<void>) {
	openConfirmDialog({
		title: m.common_remove_title({ resource: m.podman_pod() }),
		message: m.common_remove_confirm({ resource: pod.name }),
		checkboxes: [{ id: 'force', label: m.podman_force_remove() }],
		confirm: {
			label: m.common_remove(),
			destructive: true,
			action: async (checkboxStates) => {
				await handleApiResultWithCallbacks({
					result: await tryCatch(podmanService.removePod(pod.id, checkboxStates['force'] === true, pod.environmentId)),
					message: m.common_remove_failed({ resource: pod.name }),
					onSuccess: async () => {
						toast.success(m.common_remove_success({ resource: pod.name }));
						await onDone();
					}
				});
			}
		}
	});
}

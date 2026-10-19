import { toast } from 'svelte-sonner';

import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { podmanService, type PodmanQuadletAction } from '#lib/services/podman-service.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';

export const quadletActionLabels: Record<PodmanQuadletAction, () => string> = {
	start: m.common_start,
	stop: m.common_stop,
	restart: m.common_restart
};

export const quadletActions: PodmanQuadletAction[] = ['start', 'stop', 'restart'];

export async function runQuadletAction(
	name: string,
	action: PodmanQuadletAction,
	onDone: () => void | Promise<void>,
	setLoading: (loading: boolean) => void = () => {}
) {
	setLoading(true);
	await handleApiResultWithCallbacks({
		result: await tryCatch(podmanService.quadletAction(name, action)),
		message: m.podman_action_failed({ action: quadletActionLabels[action](), name }),
		setLoadingState: setLoading,
		onSuccess: async (unit) => {
			toast.success(m.podman_action_success({ action: quadletActionLabels[action](), name }), {
				description: `${unit.name}: ${unit.activeState} (${unit.subState})`
			});
			await onDone();
		}
	});
}

export function confirmRemoveQuadlet(name: string, onDone: () => void | Promise<void>) {
	openConfirmDialog({
		title: m.common_remove_title({ resource: m.podman_quadlet() }),
		message: m.common_remove_confirm({ resource: name }),
		confirm: {
			label: m.common_remove(),
			destructive: true,
			action: async () => {
				await handleApiResultWithCallbacks({
					result: await tryCatch(podmanService.removeQuadlet(name)),
					message: m.common_remove_failed({ resource: name }),
					onSuccess: async () => {
						toast.success(m.common_remove_success({ resource: name }));
						await onDone();
					}
				});
			}
		}
	});
}

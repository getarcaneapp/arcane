import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { containerService } from '#lib/services/container-service.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
import type { AutoUpdateResult } from '#lib/types/automation.js';
import { toast } from 'svelte-sonner';

type ContainerLifecycleAction = 'start' | 'stop' | 'restart' | 'pause' | 'unpause';
type ContainerLifecycleStatus = 'starting' | 'stopping' | 'restarting' | 'pausing' | 'unpausing' | '';
type ContainerRemoveStatus = 'removing' | '';

type ContainerLifecycleActionConfig = {
	status: Exclude<ContainerLifecycleStatus, ''>;
	run: (id: string) => Promise<unknown>;
	success: () => string;
	failure: () => string;
};

const containerLifecycleActionConfigs: Record<ContainerLifecycleAction, ContainerLifecycleActionConfig> = {
	start: {
		status: 'starting',
		run: (id) => containerService.startContainer(id),
		success: () => m.containers_start_success(),
		failure: () => m.containers_start_failed()
	},
	stop: {
		status: 'stopping',
		run: (id) => containerService.stopContainer(id),
		success: () => m.containers_stop_success(),
		failure: () => m.containers_stop_failed()
	},
	restart: {
		status: 'restarting',
		run: (id) => containerService.restartContainer(id),
		success: () => m.containers_restart_success(),
		failure: () => m.containers_restart_failed()
	},
	pause: {
		status: 'pausing',
		run: (id) => containerService.pauseContainer(id),
		success: () => m.containers_pause_success(),
		failure: () => m.containers_pause_failed()
	},
	unpause: {
		status: 'unpausing',
		run: (id) => containerService.unpauseContainer(id),
		success: () => m.containers_unpause_success(),
		failure: () => m.containers_unpause_failed()
	}
};

type RunContainerLifecycleActionOptions = {
	action: ContainerLifecycleAction;
	containerId: string;
	setStatus: (status: ContainerLifecycleStatus) => void;
	onRefresh?: () => Promise<unknown> | unknown;
};

export async function runContainerLifecycleAction({
	action,
	containerId,
	setStatus,
	onRefresh
}: RunContainerLifecycleActionOptions) {
	if (!containerId) return;

	const config = containerLifecycleActionConfigs[action];
	setStatus(config.status);

	const operationResult = await tryCatch(
		(async () => {
			await handleApiResultWithCallbacks({
				result: await tryCatch(config.run(containerId)),
				message: config.failure(),
				setLoadingState: (value) => {
					setStatus(value ? config.status : '');
				},
				async onSuccess(data) {
					toast.success(config.success(), activityToastOptions(extractActivityId(data)));
					await onRefresh?.();
				}
			});
		})()
	);
	if (operationResult.error !== null) {
		const error = operationResult.error;
		console.error('Container action failed:', error);
		toast.error(m.containers_action_error());
		setStatus('');
	}
}

type ConfirmAndRemoveContainerOptions = {
	containerId: string;
	containerName: string;
	setStatus: (status: ContainerRemoveStatus) => void;
	onRefresh?: () => Promise<unknown> | unknown;
};

export function confirmAndRemoveContainer({
	containerId,
	containerName,
	setStatus,
	onRefresh
}: ConfirmAndRemoveContainerOptions) {
	openConfirmDialog({
		title: m.containers_remove_confirm_title(),
		message: m.containers_remove_confirm_message({ resource: containerName }),
		checkboxes: [
			{ id: 'force', label: m.containers_remove_force_label(), initialState: false },
			{ id: 'volumes', label: m.containers_remove_volumes_label(), initialState: false }
		],
		confirm: {
			label: m.common_remove(),
			destructive: true,
			action: async (checkboxStates) => {
				const force = !!checkboxStates['force'];
				const volumes = !!checkboxStates['volumes'];
				setStatus('removing');
				await handleApiResultWithCallbacks({
					result: await tryCatch(containerService.deleteContainer(containerId, { force, volumes })),
					message: m.containers_remove_failed(),
					setLoadingState: (value) => {
						setStatus(value ? 'removing' : '');
					},
					async onSuccess(data) {
						toast.success(m.containers_remove_success(), activityToastOptions(extractActivityId(data)));
						await onRefresh?.();
					}
				});
			}
		}
	});
}

type ConfirmAndUpdateContainerOptions = {
	containerId: string;
	containerName: string;
	showPullingToast?: boolean;
	useActivityToast?: boolean;
	setLoading?: (loading: boolean) => void;
	onRefresh?: () => Promise<unknown> | unknown;
};

export function confirmAndUpdateContainer({
	containerId,
	containerName,
	showPullingToast = false,
	useActivityToast = false,
	setLoading,
	onRefresh
}: ConfirmAndUpdateContainerOptions) {
	openConfirmDialog({
		title: m.update_container(),
		message: m.containers_update_confirm_message({ name: containerName }),
		confirm: {
			label: m.update_container(),
			destructive: false,
			action: async () => {
				setLoading?.(true);
				if (showPullingToast) {
					toast.info(m.containers_update_pulling_image());
				}

				const operationResult = await tryCatch(
					handleApiResultWithCallbacks({
						result: await tryCatch(containerService.updateContainer(containerId)),
						message: m.containers_update_failed({ name: containerName }),
						setLoadingState: (value) => setLoading?.(value),
						async onSuccess(result) {
							showContainerUpdateResultToast(result, containerName, useActivityToast);
							await onRefresh?.();
						}
					})
				);
				if (operationResult.error !== null) {
					const error = operationResult.error;
					console.error('Container update failed:', error);
					toast.error(m.containers_update_failed({ name: containerName }));
					setLoading?.(false);
				}
			}
		}
	});
}

/**
 * A single-container update answers 200 with a one-item result, so the outcome
 * lives in the tally: failed, updated (or restarted), skipped with the engine's
 * reason, or nothing to do. A skip is not "up to date" — the engine did not
 * apply anything, and its reason says why.
 */
function showContainerUpdateResultToast(result: AutoUpdateResult, containerName: string, useActivityToast: boolean) {
	const toastOptions = useActivityToast ? activityToastOptions(extractActivityId(result)) : undefined;
	const itemWithStatus = (status: AutoUpdateResult['items'][number]['status']) =>
		result.items?.find((item) => item.status === status);

	if ((result.failed ?? 0) > 0) {
		toast.error(m.containers_update_failed({ name: containerName }), {
			...toastOptions,
			description: itemWithStatus('failed')?.error
		});
		return;
	}
	if ((result.updated ?? 0) + (result.restarted ?? 0) > 0) {
		toast.success(m.containers_update_success({ name: containerName }), toastOptions);
		return;
	}
	const skipped = itemWithStatus('skipped');
	if (skipped) {
		toast.info(m.containers_update_skipped({ name: containerName }), {
			...toastOptions,
			description: skipped.error || m.containers_update_skipped_no_reason()
		});
		return;
	}
	toast.info(m.image_update_up_to_date_title(), toastOptions);
}

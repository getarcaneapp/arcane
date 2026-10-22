import { toast } from 'svelte-sonner';

import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { PERMISSIONS } from '#lib/permissions.generated.js';
import { containerService } from '#lib/services/container-service.js';
import { activityStore } from '#lib/stores/activity.store.svelte.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { Activity } from '#lib/types/activity.type.js';
import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { hasPermission } from '#lib/utils/auth.js';
import { tryCatch } from '#lib/utils/try-catch.js';

type ContainerLifecycleAction = 'start' | 'stop' | 'restart' | 'pause' | 'unpause';
type ContainerLifecycleStatus = 'starting' | 'stopping' | 'restarting' | 'pausing' | 'unpausing' | '';
type ContainerRemoveStatus = 'removing' | '';

type ContainerLifecycleActionConfig = {
	status: Exclude<ContainerLifecycleStatus, ''>;
	run: (id: string, environmentId?: string) => Promise<unknown>;
	success: () => string;
	failure: () => string;
};

const containerLifecycleActionConfigs: Record<ContainerLifecycleAction, ContainerLifecycleActionConfig> = {
	start: {
		status: 'starting',
		run: (id, environmentId) => containerService.startContainer(id, environmentId),
		success: () => m.containers_start_success(),
		failure: () => m.containers_start_failed()
	},
	stop: {
		status: 'stopping',
		run: (id, environmentId) => containerService.stopContainer(id, environmentId),
		success: () => m.containers_stop_success(),
		failure: () => m.containers_stop_failed()
	},
	restart: {
		status: 'restarting',
		run: (id, environmentId) => containerService.restartContainer(id, environmentId),
		success: () => m.containers_restart_success(),
		failure: () => m.containers_restart_failed()
	},
	pause: {
		status: 'pausing',
		run: (id, environmentId) => containerService.pauseContainer(id, environmentId),
		success: () => m.containers_pause_success(),
		failure: () => m.containers_pause_failed()
	},
	unpause: {
		status: 'unpausing',
		run: (id, environmentId) => containerService.unpauseContainer(id, environmentId),
		success: () => m.containers_unpause_success(),
		failure: () => m.containers_unpause_failed()
	}
};

type RunContainerLifecycleActionOptions = {
	action: ContainerLifecycleAction;
	containerId: string;
	environmentId?: string;
	setStatus: (status: ContainerLifecycleStatus) => void;
	onRefresh?: () => Promise<unknown> | unknown;
};

export async function runContainerLifecycleAction({
	action,
	containerId,
	environmentId,
	setStatus,
	onRefresh
}: RunContainerLifecycleActionOptions) {
	if (!containerId) return;

	const config = containerLifecycleActionConfigs[action];
	setStatus(config.status);

	const operationResult = await tryCatch(
		(async () => {
			await handleApiResultWithCallbacks({
				result: await tryCatch(config.run(containerId, environmentId)),
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
	environmentId?: string;
	setStatus: (status: ContainerRemoveStatus) => void;
	onRefresh?: () => Promise<unknown> | unknown;
};

export function confirmAndRemoveContainer({
	containerId,
	containerName,
	environmentId,
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
					result: await tryCatch(containerService.deleteContainer(containerId, { force, volumes, environmentId })),
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
	environmentId: string;
	setLoading?: (loading: boolean) => void;
	onRefresh?: () => Promise<unknown> | unknown;
	onAccepted?: (activity: Activity, environmentId: string) => void | Promise<void>;
};

export function confirmAndUpdateContainer({
	containerId,
	containerName,
	environmentId,
	setLoading,
	onRefresh,
	onAccepted
}: ConfirmAndUpdateContainerOptions) {
	openConfirmDialog({
		title: m.update_container(),
		message: m.containers_update_confirm_message({ name: containerName }),
		confirm: {
			label: m.update_container(),
			destructive: false,
			action: async () => {
				setLoading?.(true);
				await handleApiResultWithCallbacks({
					result: await tryCatch(containerService.updateContainer(containerId, environmentId)),
					message: m.containers_update_accept_failed({ name: containerName }),
					setLoadingState: (value) => setLoading?.(value),
					async onSuccess(activity) {
						const canReadActivity = hasPermission(PERMISSIONS.ACTIVITIES_READ, environmentId);
						if (canReadActivity) activityStore.acceptActivity(activity);
						toast.info(
							m.containers_update_accepted({ name: containerName }),
							canReadActivity ? activityToastOptions(activity.id, false, environmentId) : undefined
						);
						await onAccepted?.(activity, environmentId);
						if (!canReadActivity && environmentStore.shown.some((env) => env.id === environmentId)) await onRefresh?.();
					}
				});
			}
		}
	});
}

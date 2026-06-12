import { openConfirmDialog } from '$lib/components/confirm-dialog';
import { m } from '$lib/paraglide/messages';
import { containerService } from '$lib/services/container-service';
import { handleApiResultWithCallbacks, tryCatch } from '$lib/utils/api';
import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
import { toast } from 'svelte-sonner';

type ContainerLifecycleAction = 'start' | 'stop' | 'restart';
type ContainerLifecycleStatus = 'starting' | 'stopping' | 'restarting' | '';
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

	try {
		handleApiResultWithCallbacks({
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
	} catch (error) {
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
				handleApiResultWithCallbacks({
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

type ContainerUpdateResultItem = {
	status?: string;
	error?: string;
};

type ContainerUpdateResult = {
	failed?: number;
	updated?: number;
	items?: ContainerUpdateResultItem[];
};

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
		title: m.containers_update_confirm_title(),
		message: m.containers_update_confirm_message({ name: containerName }),
		confirm: {
			label: m.containers_update_container(),
			destructive: false,
			action: async () => {
				setLoading?.(true);
				try {
					if (showPullingToast) {
						toast.info(m.containers_update_pulling_image());
					}

					const result = (await containerService.updateContainer(containerId)) as ContainerUpdateResult;
					const toastOptions = useActivityToast ? activityToastOptions(extractActivityId(result)) : undefined;

					if ((result.failed ?? 0) > 0) {
						const failedItem = result.items?.find((item) => item.status === 'failed');
						toast.error(
							m.containers_update_failed({ name: containerName }) + (failedItem?.error ? `: ${failedItem.error}` : ''),
							toastOptions
						);
					} else if ((result.updated ?? 0) > 0) {
						toast.success(m.containers_update_success({ name: containerName }), toastOptions);
					} else {
						toast.info(m.image_update_up_to_date_title(), toastOptions);
					}

					await onRefresh?.();
				} catch (error) {
					console.error('Container update failed:', error);
					toast.error(m.containers_update_failed({ name: containerName }));
				} finally {
					setLoading?.(false);
				}
			}
		}
	});
}

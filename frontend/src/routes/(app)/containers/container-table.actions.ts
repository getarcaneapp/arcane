import { openConfirmDialog } from '$lib/components/confirm-dialog';
import { m } from '$lib/paraglide/messages';
import { containerService, type ContainersPaginatedResponse } from '$lib/services/container-service';
import type { ContainerSummaryDto } from '$lib/types/docker';
import { handleApiResultWithCallbacks } from '$lib/utils/api';
import { tryCatch } from '$lib/utils/api';
import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
import { bulkConfirmAndRun } from '$lib/utils/bulk-actions';
import { confirmAndRemoveContainer, confirmAndUpdateContainer, runContainerLifecycleAction } from '$lib/utils/container-actions';
import type { TableActionConfig, TableBulkActionConfig } from '$lib/utils/table-action-types';
import { toast } from 'svelte-sonner';
import { getContainerDisplayName, type ActionStatus } from './container-table.helpers';

type BulkLoadingState = {
	start: boolean;
	stop: boolean;
	restart: boolean;
	remove: boolean;
};

type ActionDeps = {
	setContainers: (next: ContainersPaginatedResponse) => void;
	setSelectedIds: (next: string[]) => void;
	refreshContainers: () => Promise<ContainersPaginatedResponse>;
	actionStatus: Record<string, ActionStatus>;
	isBulkLoading: BulkLoadingState;
};

type ContainerActionKind = 'start' | 'stop' | 'restart' | 'redeploy';

type ContainerActionConfig = TableActionConfig<ActionStatus>;
type BulkActionConfig = TableBulkActionConfig<keyof BulkLoadingState>;

const containerActionConfigs: Record<Exclude<ContainerActionKind, 'start' | 'stop' | 'restart'>, ContainerActionConfig> = {
	redeploy: {
		status: 'redeploying',
		run: (id) => containerService.redeployContainer(id),
		success: () => m.container_redeploy_success(),
		failure: () => m.container_redeploy_failed()
	}
};

export function createContainerActions({
	setContainers,
	setSelectedIds,
	refreshContainers,
	actionStatus,
	isBulkLoading
}: ActionDeps) {
	const reloadContainers = async () => {
		const result = await refreshContainers();
		setContainers(result);
		return result;
	};

	async function performContainerAction(action: ContainerActionKind, id: string) {
		if (action === 'start' || action === 'stop' || action === 'restart') {
			await runContainerLifecycleAction({
				action,
				containerId: id,
				setStatus: (status) => {
					actionStatus[id] = status;
				},
				onRefresh: reloadContainers
			});
			return;
		}

		const config = containerActionConfigs[action];
		actionStatus[id] = config.status;

		try {
			handleApiResultWithCallbacks({
				result: await tryCatch(config.run(id)),
				message: config.failure(),
				setLoadingState: (value) => {
					actionStatus[id] = value ? config.status : '';
				},
				async onSuccess(data) {
					toast.success(config.success(), activityToastOptions(extractActivityId(data)));
					await reloadContainers();
				}
			});
		} catch (error) {
			console.error('Container action failed:', error);
			toast.error(m.containers_action_error());
			actionStatus[id] = '';
		}
	}

	async function handleRemoveContainer(id: string, name: string) {
		confirmAndRemoveContainer({
			containerId: id,
			containerName: name,
			setStatus: (status) => {
				actionStatus[id] = status;
			},
			onRefresh: reloadContainers
		});
	}

	async function handleUpdateContainer(container: ContainerSummaryDto) {
		const containerName = getContainerDisplayName(container);

		confirmAndUpdateContainer({
			containerId: container.id,
			containerName,
			useActivityToast: true,
			setLoading: (loading) => {
				actionStatus[container.id] = loading ? 'updating' : '';
			},
			onRefresh: reloadContainers
		});
	}

	async function handleRedeployContainer(container: ContainerSummaryDto) {
		openConfirmDialog({
			title: m.container_confirm_redeploy_title(),
			message: m.container_confirm_redeploy_message(),
			confirm: {
				label: m.common_redeploy(),
				destructive: false,
				action: async () => {
					actionStatus[container.id] = 'redeploying';
					handleApiResultWithCallbacks({
						result: await tryCatch(containerService.redeployContainer(container.id)),
						message: m.container_redeploy_failed(),
						setLoadingState: (value) => {
							actionStatus[container.id] = value ? 'redeploying' : '';
						},
						async onSuccess(data) {
							toast.success(m.container_redeploy_success(), activityToastOptions(extractActivityId(data)));
							await refreshContainers();
						}
					});
				}
			}
		});
	}

	function runBulkAction(ids: string[], config: BulkActionConfig) {
		bulkConfirmAndRun({
			ids,
			title: config.title(ids.length),
			message: config.message(ids.length),
			confirmLabel: config.label,
			destructive: config.destructive ?? false,
			run: (id) => config.run(id),
			messages: {
				success: config.success,
				partial: config.partial,
				failure: config.failure
			},
			setLoading: (loading) => {
				isBulkLoading[config.loadingKey] = loading;
			},
			onComplete: () => reloadContainers(),
			clearSelection: () => setSelectedIds([])
		});
	}

	async function handleBulkStart(ids: string[]) {
		await runBulkAction(ids, {
			title: (count) => m.containers_bulk_start_confirm_title({ count }),
			message: (count) => m.containers_bulk_start_confirm_message({ count }),
			label: m.common_start(),
			loadingKey: 'start',
			run: (id) => containerService.startContainer(id),
			success: (count) => m.containers_bulk_start_success({ count }),
			partial: (success, total, failed) => m.containers_bulk_start_partial({ success, total, failed }),
			failure: () => m.containers_start_failed()
		});
	}

	async function handleBulkStop(ids: string[]) {
		await runBulkAction(ids, {
			title: (count) => m.containers_bulk_stop_confirm_title({ count }),
			message: (count) => m.containers_bulk_stop_confirm_message({ count }),
			label: m.common_stop(),
			loadingKey: 'stop',
			run: (id) => containerService.stopContainer(id),
			success: (count) => m.containers_bulk_stop_success({ count }),
			partial: (success, total, failed) => m.containers_bulk_stop_partial({ success, total, failed }),
			failure: () => m.containers_stop_failed()
		});
	}

	async function handleBulkRestart(ids: string[]) {
		await runBulkAction(ids, {
			title: (count) => m.containers_bulk_restart_confirm_title({ count }),
			message: (count) => m.containers_bulk_restart_confirm_message({ count }),
			label: m.common_restart(),
			loadingKey: 'restart',
			run: (id) => containerService.restartContainer(id),
			success: (count) => m.containers_bulk_restart_success({ count }),
			partial: (success, total, failed) => m.containers_bulk_restart_partial({ success, total, failed }),
			failure: () => m.containers_restart_failed()
		});
	}

	function handleBulkRemove(ids: string[]) {
		bulkConfirmAndRun({
			ids,
			title: m.containers_bulk_remove_confirm_title({ count: ids.length }),
			message: m.containers_bulk_remove_confirm_message({ count: ids.length }),
			confirmLabel: m.common_remove(),
			destructive: true,
			checkboxes: [
				{ id: 'force', label: m.containers_remove_force_label(), initialState: false },
				{ id: 'volumes', label: m.containers_remove_volumes_label(), initialState: false }
			],
			run: (id, checkboxStates) =>
				containerService.deleteContainer(id, { force: !!checkboxStates['force'], volumes: !!checkboxStates['volumes'] }),
			messages: {
				success: (count) => m.containers_bulk_remove_success({ count }),
				partial: (success, total, failed) => m.containers_bulk_remove_partial({ success, total, failed }),
				failure: () => m.containers_remove_failed()
			},
			setLoading: (loading) => {
				isBulkLoading.remove = loading;
			},
			onComplete: () => reloadContainers(),
			clearSelection: () => setSelectedIds([])
		});
	}

	return {
		performContainerAction,
		handleRemoveContainer,
		handleUpdateContainer,
		handleRedeployContainer,
		handleBulkStart,
		handleBulkStop,
		handleBulkRestart,
		handleBulkRemove
	};
}

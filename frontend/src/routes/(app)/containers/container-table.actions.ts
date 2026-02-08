import { openConfirmDialog } from '$lib/components/confirm-dialog';
import { m } from '$lib/paraglide/messages';
import { containerService } from '$lib/services/container-service';
import type { ContainerSummaryDto } from '$lib/types/container.type';
import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
import { tryCatch } from '$lib/utils/try-catch';
import { toast } from 'svelte-sonner';
import { getContainerDisplayName, type ActionStatus } from './container-table.helpers';

type BulkLoadingState = {
	start: boolean;
	stop: boolean;
	restart: boolean;
	remove: boolean;
};

type ActionDeps = {
	getRequestOptions: () => SearchPaginationSortRequest;
	setContainers: (next: Paginated<ContainerSummaryDto>) => void;
	setSelectedIds: (next: string[]) => void;
	actionStatus: Record<string, ActionStatus>;
	isBulkLoading: BulkLoadingState;
};

type ContainerActionKind = 'start' | 'stop' | 'restart';

type ContainerActionConfig = {
	status: ActionStatus;
	run: (id: string) => Promise<unknown>;
	success: () => string;
	failure: () => string;
};

type BulkActionConfig = {
	title: (count: number) => string;
	message: (count: number) => string;
	label: string;
	loadingKey: keyof BulkLoadingState;
	run: (id: string) => Promise<unknown>;
	success: (count: number) => string;
	partial: (success: number, total: number, failed: number) => string;
	failure: () => string;
	destructive?: boolean;
};

const containerActionConfigs: Record<ContainerActionKind, ContainerActionConfig> = {
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

export function createContainerActions({
	getRequestOptions,
	setContainers,
	setSelectedIds,
	actionStatus,
	isBulkLoading
}: ActionDeps) {
	const refreshContainers = async () => {
		const result = await containerService.getContainers(getRequestOptions());
		setContainers(result);
		return result;
	};

	async function performContainerAction(action: ContainerActionKind, id: string) {
		const config = containerActionConfigs[action];
		actionStatus[id] = config.status;

		try {
			handleApiResultWithCallbacks({
				result: await tryCatch(config.run(id)),
				message: config.failure(),
				setLoadingState: (value) => {
					actionStatus[id] = value ? config.status : '';
				},
				async onSuccess() {
					toast.success(config.success());
					await refreshContainers();
				}
			});
		} catch (error) {
			console.error('Container action failed:', error);
			toast.error(m.containers_action_error());
			actionStatus[id] = '';
		}
	}

	async function handleRemoveContainer(id: string, name: string) {
		openConfirmDialog({
			title: m.containers_remove_confirm_title(),
			message: m.containers_remove_confirm_message({ resource: name }),
			checkboxes: [
				{
					id: 'force',
					label: m.containers_remove_force_label(),
					initialState: false
				},
				{
					id: 'volumes',
					label: m.containers_remove_volumes_label(),
					initialState: false
				}
			],
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async (checkboxStates) => {
					const force = !!checkboxStates.force;
					const volumes = !!checkboxStates.volumes;
					actionStatus[id] = 'removing';
					handleApiResultWithCallbacks({
						result: await tryCatch(containerService.deleteContainer(id, { force, volumes })),
						message: m.containers_remove_failed(),
						setLoadingState: (value) => {
							actionStatus[id] = value ? 'removing' : '';
						},
						async onSuccess() {
							toast.success(m.containers_remove_success());
							await refreshContainers();
						}
					});
				}
			}
		});
	}

	async function handleUpdateContainer(container: ContainerSummaryDto) {
		const containerName = getContainerDisplayName(container);

		openConfirmDialog({
			title: m.containers_update_confirm_title(),
			message: m.containers_update_confirm_message({ name: containerName }),
			confirm: {
				label: m.containers_update_container(),
				destructive: false,
				action: async () => {
					actionStatus[container.id] = 'updating';
					try {
						toast.info(m.containers_update_pulling_image());

						const result = await containerService.updateContainer(container.id);

						if (result.failed > 0) {
							const failedItem = result.items?.find((item: { status?: string; error?: string }) => item.status === 'failed');
							toast.error(
								m.containers_update_failed({ name: containerName }) + (failedItem?.error ? `: ${failedItem.error}` : '')
							);
						} else if (result.updated > 0) {
							toast.success(m.containers_update_success({ name: containerName }));
						} else {
							toast.info(m.image_update_up_to_date_title());
						}

						await refreshContainers();
					} catch (error) {
						console.error('Container update failed:', error);
						toast.error(m.containers_update_failed({ name: containerName }));
					} finally {
						actionStatus[container.id] = '';
					}
				}
			}
		});
	}

	async function runBulkAction(ids: string[], config: BulkActionConfig) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: config.title(ids.length),
			message: config.message(ids.length),
			confirm: {
				label: config.label,
				destructive: config.destructive ?? false,
				action: async () => {
					isBulkLoading[config.loadingKey] = true;

					const results = await Promise.allSettled(ids.map((id) => config.run(id)));

					const successCount = results.filter((result) => result.status === 'fulfilled').length;
					const failureCount = results.length - successCount;

					isBulkLoading[config.loadingKey] = false;

					if (successCount === ids.length) {
						toast.success(config.success(successCount));
					} else if (successCount > 0) {
						toast.warning(config.partial(successCount, ids.length, failureCount));
					} else {
						toast.error(config.failure());
					}

					await refreshContainers();
					setSelectedIds([]);
				}
			}
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

	async function handleBulkRemove(ids: string[]) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: m.containers_bulk_remove_confirm_title({ count: ids.length }),
			message: m.containers_bulk_remove_confirm_message({ count: ids.length }),
			checkboxes: [
				{
					id: 'force',
					label: m.containers_remove_force_label(),
					initialState: false
				},
				{
					id: 'volumes',
					label: m.containers_remove_volumes_label(),
					initialState: false
				}
			],
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async (checkboxStates) => {
					const force = !!checkboxStates.force;
					const volumes = !!checkboxStates.volumes;
					isBulkLoading.remove = true;

					const results = await Promise.allSettled(ids.map((id) => containerService.deleteContainer(id, { force, volumes })));

					const successCount = results.filter((result) => result.status === 'fulfilled').length;
					const failureCount = results.length - successCount;

					isBulkLoading.remove = false;

					if (successCount === ids.length) {
						toast.success(m.containers_bulk_remove_success({ count: successCount }));
					} else if (successCount > 0) {
						toast.warning(m.containers_bulk_remove_partial({ success: successCount, total: ids.length, failed: failureCount }));
					} else {
						toast.error(m.containers_remove_failed());
					}

					await refreshContainers();
					setSelectedIds([]);
				}
			}
		});
	}

	return {
		performContainerAction,
		handleRemoveContainer,
		handleUpdateContainer,
		handleBulkStart,
		handleBulkStop,
		handleBulkRestart,
		handleBulkRemove
	};
}

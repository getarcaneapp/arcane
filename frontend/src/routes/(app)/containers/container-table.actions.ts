import { toast } from 'svelte-sonner';

// fallow-ignore-file code-duplication -- project and global tables already delegate removal to confirmAndRemoveContainer; only state callbacks differ
import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { containerService, type ContainersPaginatedResponse } from '#lib/services/container-service.js';
import { environmentStore } from '#lib/stores/environment.store.svelte.js';
import type { Activity } from '#lib/types/activity.type.js';
import type { ContainerSummaryDto } from '#lib/types/docker.js';
import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { hasPermission } from '#lib/utils/auth.js';
import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';
import {
	confirmAndRemoveContainer,
	confirmAndUpdateContainer,
	runContainerLifecycleAction
} from '#lib/utils/container-actions.js';
import { hasShownPermission, rowKey, type EnvironmentPage, type EnvironmentRow } from '#lib/utils/environments.js';
import type { TableActionConfig, TableBulkActionConfig } from '#lib/utils/table-action-types.js';
import { tryCatch } from '#lib/utils/try-catch.js';

import { getContainerDisplayName, type ActionStatus } from './container-table.helpers';

type BulkLoadingState = {
	start: boolean;
	stop: boolean;
	restart: boolean;
	remove: boolean;
	update: boolean;
};

type ActionDeps = {
	setContainers: (next: ContainerPage) => void;
	setSelectedIds: (next: string[]) => void;
	refreshContainers: () => Promise<ContainerPage>;
	actionStatus: Record<string, ActionStatus>;
	isBulkLoading: BulkLoadingState;
	/** Shown-environments scope the table rendered; compared only, never sent to the API. */
	scope: () => string;
	onUpdateAccepted: (activity: Activity) => void;
	canRefreshUpdates: () => boolean;
};

type ContainerRow = EnvironmentRow<ContainerSummaryDto>;
type ContainerPage = EnvironmentPage<ContainersPaginatedResponse>;

type ContainerActionKind = 'start' | 'stop' | 'restart' | 'pause' | 'unpause' | 'redeploy';

type ContainerActionConfig = TableActionConfig<ActionStatus>;
type BulkActionConfig = TableBulkActionConfig<keyof BulkLoadingState>;

const containerActionConfigs: Record<
	Exclude<ContainerActionKind, 'start' | 'stop' | 'restart' | 'pause' | 'unpause'>,
	ContainerActionConfig
> = {
	redeploy: {
		status: 'redeploying',
		run: (id, environmentId) => containerService.redeployContainer(id, environmentId),
		success: () => m.container_redeploy_success(),
		failure: () => m.container_redeploy_failed()
	}
};

export function createContainerActions({
	setContainers,
	setSelectedIds,
	refreshContainers,
	actionStatus,
	isBulkLoading,
	scope,
	onUpdateAccepted,
	canRefreshUpdates
}: ActionDeps) {
	const reloadContainers = async () => {
		const result = await refreshContainers();
		setContainers(result);
		return result;
	};

	async function performContainerAction(action: ContainerActionKind, id: string, environmentId: string) {
		const key = rowKey({ id, environmentId, environmentTagged: true });
		if (action === 'start' || action === 'stop' || action === 'restart' || action === 'pause' || action === 'unpause') {
			await runContainerLifecycleAction({
				action,
				containerId: id,
				environmentId,
				setStatus: (status) => {
					actionStatus[key] = status;
				},
				onRefresh: reloadContainers
			});
			return;
		}

		const config = containerActionConfigs[action];
		actionStatus[key] = config.status;

		const operationResult = await tryCatch(
			(async () => {
				await handleApiResultWithCallbacks({
					result: await tryCatch(config.run(id, environmentId)),
					message: config.failure(),
					setLoadingState: (value) => {
						actionStatus[key] = value ? config.status : '';
					},
					async onSuccess(data) {
						toast.success(config.success(), activityToastOptions(extractActivityId(data)));
						await reloadContainers();
					}
				});
			})()
		);
		if (operationResult.error !== null) {
			const error = operationResult.error;

			console.error('Container action failed:', error);
			toast.error(m.containers_action_error());
			actionStatus[key] = '';
		}
	}

	async function handleRemoveContainer(id: string, name: string, environmentId: string) {
		const key = rowKey({ id, environmentId, environmentTagged: true });
		confirmAndRemoveContainer({
			containerId: id,
			containerName: name,
			environmentId,
			setStatus: (status) => {
				actionStatus[key] = status;
			},
			onRefresh: reloadContainers
		});
	}

	async function handleUpdateContainer(container: ContainerRow) {
		const key = rowKey(container);
		const containerName = getContainerDisplayName(container);

		confirmAndUpdateContainer({
			containerId: container.id,
			containerName,
			environmentId: container.environmentId,
			setLoading: (loading) => {
				actionStatus[key] = loading ? 'updating' : '';
			},
			onRefresh: () => (canRefreshUpdates() ? reloadContainers() : undefined),
			onAccepted: onUpdateAccepted
		});
	}

	async function handleRedeployContainer(container: ContainerRow) {
		const key = rowKey(container);
		openConfirmDialog({
			title: m.container_confirm_redeploy_title(),
			message: m.container_confirm_redeploy_message(),
			confirm: {
				label: m.common_redeploy(),
				destructive: false,
				action: async () => {
					actionStatus[key] = 'redeploying';
					await handleApiResultWithCallbacks({
						result: await tryCatch(containerService.redeployContainer(container.id, container.environmentId)),
						message: m.container_redeploy_failed(),
						setLoadingState: (value) => {
							actionStatus[key] = value ? 'redeploying' : '';
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
			runInEnvironment: (id, environmentId) => config.run(id, environmentId),
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
			run: (id, environmentId) => containerService.startContainer(id, environmentId),
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
			run: (id, environmentId) => containerService.stopContainer(id, environmentId),
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
			run: (id, environmentId) => containerService.restartContainer(id, environmentId),
			success: (count) => m.containers_bulk_restart_success({ count }),
			partial: (success, total, failed) => m.containers_bulk_restart_partial({ success, total, failed }),
			failure: () => m.containers_restart_failed()
		});
	}

	function handleBulkUpdate(validIds: string[], allIds: string[]) {
		const updateScope = scope();
		const tracksActivities = environmentStore.shown.every((env) => hasPermission('activities:read', env.id));
		const totalCount = allIds.length;
		const filteredCount = validIds.length;
		const message =
			validIds?.length < allIds?.length
				? m.containers_bulk_update_filtered_confirm_message({ filteredCount, totalCount })
				: m.containers_bulk_update_confirm_message({ count: filteredCount });

		bulkConfirmAndRun({
			ids: validIds,
			title: m.containers_bulk_update_confirm_title({ count: filteredCount }),
			message,
			confirmLabel: m.common_update(),
			destructive: false,
			runInEnvironment: (id, environmentId) => containerService.updateContainer(id, environmentId),
			messages: {
				success: (count) => m.containers_bulk_update_accepted({ count }),
				partial: (success, total, failed) => m.containers_bulk_update_accept_partial({ success, total, failed }),
				failure: () => m.containers_bulk_update_accept_failed()
			},
			acceptanceOnly: true,
			activityLink: hasShownPermission('activities:read'),
			onItemSuccess: (_id, activity) => onUpdateAccepted(activity),
			setLoading: (loading) => {
				isBulkLoading['update'] = loading;
			},
			onComplete: () => (canRefreshUpdates() && updateScope === scope() && !tracksActivities ? reloadContainers() : undefined),
			clearSelection: () => setSelectedIds([])
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
			runInEnvironment: (id, environmentId, checkboxStates) =>
				containerService.deleteContainer(id, {
					force: !!checkboxStates['force'],
					volumes: !!checkboxStates['volumes'],
					environmentId
				}),
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
		handleBulkRemove,
		handleBulkUpdate
	};
}

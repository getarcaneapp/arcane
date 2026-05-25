import { openConfirmDialog } from '$lib/components/confirm-dialog';
import { m } from '$lib/paraglide/messages';
import { deployOptionsStore } from '$lib/stores/deploy-options.store.svelte';
import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
import { projectService } from '$lib/services/project-service';
import type { SearchPaginationSortRequest } from '$lib/types/shared';
import { handleApiResultWithCallbacks } from '$lib/utils/api';
import { tryCatch } from '$lib/utils/api';
import { toast } from 'svelte-sonner';
import type { ActionStatus } from './projects-table.helpers';

type BulkLoadingState = {
	up: boolean;
	down: boolean;
	redeploy: boolean;
	archive: boolean;
};

type ActionDeps = {
	getRequestOptions: () => SearchPaginationSortRequest;
	refreshProjects: (options?: SearchPaginationSortRequest) => Promise<void>;
	setSelectedIds: (next: string[]) => void;
	actionStatus: Record<string, ActionStatus>;
	isBulkLoading: BulkLoadingState;
	getEnvId: () => string | undefined;
};

type ProjectActionKind = 'start' | 'stop' | 'restart' | 'redeploy' | 'archive' | 'unarchive';

type ProjectActionConfig = {
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

type DestroyConfirmResult = {
	checkboxes?: {
		volumes?: boolean;
		files?: boolean;
	};
	volumes?: boolean;
	files?: boolean;
};

type ProjectActions = {
	performProjectAction: (action: ProjectActionKind, id: string) => Promise<void>;
	handleDestroyProject: (id: string) => Promise<void>;
	handleSyncFromGit: (projectId: string, gitOpsSyncId: string) => Promise<void>;
	handleBulkUp: (ids: string[]) => Promise<void>;
	handleBulkDown: (ids: string[]) => Promise<void>;
	handleBulkRedeploy: (ids: string[]) => Promise<void>;
	handleBulkArchive: (ids: string[]) => Promise<void>;
};

const projectActionConfigs: Record<ProjectActionKind, ProjectActionConfig> = {
	start: {
		status: 'starting',
		run: (id) => projectService.deployProject(id, deployOptionsStore.getRequestOptions()),
		success: () => m.compose_start_success(),
		failure: () => m.compose_start_failed()
	},
	stop: {
		status: 'stopping',
		run: (id) => projectService.downProject(id),
		success: () => m.compose_stop_success(),
		failure: () => m.compose_stop_failed()
	},
	restart: {
		status: 'restarting',
		run: (id) => projectService.restartProject(id),
		success: () => m.compose_restart_success(),
		failure: () => m.compose_restart_failed()
	},
	redeploy: {
		status: 'redeploying',
		run: (id) => projectService.redeployProject(id),
		success: () => m.compose_pull_success(),
		failure: () => m.compose_pull_failed()
	},
	archive: {
		status: 'archiving',
		run: (id) => projectService.archiveProject(id),
		success: () => m.compose_archive_success(),
		failure: () => m.compose_archive_failed()
	},
	unarchive: {
		status: 'unarchiving',
		run: (id) => projectService.unarchiveProject(id),
		success: () => m.compose_unarchive_success(),
		failure: () => m.compose_unarchive_failed()
	}
};

export function createProjectActions({
	getRequestOptions,
	refreshProjects,
	setSelectedIds,
	actionStatus,
	isBulkLoading,
	getEnvId
}: ActionDeps): ProjectActions {
	async function performProjectAction(action: ProjectActionKind, id: string): Promise<void> {
		const config = projectActionConfigs[action];
		actionStatus[id] = config.status;

		try {
			await handleApiResultWithCallbacks({
				result: await tryCatch(config.run(id)),
				message: config.failure(),
				setLoadingState: (value) => {
					actionStatus[id] = value ? config.status : '';
				},
				onSuccess: async () => {
					toast.success(config.success());
					await refreshProjects();
				}
			});
		} catch (error) {
			toast.error(m.common_action_failed());
			actionStatus[id] = '';
		}
	}

	async function handleDestroyProject(id: string): Promise<void> {
		openConfirmDialog({
			title: m.common_confirm_removal_title(),
			message: m.compose_confirm_removal_message(),
			checkboxes: [
				{
					id: 'volumes',
					label: m.confirm_remove_volumes_warning(),
					initialState: false
				},
				{
					id: 'files',
					label: m.confirm_remove_project_files(),
					initialState: false
				}
			],
			confirm: {
				label: m.compose_destroy(),
				destructive: true,
				action: async (result: DestroyConfirmResult) => {
					const removeVolumes = !!(result?.checkboxes?.volumes ?? result?.volumes);
					const removeFiles = !!(result?.checkboxes?.files ?? result?.files);
					actionStatus[id] = 'destroying';

					await handleApiResultWithCallbacks({
						result: await tryCatch(projectService.destroyProject(id, removeVolumes, removeFiles)),
						message: m.compose_destroy_failed(),
						setLoadingState: (value) => {
							actionStatus[id] = value ? 'destroying' : '';
						},
						onSuccess: async () => {
							toast.success(m.compose_destroy_success());
							await refreshProjects();
						}
					});
				}
			}
		});
	}

	async function handleSyncFromGit(projectId: string, gitOpsSyncId: string): Promise<void> {
		const envId = getEnvId();
		if (!envId) return;

		actionStatus[projectId] = 'syncing';
		const result = await tryCatch(gitOpsSyncService.performSync(envId, gitOpsSyncId));

		await handleApiResultWithCallbacks({
			result,
			message: m.git_sync_failed(),
			setLoadingState: (value) => {
				actionStatus[projectId] = value ? 'syncing' : '';
			},
			onSuccess: async () => {
				toast.success(m.git_sync_success());
				await refreshProjects();
			}
		});
	}

	async function runBulkAction(ids: string[], config: BulkActionConfig): Promise<void> {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: config.title(ids.length),
			message: config.message(ids.length),
			confirm: {
				label: config.label,
				destructive: config.destructive ?? false,
				action: async () => {
					isBulkLoading[config.loadingKey] = true;

					try {
						const results = await Promise.allSettled(ids.map((id) => config.run(id)));

						const successCount = results.filter((result) => result.status === 'fulfilled').length;
						const failureCount = results.length - successCount;

						if (successCount === ids.length) {
							toast.success(config.success(successCount));
						} else if (successCount > 0) {
							toast.warning(config.partial(successCount, ids.length, failureCount));
						} else {
							toast.error(config.failure());
						}

						await refreshProjects(getRequestOptions());
						setSelectedIds([]);
					} finally {
						isBulkLoading[config.loadingKey] = false;
					}
				}
			}
		});
	}

	async function handleBulkUp(ids: string[]): Promise<void> {
		await runBulkAction(ids, {
			title: (count) => m.projects_bulk_up_confirm_title({ count }),
			message: (count) => m.projects_bulk_up_confirm_message({ count }),
			label: m.common_up(),
			loadingKey: 'up',
			run: (id) => projectService.deployProject(id, deployOptionsStore.getRequestOptions()),
			success: (count) => m.projects_bulk_up_success({ count }),
			partial: (success, total, failed) => m.projects_bulk_up_partial({ success, total, failed }),
			failure: () => m.compose_start_failed()
		});
	}

	async function handleBulkDown(ids: string[]): Promise<void> {
		await runBulkAction(ids, {
			title: (count) => m.projects_bulk_down_confirm_title({ count }),
			message: (count) => m.projects_bulk_down_confirm_message({ count }),
			label: m.common_down(),
			loadingKey: 'down',
			run: (id) => projectService.downProject(id),
			success: (count) => m.projects_bulk_down_success({ count }),
			partial: (success, total, failed) => m.projects_bulk_down_partial({ success, total, failed }),
			failure: () => m.compose_stop_failed()
		});
	}

	async function handleBulkRedeploy(ids: string[]): Promise<void> {
		await runBulkAction(ids, {
			title: (count) => m.projects_bulk_redeploy_confirm_title({ count }),
			message: (count) => m.projects_bulk_redeploy_confirm_message({ count }),
			label: m.compose_pull_redeploy(),
			loadingKey: 'redeploy',
			run: (id) => projectService.redeployProject(id),
			success: (count) => m.projects_bulk_redeploy_success({ count }),
			partial: (success, total, failed) => m.projects_bulk_redeploy_partial({ success, total, failed }),
			failure: () => m.compose_pull_failed()
		});
	}

	async function handleBulkArchive(ids: string[]): Promise<void> {
		await runBulkAction(ids, {
			title: (count) => m.projects_bulk_archive_confirm_title({ count }),
			message: (count) => m.projects_bulk_archive_confirm_message({ count }),
			label: m.projects_archive(),
			loadingKey: 'archive',
			run: (id) => projectService.archiveProject(id),
			success: (count) => m.projects_bulk_archive_success({ count }),
			partial: (success, total, failed) => m.projects_bulk_archive_partial({ success, total, failed }),
			failure: () => m.compose_archive_failed()
		});
	}

	return {
		performProjectAction,
		handleDestroyProject,
		handleSyncFromGit,
		handleBulkUp,
		handleBulkDown,
		handleBulkRedeploy,
		handleBulkArchive
	};
}

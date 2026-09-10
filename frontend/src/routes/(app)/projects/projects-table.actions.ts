import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
import { m } from '#lib/paraglide/messages.js';
import { deployOptionsStore } from '#lib/stores/deploy-options.store.svelte.js';
import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
import { projectService } from '#lib/services/project-service.js';
import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
import { tryCatch } from '#lib/utils/try-catch.js';
import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
import type { TableActionConfig, TableBulkActionConfig } from '#lib/utils/table-action-types.js';
import { toast } from 'svelte-sonner';
import type { ActionStatus } from './projects-table.helpers';
import type { Project } from '#lib/types/swarm.js';
import { bulkConfirmAndRun } from '#lib/utils/bulk-actions.js';

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
	getProjects: () => Project[];
};

type ProjectActionKind = 'start' | 'stop' | 'restart' | 'redeploy' | 'archive' | 'unarchive';

type ProjectActionConfig = TableActionConfig<ActionStatus, Project>;
type BulkActionConfig = TableBulkActionConfig<keyof BulkLoadingState, Project>;

type DestroyConfirmResult = {
	checkboxes?: {
		volumes?: boolean;
		files?: boolean;
	};
	volumes?: boolean;
	files?: boolean;
};

type ProjectActions = {
	performProjectAction: (action: ProjectActionKind, project: Project) => Promise<void>;
	handleDestroyProject: (project: Project) => Promise<void>;
	handleSyncFromGit: (project: Project, gitOpsSyncId: string) => Promise<void>;
	handleBulkUp: (ids: string[]) => Promise<void>;
	handleBulkDown: (ids: string[]) => Promise<void>;
	handleBulkRedeploy: (ids: string[]) => Promise<void>;
	handleBulkArchive: (ids: string[]) => Promise<void>;
};

const projectActionConfigs: Record<ProjectActionKind, ProjectActionConfig> = {
	start: {
		status: 'starting',
		run: (project) =>
			projectService.deployProject(project.environmentId, project.id, 'up', deployOptionsStore.takeRequestOptions()),
		success: () => m.compose_start_success(),
		failure: () => m.compose_start_failed()
	},
	stop: {
		status: 'stopping',
		run: (project) => projectService.downProject(project.environmentId, project.id),
		success: () => m.compose_stop_success(),
		failure: () => m.compose_stop_failed()
	},
	restart: {
		status: 'restarting',
		run: (project) => projectService.restartProject(project.environmentId, project.id),
		success: () => m.compose_restart_success(),
		failure: () => m.compose_restart_failed()
	},
	redeploy: {
		status: 'redeploying',
		run: (project) =>
			projectService.deployProject(project.environmentId, project.id, 'redeploy', deployOptionsStore.takeRequestOptions()),
		success: () => m.compose_pull_success(),
		failure: () => m.compose_pull_failed()
	},
	archive: {
		status: 'archiving',
		run: (project) => projectService.archiveProject(project.environmentId, project.id),
		success: () => m.compose_archive_success(),
		failure: () => m.compose_archive_failed()
	},
	unarchive: {
		status: 'unarchiving',
		run: (project) => projectService.unarchiveProject(project.environmentId, project.id),
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
	getProjects
}: ActionDeps): ProjectActions {
	async function performProjectAction(action: ProjectActionKind, project: Project): Promise<void> {
		const { id } = project;
		const config = projectActionConfigs[action];
		actionStatus[id] = config.status;

		const operationResult = await tryCatch(
			(async () => {
				await handleApiResultWithCallbacks({
					result: await tryCatch(config.run(project)),
					message: config.failure(),
					setLoadingState: (value) => {
						actionStatus[id] = value ? config.status : '';
					},
					onSuccess: async (data) => {
						toast.success(config.success(), activityToastOptions(extractActivityId(data)));
						await refreshProjects();
					}
				});
			})()
		);
		if (operationResult.error !== null) {
			toast.error(m.common_action_failed());
			actionStatus[id] = '';
		}
	}

	async function handleDestroyProject(project: Project): Promise<void> {
		const { id } = project;
		openConfirmDialog({
			title: m.common_confirm_removal_title(),
			message: m.compose_confirm_removal_message(),
			checkboxes: [
				{
					id: 'volumes',
					label: m.confirm_remove_volumes_warning(),
					initialState: false
				}
			],
			confirm: {
				label: m.compose_destroy(),
				destructive: true,
				action: async (result: DestroyConfirmResult) => {
					const removeVolumes = !!(result?.checkboxes?.volumes ?? result?.volumes);
					actionStatus[id] = 'destroying';

					await handleApiResultWithCallbacks({
						result: await tryCatch(projectService.destroyProject(project.environmentId, project.id, removeVolumes)),
						message: m.compose_destroy_failed(),
						setLoadingState: (value) => {
							actionStatus[id] = value ? 'destroying' : '';
						},
						onSuccess: async (data) => {
							toast.success(m.compose_destroy_success(), activityToastOptions(extractActivityId(data)));
							await refreshProjects();
						}
					});
				}
			}
		});
	}

	async function handleSyncFromGit(project: Project, gitOpsSyncId: string): Promise<void> {
		const { id: projectId, environmentId: envId } = project;

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
		const targets = new Map(
			getProjects()
				.filter((project) => ids.includes(project.id))
				.map((project) => [project.id, project])
		);
		if (targets.size !== ids.length) return;

		bulkConfirmAndRun({
			ids,
			title: config.title(ids.length),
			message: config.message(ids.length),
			confirmLabel: config.label,
			destructive: config.destructive ?? false,
			run: (id) => config.run(targets.get(id)!),
			messages: {
				success: config.success,
				partial: config.partial,
				failure: config.failure
			},
			setLoading: (loading) => {
				isBulkLoading[config.loadingKey] = loading;
			},
			onComplete: () => refreshProjects(getRequestOptions()),
			clearSelection: () => setSelectedIds([])
		});
	}

	async function handleBulkUp(ids: string[]): Promise<void> {
		// One snapshot for the whole batch — the consuming read would otherwise
		// apply the recreate-volumes opt-in to only the first project. Taken
		// lazily so cancelling the confirm dialog doesn't spend the opt-in.
		let deployOptions: ReturnType<typeof deployOptionsStore.takeRequestOptions> | undefined;
		await runBulkAction(ids, {
			title: (count) => m.projects_bulk_up_confirm_title({ count }),
			message: (count) => m.projects_bulk_up_confirm_message({ count }),
			label: m.common_up(),
			loadingKey: 'up',
			run: (project) =>
				projectService.deployProject(
					project.environmentId,
					project.id,
					'up',
					(deployOptions ??= deployOptionsStore.takeRequestOptions())
				),
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
			run: (project) => projectService.downProject(project.environmentId, project.id),
			success: (count) => m.projects_bulk_down_success({ count }),
			partial: (success, total, failed) => m.projects_bulk_down_partial({ success, total, failed }),
			failure: () => m.compose_stop_failed()
		});
	}

	async function handleBulkRedeploy(ids: string[]): Promise<void> {
		// One lazy snapshot per batch, matching handleBulkUp.
		let deployOptions: ReturnType<typeof deployOptionsStore.takeRequestOptions> | undefined;
		await runBulkAction(ids, {
			title: (count) => m.projects_bulk_redeploy_confirm_title({ count }),
			message: (count) => m.projects_bulk_redeploy_confirm_message({ count }),
			label: m.compose_pull_redeploy(),
			loadingKey: 'redeploy',
			run: (project) =>
				projectService.deployProject(
					project.environmentId,
					project.id,
					'redeploy',
					(deployOptions ??= deployOptionsStore.takeRequestOptions())
				),
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
			run: (project) => projectService.archiveProject(project.environmentId, project.id),
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

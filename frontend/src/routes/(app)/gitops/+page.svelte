<script lang="ts">
	import { afterNavigate, goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import GitOpsImportDialog from '#lib/components/dialogs/gitops-import-dialog.svelte';
	import GitOpsSyncFormSheet from '#lib/components/dialogs/gitops-sync-dialog.svelte';
	import { RefreshIcon, ClockIcon, SuccessIcon, GitBranchIcon, UploadIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { gitOpsSyncListQuery } from '#lib/query/gitops.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import type {
		GitOpsSync,
		GitOpsSyncCounts,
		GitOpsSyncCreate,
		GitOpsSyncMode,
		GitOpsSyncUpdate,
		ImportGitOpsSyncRequest
	} from '#lib/types/automation.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { destinationEnvironmentId, hasShownPermission, readableEnvironments } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SyncTable from './components/sync-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.syncRequestOptions));
	let isSyncDialogOpen = $state(false);
	let syncSession = $state(0);
	let isImportDialogOpen = $state(false);
	let syncToEdit = $state<GitOpsSync | null>(null);
	let pickedEnvironmentId = $state('');
	let isLoadingSync = $state(false);
	const createEnvironmentId = $derived(destinationEnvironmentId(pickedEnvironmentId, 'gitops:create'));
	const canCreate = $derived(hasShownPermission('gitops:create'));

	const syncsQuery = createQuery(() => {
		const scope = pageState.scope;
		return {
			...gitOpsSyncListQuery(scope, readableEnvironments('gitops:list'), pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ scope, value })
		};
	});
	const syncs = $derived(syncsQuery.data?.value ?? data.syncs);
	const resourcesReady = $derived(syncsQuery.data?.scope === pageState.scope || data.scope === pageState.scope);
	const isRefreshing = $derived(syncsQuery.isFetching && !syncsQuery.isPending);

	const saveSyncMutation = createMutation(() => ({
		mutationFn: ({
			sync,
			target,
			environmentId
		}: {
			sync: GitOpsSyncCreate | GitOpsSyncUpdate;
			target: GitOpsSync | null;
			environmentId: string;
		}) =>
			target
				? gitOpsSyncService.updateSync(target.environmentId, target.id, sync as GitOpsSyncUpdate)
				: gitOpsSyncService.createSync(environmentId, sync as GitOpsSyncCreate),
		onSuccess: async (_result, { target }) => {
			toast.success(
				target
					? m.common_update_success({ resource: m.resource_sync() })
					: m.common_create_success({ resource: m.resource_sync() })
			);
			await queryClient.invalidateQueries({ queryKey: queryKeys.gitOpsSyncs.listPrefix(pageState.scope) });
			isSyncDialogOpen = false;
		},
		onError: (error, { target }) => {
			toast.error(
				target
					? m.common_update_failed({ resource: m.resource_sync() })
					: m.common_create_failed({ resource: m.resource_sync() }),
				{ description: extractApiErrorMessage(error) }
			);
		}
	}));

	const importSyncsMutation = createMutation(() => ({
		mutationFn: ({ syncs, environmentId }: { syncs: ImportGitOpsSyncRequest[]; environmentId: string }) =>
			gitOpsSyncService.importSyncs(environmentId, syncs),
		onSuccess: async (response) => {
			if (response.failedCount === 0) {
				toast.success(m.git_sync_import_success({ count: response.successCount }));
				isImportDialogOpen = false;
			} else {
				if (response.successCount > 0) {
					toast.warning(
						m.git_sync_import_partial_success({ successCount: response.successCount, failedCount: response.failedCount })
					);
				} else {
					toast.error(m.git_sync_import_failed_count({ count: response.failedCount }));
				}

				response.errors?.slice(0, 3).forEach((err) => toast.error(err));
			}

			await queryClient.invalidateQueries({ queryKey: queryKeys.gitOpsSyncs.listPrefix(pageState.scope) });
		},
		onError: (error) => {
			toast.error(m.git_sync_import_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	const syncCountsFallback: GitOpsSyncCounts = {
		totalSyncs: 0,
		activeSyncs: 0,
		successfulSyncs: 0,
		deploySyncs: 0,
		backupSyncs: 0
	};
	const syncCounts = $derived(resourcesReady ? (syncs.counts ?? syncCountsFallback) : syncCountsFallback);

	let dialogTargetType = $state<string | undefined>(undefined);
	let dialogMode = $state<GitOpsSyncMode | undefined>(undefined);
	let dialogProjectId = $state<string | undefined>(undefined);

	function parseSyncMode(value: string | null): GitOpsSyncMode | undefined {
		return value === 'deploy' || value === 'backup' ? value : undefined;
	}

	afterNavigate(() => {
		const action = page.url.searchParams.get('action');
		const environmentId = page.url.searchParams.get('env') ?? '';
		if (action === 'create') {
			pickedEnvironmentId = environmentId;
			openCreateSyncDialog(
				page.url.searchParams.get('targetType') ?? undefined,
				parseSyncMode(page.url.searchParams.get('mode')),
				page.url.searchParams.get('projectId') ?? undefined
			);
		} else if (action === 'edit') {
			const syncId = page.url.searchParams.get('syncId');
			if (!syncId || !environmentId) return;
			void openEditSyncDialogById(environmentId, syncId);
		} else {
			return;
		}

		const newUrl = new URL(page.url.href);
		for (const param of ['action', 'targetType', 'mode', 'projectId', 'syncId', 'env']) {
			newUrl.searchParams.delete(param);
		}
		const href = newUrl.searchParams.size > 0 ? resolve(`/(app)/gitops?${newUrl.searchParams}`) : resolve('/(app)/gitops');
		void goto(href, { replace: true, reset: false });
	});

	function openCreateSyncDialog(targetType?: string | Event, mode?: GitOpsSyncMode, projectId?: string) {
		syncToEdit = null;
		dialogTargetType = typeof targetType === 'string' ? targetType : undefined;
		dialogMode = mode;
		dialogProjectId = projectId;
		syncSession += 1;
		isSyncDialogOpen = true;
	}

	function openEditSyncDialog(sync: GitOpsSync) {
		syncToEdit = sync;
		dialogTargetType = undefined;
		dialogMode = undefined;
		dialogProjectId = undefined;
		syncSession += 1;
		isSyncDialogOpen = true;
	}

	async function openEditSyncDialogById(environmentId: string, syncId: string) {
		const loaded = syncs.data.find((item) => item.id === syncId && item.environmentId === environmentId);
		if (loaded) {
			openEditSyncDialog(loaded);
			return;
		}

		await handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.getSync(environmentId, syncId)),
			message: m.common_not_found_title({ resource: m.resource_sync_cap() }),
			setLoadingState: (value) => (isLoadingSync = value),
			onSuccess: (sync) => openEditSyncDialog({ ...sync, environmentId })
		});
	}

	function handleSyncDialogSubmit({ sync, isEditMode }: { sync: GitOpsSyncCreate | GitOpsSyncUpdate; isEditMode: boolean }) {
		saveSyncMutation.mutate({
			sync,
			target: isEditMode && syncToEdit?.id ? syncToEdit : null,
			environmentId: createEnvironmentId
		});
	}

	function handleImportSubmit(syncs: ImportGitOpsSyncRequest[]) {
		importSyncsMutation.mutate({ syncs, environmentId: createEnvironmentId });
	}

	const actionButtons: ActionButton[] = $derived(
		[
			canCreate
				? {
						id: 'create',
						action: 'create',
						placement: 'primary',
						label: m.common_add_button({ resource: m.resource_sync_cap() }),
						onclick: () => {
							pickedEnvironmentId = '';
							openCreateSyncDialog();
						}
					}
				: null,
			canCreate
				? {
						id: 'import',
						action: 'create',
						label: m.git_sync_import_open_button(),
						icon: UploadIcon,
						onclick: () => {
							pickedEnvironmentId = '';
							isImportDialogOpen = true;
						}
					}
				: null,
			{
				id: 'manage-repos',
				action: 'edit',
				label: m.git_repositories_title(),
				icon: GitBranchIcon,
				onclick: () => goto(resolve('/(app)/customize/git-repositories'))
			},
			{
				id: 'refresh',
				action: 'refresh',
				placement: 'secondary',
				iconOnly: true,
				label: m.common_refresh(),
				onclick: () => syncsQuery.refetch(),
				loading: isRefreshing,
				disabled: syncsQuery.isFetching
			}
		].filter((button) => button !== null) as ActionButton[]
	);

	const statCards = $derived<StatCardConfig[]>([
		{
			title: m.common_total(),
			value: syncCounts.totalSyncs,
			icon: RefreshIcon,
			iconColor: 'text-info',
			bgColor: 'bg-info/10'
		},
		{
			title: m.common_active(),
			value: syncCounts.activeSyncs,
			icon: ClockIcon,
			iconColor: 'text-purple',
			bgColor: 'bg-purple/10'
		},
		{
			title: m.common_successful(),
			value: syncCounts.successfulSyncs,
			icon: SuccessIcon,
			iconColor: 'text-success',
			bgColor: 'bg-success/10'
		}
	]);
</script>

<ResourcePageLayout title={m.git_syncs_title()} subtitle={m.git_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SyncTable
			{syncs}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => syncsQuery.refetch()}
			onEditSync={openEditSyncDialog}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#if syncSession > 0}
			{#key syncSession}
				<GitOpsSyncFormSheet
					bind:open={isSyncDialogOpen}
					bind:syncToEdit
					bind:environmentId={() => syncToEdit?.environmentId ?? createEnvironmentId, (value) => (pickedEnvironmentId = value)}
					targetType={dialogTargetType}
					mode={dialogMode}
					projectId={dialogProjectId}
					onSubmit={handleSyncDialogSubmit}
					isLoading={saveSyncMutation.isPending || isLoadingSync}
				/>
			{/key}
		{/if}
		<GitOpsImportDialog
			bind:open={isImportDialogOpen}
			bind:environmentId={() => createEnvironmentId, (value) => (pickedEnvironmentId = value)}
			onSubmit={handleImportSubmit}
			isLoading={importSyncsMutation.isPending}
		/>
	{/snippet}
</ResourcePageLayout>

<script lang="ts">
	import { toast } from 'svelte-sonner';
	import type {
		GitOpsSync,
		GitOpsSyncCounts,
		GitOpsSyncCreateDto,
		GitOpsSyncUpdateDto,
		ImportGitOpsSyncRequest
	} from '#lib/types/automation.js';
	import GitOpsSyncFormSheet from '#lib/components/dialogs/gitops-sync-dialog.svelte';
	import GitOpsImportDialog from '#lib/components/dialogs/gitops-import-dialog.svelte';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';
	import { m } from '#lib/paraglide/messages.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { page } from '$app/state';
	import { afterNavigate, goto } from '$app/navigation';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import SyncTable from './sync-table.svelte';
	import { RefreshIcon, ClockIcon, SuccessIcon, GitBranchIcon, UploadIcon } from '#lib/icons/index.js';

	let { data } = $props();

	let syncs = $derived(data.syncs);
	let selectedIds = $state<string[]>([]);
	let isSyncDialogOpen = $state(false);
	let syncSession = $state(0);
	let isImportDialogOpen = $state(false);
	let syncToEdit = $state<GitOpsSync | null>(null);
	let syncRequestOptions = $derived(data.syncRequestOptions);
	let environmentId = $derived(data.environmentId);

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false,
		import: false
	});

	const syncCountsFallback: GitOpsSyncCounts = {
		totalSyncs: 0,
		activeSyncs: 0,
		successfulSyncs: 0
	};
	const syncCounts = $derived(syncs?.counts ?? syncCountsFallback);

	let dialogTargetType = $state<string | undefined>(undefined);

	afterNavigate(() => {
		if (page.url.searchParams.get('action') !== 'create') return;
		openCreateSyncDialog(page.url.searchParams.get('targetType') ?? undefined);
		const newUrl = new URL(page.url.href);
		newUrl.searchParams.delete('action');
		newUrl.searchParams.delete('targetType');
		void goto(newUrl.toString(), { replaceState: true, reset: false });
	});

	async function refreshSyncs() {
		isLoading.refresh = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.getSyncs(environmentId, syncRequestOptions)),
			message: m.common_refresh_failed({ resource: m.git_syncs_title() }),
			setLoadingState: (value) => (isLoading.refresh = value),
			onSuccess: async (newSyncs) => {
				syncs = newSyncs;
				toast.success(m.common_refresh_success({ resource: m.git_syncs_title() }));
			}
		});
	}

	function openCreateSyncDialog(targetType?: string | Event) {
		syncToEdit = null;
		dialogTargetType = typeof targetType === 'string' ? targetType : undefined;
		syncSession += 1;
		isSyncDialogOpen = true;
	}

	function openEditSyncDialog(sync: GitOpsSync) {
		syncToEdit = sync;
		dialogTargetType = undefined;
		syncSession += 1;
		isSyncDialogOpen = true;
	}

	async function handleSyncDialogSubmit(detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) {
		const { sync, isEditMode } = detail;
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			const operationResult = await tryCatch(
				(async () => {
					if (isEditMode && syncToEdit?.id) {
						await gitOpsSyncService.updateSync(environmentId, syncToEdit.id, sync as GitOpsSyncUpdateDto);
						toast.success(m.common_update_success({ resource: m.resource_sync() }));
					} else {
						await gitOpsSyncService.createSync(environmentId, sync as GitOpsSyncCreateDto);
						toast.success(m.common_create_success({ resource: m.resource_sync() }));
					}

					syncs = await gitOpsSyncService.getSyncs(environmentId, syncRequestOptions);
					isSyncDialogOpen = false;
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;

				console.error('Error saving sync:', error);
				const title = isEditMode
					? m.common_update_failed({ resource: m.resource_sync() })
					: m.common_create_failed({ resource: m.resource_sync() });
				toast.error(title, { description: extractApiErrorMessage(error) });
			}
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	async function handleImportSubmit(data: ImportGitOpsSyncRequest[]) {
		isLoading.import = true;
		try {
			const operationResult = await tryCatch(
				(async () => {
					const response = await gitOpsSyncService.importSyncs(environmentId, data);

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

						// Show error details
						if (response.errors && response.errors.length > 0) {
							console.error('Import errors:', response.errors);
							// Could show a dialog with errors here, for now just toast the first few
							response.errors.slice(0, 3).forEach((err) => toast.error(err));
						}
					}

					syncs = await gitOpsSyncService.getSyncs(environmentId, syncRequestOptions);
				})()
			);
			if (operationResult.error !== null) {
				const error = operationResult.error;

				console.error('Error importing syncs:', error);
				toast.error(error instanceof Error ? error.message : m.git_sync_import_failed());
			}
		} finally {
			isLoading.import = false;
		}
	}

	const actionButtons: ActionButton[] = [
		{
			id: 'create',
			action: 'create',
			label: m.common_add_button({ resource: m.resource_sync_cap() }),
			onclick: () => openCreateSyncDialog()
		},
		{
			id: 'import',
			action: 'create',
			label: m.git_sync_import_open_button(),
			icon: UploadIcon,
			onclick: () => (isImportDialogOpen = true)
		},
		{
			id: 'manage-repos',
			action: 'edit',
			label: m.git_repositories_title(),
			icon: GitBranchIcon,
			onclick: () => goto('/customize/git-repositories')
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshSyncs,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	];

	const statCards = $derived<StatCardConfig[]>([
		{
			title: m.common_total(),
			value: syncCounts.totalSyncs,
			icon: RefreshIcon,
			iconColor: 'text-blue-500',
			bgColor: 'bg-blue-500/10'
		},
		{
			title: m.common_active(),
			value: syncCounts.activeSyncs,
			icon: ClockIcon,
			iconColor: 'text-purple-500',
			bgColor: 'bg-purple-500/10'
		},
		{
			title: m.common_successful(),
			value: syncCounts.successfulSyncs,
			icon: SuccessIcon,
			iconColor: 'text-green-500',
			bgColor: 'bg-green-500/10'
		}
	]);
</script>

<ResourcePageLayout title={m.git_syncs_title()} subtitle={m.git_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SyncTable
			{environmentId}
			bind:syncs
			bind:selectedIds
			bind:requestOptions={syncRequestOptions}
			onEditSync={openEditSyncDialog}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#if syncSession > 0}
			{#key syncSession}
				<GitOpsSyncFormSheet
					bind:open={isSyncDialogOpen}
					bind:syncToEdit
					{environmentId}
					targetType={dialogTargetType}
					onSubmit={handleSyncDialogSubmit}
					isLoading={isLoading.create || isLoading.edit}
				/>
			{/key}
		{/if}
		<GitOpsImportDialog bind:open={isImportDialogOpen} onSubmit={handleImportSubmit} isLoading={isLoading.import} />
	{/snippet}
</ResourcePageLayout>

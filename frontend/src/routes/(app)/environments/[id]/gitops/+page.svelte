<script lang="ts">
	import { toast } from 'svelte-sonner';
	import type { GitOpsSync, GitOpsSyncCreateDto, GitOpsSyncUpdateDto, ImportGitOpsSyncRequest } from '$lib/types/gitops.type';
	import GitOpsSyncFormSheet from '$lib/components/dialogs/gitops-sync-dialog.svelte';
	import GitOpsImportDialog from '$lib/components/dialogs/gitops-import-dialog.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import SyncTable from './sync-table.svelte';
	import { RefreshIcon, ClockIcon, SuccessIcon, GitBranchIcon, UploadIcon } from '$lib/icons';

	let { data } = $props();

	let syncs = $state(untrack(() => data.syncs));
	let selectedIds = $state<string[]>([]);
	let isSyncDialogOpen = $state(false);
	let isImportDialogOpen = $state(false);
	let syncToEdit = $state<GitOpsSync | null>(null);
	let syncRequestOptions = $state(untrack(() => data.syncRequestOptions));
	let environmentId = $derived(data.environmentId);

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false,
		import: false
	});

	const activeSyncs = $derived(syncs.data?.filter((s) => s.enabled && s.autoSync).length ?? 0);
	const successfulSyncs = $derived(syncs.data?.filter((s) => s.lastSyncStatus === 'success').length ?? 0);

	$effect(() => {
		if (page.url.searchParams.get('action') === 'create') {
			// Use a small timeout to ensure the page is fully mounted and ready
			setTimeout(() => {
				openCreateSyncDialog();
				// Remove the query param so it doesn't reopen on refresh
				const newUrl = new URL(page.url);
				newUrl.searchParams.delete('action');
				goto(newUrl.toString(), { replaceState: true, keepFocus: true });
			}, 100);
		}
	});

	async function refreshSyncs() {
		isLoading.refresh = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.getSyncs(environmentId, syncRequestOptions)),
			message: m.common_refresh_failed({ resource: m.git_syncs_title() }),
			setLoadingState: (value) => (isLoading.refresh = value),
			onSuccess: async (newSyncs) => {
				syncs = newSyncs;
				toast.success(m.common_refresh_success({ resource: m.git_syncs_title() }));
			}
		});
	}

	function openCreateSyncDialog() {
		syncToEdit = null;
		isSyncDialogOpen = true;
	}

	function openEditSyncDialog(sync: GitOpsSync) {
		syncToEdit = sync;
		isSyncDialogOpen = true;
	}

	async function handleSyncDialogSubmit(detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) {
		const { sync, isEditMode } = detail;
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && syncToEdit?.id) {
				await gitOpsSyncService.updateSync(environmentId, syncToEdit.id, sync as GitOpsSyncUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_sync() }));
			} else {
				await gitOpsSyncService.createSync(environmentId, sync as GitOpsSyncCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_sync() }));
			}

			syncs = await gitOpsSyncService.getSyncs(environmentId, syncRequestOptions);
			isSyncDialogOpen = false;
		} catch (error) {
			console.error('Error saving sync:', error);
			toast.error(error instanceof Error ? error.message : m.common_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	async function handleImportSubmit(data: ImportGitOpsSyncRequest[]) {
		isLoading.import = true;
		try {
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
		} catch (error) {
			console.error('Error importing syncs:', error);
			toast.error(error instanceof Error ? error.message : m.git_sync_import_failed());
		} finally {
			isLoading.import = false;
		}
	}

	const actionButtons: ActionButton[] = [
		{
			id: 'create',
			action: 'create',
			label: m.common_add_button({ resource: m.resource_sync_cap() }),
			onclick: openCreateSyncDialog
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
			value: syncs?.pagination?.totalItems ?? 0,
			icon: RefreshIcon,
			iconColor: 'text-blue-500',
			bgColor: 'bg-blue-500/10'
		},
		{
			title: m.common_active(),
			value: activeSyncs,
			icon: ClockIcon,
			iconColor: 'text-purple-500',
			bgColor: 'bg-purple-500/10'
		},
		{
			title: m.common_successful(),
			value: successfulSyncs,
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
		<GitOpsSyncFormSheet
			bind:open={isSyncDialogOpen}
			bind:syncToEdit
			onSubmit={handleSyncDialogSubmit}
			isLoading={isLoading.create || isLoading.edit}
		/>
		<GitOpsImportDialog bind:open={isImportDialogOpen} onSubmit={handleImportSubmit} isLoading={isLoading.import} />
	{/snippet}
</ResourcePageLayout>

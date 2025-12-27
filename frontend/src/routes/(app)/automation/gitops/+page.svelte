<script lang="ts">
	import { toast } from 'svelte-sonner';
	import type { GitRepository, GitRepositoryCreateDto, GitRepositoryUpdateDto } from '$lib/types/gitops.type';
	import type { GitOpsSync, GitOpsSyncCreateDto, GitOpsSyncUpdateDto } from '$lib/types/gitops.type';
	import GitRepositoryFormSheet from '$lib/components/sheets/git-repository-sheet.svelte';
	import GitOpsSyncFormSheet from '$lib/components/sheets/gitops-sync-sheet.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { TabBar, type TabItem } from '$lib/components/tab-bar';
	import * as Tabs from '$lib/components/ui/tabs';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import CheckCircleIcon from '@lucide/svelte/icons/check-circle';
	import GitBranchIcon from '@lucide/svelte/icons/git-branch';
	import SyncTable from './sync-table.svelte';
	import RepositoryTable from './repository-table.svelte';

	let { data } = $props();

	let syncs = $state(untrack(() => data.syncs));
	let repositories = $state(untrack(() => data.repositories));
	let selectedSyncIds = $state<string[]>([]);
	let selectedRepoIds = $state<string[]>([]);
	let isSyncDialogOpen = $state(false);
	let isRepositoryDialogOpen = $state(false);
	let syncToEdit = $state<GitOpsSync | null>(null);
	let repositoryToEdit = $state<GitRepository | null>(null);
	let syncRequestOptions = $state(untrack(() => data.syncRequestOptions));
	let repositoryRequestOptions = $state(untrack(() => data.repositoryRequestOptions));
	let activeView = $state<'syncs' | 'repositories'>('syncs');

	const tabItems: TabItem[] = [
		{
			value: 'syncs',
			label: m.gitops_syncs_title(),
			icon: RefreshCwIcon
		},
		{
			value: 'repositories',
			label: m.git_repositories_title(),
			icon: GitBranchIcon
		}
	];

	let isLoading = $state({
		createSync: false,
		editSync: false,
		refreshSync: false,
		createRepo: false,
		editRepo: false,
		refreshRepo: false
	});

	const activeSyncs = $derived(syncs.data?.filter((s) => s.enabled && s.autoSync).length ?? 0);
	const successfulSyncs = $derived(syncs.data?.filter((s) => s.lastSyncStatus === 'success').length ?? 0);

	// Sync functions
	async function refreshSyncs() {
		isLoading.refreshSync = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.getSyncs(syncRequestOptions)),
			message: m.common_refresh_failed({ resource: m.gitops_syncs_title() }),
			setLoadingState: (value) => (isLoading.refreshSync = value),
			onSuccess: async (newSyncs) => {
				syncs = newSyncs;
				toast.success(m.common_refresh_success({ resource: m.gitops_syncs_title() }));
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
		const loadingKey = isEditMode ? 'editSync' : 'createSync';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && syncToEdit?.id) {
				await gitOpsSyncService.updateSync(syncToEdit.id, sync as GitOpsSyncUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_sync() }));
			} else {
				await gitOpsSyncService.createSync(sync as GitOpsSyncCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_sync() }));
			}

			syncs = await gitOpsSyncService.getSyncs(syncRequestOptions);
			isSyncDialogOpen = false;
		} catch (error) {
			console.error('Error saving sync:', error);
			toast.error(error instanceof Error ? error.message : m.common_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	// Repository functions
	async function refreshRepositories() {
		isLoading.refreshRepo = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitRepositoryService.getRepositories(repositoryRequestOptions)),
			message: m.common_refresh_failed({ resource: m.git_repositories_title() }),
			setLoadingState: (value) => (isLoading.refreshRepo = value),
			onSuccess: async (newRepositories) => {
				repositories = newRepositories;
				toast.success(m.common_refresh_success({ resource: m.git_repositories_title() }));
			}
		});
	}

	function openCreateRepositoryDialog() {
		repositoryToEdit = null;
		isRepositoryDialogOpen = true;
	}

	function openEditRepositoryDialog(repository: GitRepository) {
		repositoryToEdit = repository;
		isRepositoryDialogOpen = true;
	}

	async function handleRepositoryDialogSubmit(detail: {
		repository: GitRepositoryCreateDto | GitRepositoryUpdateDto;
		isEditMode: boolean;
	}) {
		const { repository, isEditMode } = detail;
		const loadingKey = isEditMode ? 'editRepo' : 'createRepo';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && repositoryToEdit?.id) {
				await gitRepositoryService.updateRepository(repositoryToEdit.id, repository as GitRepositoryUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_repository() }));
			} else {
				await gitRepositoryService.createRepository(repository as GitRepositoryCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_repository() }));
			}

			repositories = await gitRepositoryService.getRepositories(repositoryRequestOptions);
			isRepositoryDialogOpen = false;
		} catch (error) {
			console.error('Error saving repository:', error);
			toast.error(error instanceof Error ? error.message : m.common_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	const actionButtons = $derived<ActionButton[]>(
		activeView === 'syncs'
			? [
					{
						id: 'create',
						action: 'create',
						label: m.common_add_button({ resource: m.resource_sync_cap() }),
						onclick: openCreateSyncDialog
					},
					{
						id: 'refresh',
						action: 'restart',
						label: m.common_refresh(),
						onclick: refreshSyncs,
						loading: isLoading.refreshSync,
						disabled: isLoading.refreshSync
					}
				]
			: [
					{
						id: 'create',
						action: 'create',
						label: m.common_add_button({ resource: m.resource_repository_cap() }),
						onclick: openCreateRepositoryDialog
					},
					{
						id: 'refresh',
						action: 'restart',
						label: m.common_refresh(),
						onclick: refreshRepositories,
						loading: isLoading.refreshRepo,
						disabled: isLoading.refreshRepo
					}
				]
	);

	const statCards = $derived<StatCardConfig[]>([
		{
			title: m.common_total(),
			value: syncs?.pagination?.totalItems ?? 0,
			icon: RefreshCwIcon,
			iconColor: 'text-blue-500',
			bgColor: 'bg-blue-500/10',
			class: 'border-l-4 border-l-blue-500'
		},
		{
			title: m.common_active(),
			value: activeSyncs,
			icon: ClockIcon,
			iconColor: 'text-purple-500',
			bgColor: 'bg-purple-500/10',
			class: 'border-l-4 border-l-purple-500'
		},
		{
			title: m.common_successful(),
			value: successfulSyncs,
			icon: CheckCircleIcon,
			iconColor: 'text-green-500',
			bgColor: 'bg-green-500/10',
			class: 'border-l-4 border-l-green-500'
		},
		{
			title: m.git_repositories_title(),
			value: repositories?.pagination?.totalItems ?? 0,
			icon: GitBranchIcon,
			iconColor: 'text-orange-500',
			bgColor: 'bg-orange-500/10',
			class: 'border-l-4 border-l-orange-500'
		}
	]);
</script>

<ResourcePageLayout title={m.gitops_title()} subtitle={m.gitops_subtitle()} {actionButtons} {statCards} statCardsColumns={4}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Tabs.Root bind:value={activeView}>
				<div class="pb-6">
					<div class="w-fit">
						<TabBar
							items={tabItems}
							value={activeView}
							onValueChange={(value) => (activeView = value as 'syncs' | 'repositories')}
						/>
					</div>
				</div>

				<Tabs.Content value="syncs">
					<SyncTable
						bind:syncs
						bind:selectedIds={selectedSyncIds}
						bind:requestOptions={syncRequestOptions}
						onEditSync={openEditSyncDialog}
					/>
				</Tabs.Content>

				<Tabs.Content value="repositories">
					<RepositoryTable
						bind:repositories
						bind:selectedIds={selectedRepoIds}
						bind:requestOptions={repositoryRequestOptions}
						onEditRepository={openEditRepositoryDialog}
					/>
				</Tabs.Content>
			</Tabs.Root>
		</div>
	{/snippet}

	{#snippet additionalContent()}
		<GitOpsSyncFormSheet
			bind:open={isSyncDialogOpen}
			bind:syncToEdit
			onSubmit={handleSyncDialogSubmit}
			isLoading={isLoading.createSync || isLoading.editSync}
		/>
		<GitRepositoryFormSheet
			bind:open={isRepositoryDialogOpen}
			bind:repositoryToEdit
			onSubmit={handleRepositoryDialogSubmit}
			isLoading={isLoading.createRepo || isLoading.editRepo}
		/>
	{/snippet}
</ResourcePageLayout>

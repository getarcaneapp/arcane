<script lang="ts">
	import { toast } from 'svelte-sonner';
	import type { GitRepository, GitRepositoryCreateDto, GitRepositoryUpdateDto } from '$lib/types/gitops.type';
	import GitRepositoryFormSheet from '$lib/components/sheets/git-repository-sheet.svelte';
	import RepositoryTable from './repository-table.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';

	let { data } = $props();

	let repositories = $state(untrack(() => data.repositories));
	let selectedIds = $state<string[]>([]);
	let isRepositoryDialogOpen = $state(false);
	let repositoryToEdit = $state<GitRepository | null>(null);
	let requestOptions = $state(untrack(() => data.repositoryRequestOptions));

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false
	});

	async function refreshRepositories() {
		isLoading.refresh = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitRepositoryService.getRepositories(requestOptions)),
			message: m.common_refresh_failed({ resource: m.git_repositories_title() }),
			setLoadingState: (value) => (isLoading.refresh = value),
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
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && repositoryToEdit?.id) {
				await gitRepositoryService.updateRepository(repositoryToEdit.id, repository as GitRepositoryUpdateDto);
				toast.success(m.common_update_success({ resource: m.resource_repository() }));
			} else {
				await gitRepositoryService.createRepository(repository as GitRepositoryCreateDto);
				toast.success(m.common_create_success({ resource: m.resource_repository() }));
			}

			repositories = await gitRepositoryService.getRepositories(requestOptions);
			isRepositoryDialogOpen = false;
		} catch (error) {
			console.error('Error saving repository:', error);
			toast.error(error instanceof Error ? error.message : m.common_save_failed());
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	const actionButtons: ActionButton[] = [
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
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	];
</script>

<ResourcePageLayout title={m.git_repositories_title()} subtitle={m.git_repositories_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<RepositoryTable bind:repositories bind:selectedIds bind:requestOptions onEditRepository={openEditRepositoryDialog} />
	{/snippet}

	{#snippet additionalContent()}
		<GitRepositoryFormSheet
			bind:open={isRepositoryDialogOpen}
			bind:repositoryToEdit
			onSubmit={handleRepositoryDialogSubmit}
			isLoading={isLoading.create || isLoading.edit}
		/>
	{/snippet}
</ResourcePageLayout>

<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import GitRepositoryFormSheet from '#lib/components/sheets/git-repository-sheet.svelte';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { gitRepositoryListQuery } from '#lib/query/git-repositories.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitRepositoryService } from '#lib/services/git-repository-service.js';
	import type { GitRepository, GitRepositoryCreate, GitRepositoryUpdate } from '#lib/types/automation.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasShownPermission } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import RepositoryTable from './components/repository-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.repositoryRequestOptions));
	let isRepositoryDialogOpen = $state(false);
	let repositorySession = $state(0);
	let clearToken = $state(false);
	let clearSshKey = $state(false);
	let clearSigningKey = $state(false);
	let repositoryToEdit = $state<GitRepository | null>(null);
	let isRefreshing = $state(false);
	const repositoriesQuery = createQuery(() => ({
		...gitRepositoryListQuery(pageState.requestOptions),
		placeholderData: (previous) => previous
	}));
	const repositories = $derived(repositoriesQuery.data ?? data.repositories);

	const saveRepositoryMutation = createMutation(() => ({
		mutationFn: ({
			repository,
			target
		}: {
			repository: GitRepositoryCreate | GitRepositoryUpdate;
			target: GitRepository | null;
		}) =>
			target
				? gitRepositoryService.updateRepository(target.id, repository as GitRepositoryUpdate)
				: gitRepositoryService.createRepository(repository as GitRepositoryCreate),
		onSuccess: async (_result, { target }) => {
			toast.success(
				target
					? m.common_update_success({ resource: m.resource_repository() })
					: m.common_create_success({ resource: m.resource_repository() })
			);
			await queryClient.invalidateQueries({ queryKey: queryKeys.gitRepositories.all });
			clearToken = false;
			clearSshKey = false;
			clearSigningKey = false;
			isRepositoryDialogOpen = false;
		},
		onError: (error) => {
			toast.error(m.common_save_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	async function refreshRepositories() {
		isRefreshing = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(repositoriesQuery.refetch({ throwOnError: true })),
			message: m.common_refresh_failed({ resource: m.git_repositories_title() }),
			setLoadingState: (value) => (isRefreshing = value),
			onSuccess: () => {
				toast.success(m.common_refresh_success({ resource: m.git_repositories_title() }));
			}
		});
	}

	function openCreateRepositoryDialog() {
		repositoryToEdit = null;
		clearToken = false;
		clearSshKey = false;
		clearSigningKey = false;
		repositorySession += 1;
		isRepositoryDialogOpen = true;
	}

	function openEditRepositoryDialog(repository: GitRepository) {
		repositoryToEdit = repository;
		clearToken = false;
		clearSshKey = false;
		clearSigningKey = false;
		repositorySession += 1;
		isRepositoryDialogOpen = true;
	}

	function handleRepositoryDialogSubmit({
		repository,
		isEditMode
	}: {
		repository: GitRepositoryCreate | GitRepositoryUpdate;
		isEditMode: boolean;
	}) {
		saveRepositoryMutation.mutate({ repository, target: isEditMode && repositoryToEdit?.id ? repositoryToEdit : null });
	}

	const canCreateRepository = $derived(hasShownPermission('git-repositories:create'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateRepository) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.common_add_button({ resource: m.resource_repository_cap() }),
				onclick: openCreateRepositoryDialog
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refreshRepositories,
			loading: isRefreshing,
			disabled: isRefreshing
		});
		return buttons;
	});
</script>

<ResourcePageLayout title={m.git_repositories_title()} subtitle={m.git_repositories_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		<RepositoryTable
			{repositories}
			bind:selectedIds={pageState.selectedIds}
			bind:requestOptions={pageState.requestOptions}
			onEditRepository={openEditRepositoryDialog}
		/>
	{/snippet}

	{#snippet additionalContent()}
		{#if repositorySession > 0}
			{#key repositorySession}
				<GitRepositoryFormSheet
					bind:clearToken
					bind:clearSshKey
					bind:clearSigningKey
					bind:open={isRepositoryDialogOpen}
					bind:repositoryToEdit
					onSubmit={handleRepositoryDialogSubmit}
					isLoading={saveRepositoryMutation.isPending}
				/>
			{/key}
		{/if}
	{/snippet}
</ResourcePageLayout>

<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import GitBranchIcon from '@lucide/svelte/icons/git-branch';
	import { toast } from 'svelte-sonner';
	import type { GitOpsRepository } from '$lib/types/gitops-repository.type';
	import type { GitOpsRepositoryCreateDto, GitOpsRepositoryUpdateDto } from '$lib/types/gitops-repository.type';
	import GitOpsRepositoryFormSheet from '$lib/components/sheets/gitops-repository-sheet.svelte';
	import GitOpsTable from './gitops-table.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { m } from '$lib/paraglide/messages';
	import { gitopsRepositoryService } from '$lib/services/gitops-repository-service';
	import { ResourcePageLayout, type ActionButton } from '$lib/layouts/index.js';

	let { data } = $props();

	let repositories = $state(data.repositories);
	let selectedIds = $state<string[]>([]);
	let isRepositoryDialogOpen = $state(false);
	let repositoryToEdit = $state<GitOpsRepository | null>(null);
	let requestOptions = $state(data.repositoryRequestOptions);

	let isLoading = $state({
		create: false,
		edit: false,
		refresh: false
	});

	async function refreshRepositories() {
		isLoading.refresh = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitopsRepositoryService.getRepositories(requestOptions)),
			message: 'Failed to refresh GitOps repositories',
			setLoadingState: (value) => (isLoading.refresh = value),
			onSuccess: async (newRepositories) => {
				repositories = newRepositories;
				toast.success('GitOps repositories refreshed');
			}
		});
	}

	function openCreateRepositoryDialog() {
		repositoryToEdit = null;
		isRepositoryDialogOpen = true;
	}

	function openEditRepositoryDialog(repository: GitOpsRepository) {
		repositoryToEdit = repository;
		isRepositoryDialogOpen = true;
	}

	async function handleRepositoryDialogSubmit(detail: {
		repository: GitOpsRepositoryCreateDto | GitOpsRepositoryUpdateDto;
		isEditMode: boolean;
	}) {
		const { repository, isEditMode } = detail;
		const loadingKey = isEditMode ? 'edit' : 'create';
		isLoading[loadingKey] = true;

		try {
			if (isEditMode && repositoryToEdit?.id) {
				await gitopsRepositoryService.updateRepository(repositoryToEdit.id, repository as GitOpsRepositoryUpdateDto);
				toast.success('GitOps repository updated successfully');
			} else {
				await gitopsRepositoryService.createRepository(repository as GitOpsRepositoryCreateDto);
				toast.success('GitOps repository created successfully');
			}

			repositories = await gitopsRepositoryService.getRepositories(requestOptions);
			isRepositoryDialogOpen = false;
		} catch (error) {
			console.error('Error saving repository:', error);
			toast.error(error instanceof Error ? error.message : 'Failed to save GitOps repository');
		} finally {
			isLoading[loadingKey] = false;
		}
	}

	const actionButtons: ActionButton[] = [
		{
			id: 'create',
			action: 'create',
			label: 'Add GitOps Repository',
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

<ResourcePageLayout title="GitOps Repositories" subtitle="Manage Git repositories for Docker Compose projects" {actionButtons}>
	{#snippet mainContent()}
		<div class="space-y-6">
			<Card.Root class="flex flex-col gap-6 border py-3 shadow-sm">
				<Card.Header
					class="@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 pb-4 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6"
				>
					<div class="flex items-center gap-3">
						<div class="rounded-full bg-blue-500/10 p-2">
							<GitBranchIcon class="size-5 text-blue-500" />
						</div>
						<div>
							<Card.Title>GitOps Repositories</Card.Title>
							<Card.Description>
								Configure Git repositories containing Docker Compose files for automatic deployment
							</Card.Description>
						</div>
					</div>
				</Card.Header>
				<Card.Content class="px-6">
					<GitOpsTable bind:repositories bind:selectedIds bind:requestOptions onEditRepository={openEditRepositoryDialog} />
				</Card.Content>
			</Card.Root>

			<Card.Root class="flex flex-col gap-6 border py-3 shadow-sm">
				<Card.Header
					class="@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-1.5 px-6 has-data-[slot=card-action]:grid-cols-[1fr_auto] [.border-b]:pb-6"
				>
					<Card.Title class="text-lg">GitOps Information</Card.Title>
					<Card.Description>Learn about GitOps for Compose projects in Arcane</Card.Description>
				</Card.Header>
				<Card.Content class="px-6">
					<div class="grid grid-cols-1 gap-4 md:grid-cols-2">
						<div class="space-y-3">
							<h4 class="text-sm font-medium">How It Works</h4>
							<div class="text-muted-foreground space-y-1 text-sm">
								<p>• Connect your Git repository containing docker-compose files</p>
								<p>• Arcane automatically syncs changes at configured intervals</p>
								<p>• Projects are deployed automatically when changes are detected</p>
								<p>• Supports both public and private repositories</p>
							</div>
						</div>
						<div class="space-y-3">
							<h4 class="text-sm font-medium">Configuration Notes</h4>
							<div class="text-muted-foreground space-y-1 text-sm">
								<p>• Use HTTPS URLs (e.g., https://github.com/user/repo.git)</p>
								<p>• For private repos, provide a personal access token</p>
								<p>• Specify the path to your compose file in the repository</p>
								<p>• Enable auto-sync to automatically deploy changes</p>
							</div>
						</div>
					</div>
				</Card.Content>
			</Card.Root>
		</div>
	{/snippet}

	{#snippet additionalContent()}
		<GitOpsRepositoryFormSheet
			bind:open={isRepositoryDialogOpen}
			bind:repositoryToEdit
			onSubmit={handleRepositoryDialogSubmit}
			isLoading={isLoading.create || isLoading.edit}
		/>
	{/snippet}
</ResourcePageLayout>

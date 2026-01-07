<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import FileBrowserDialog from '$lib/components/dialogs/file-browser-dialog.svelte';
	import type { GitOpsSync, GitOpsSyncCreateDto, GitOpsSyncUpdateDto, GitRepository, BranchInfo } from '$lib/types/gitops.type';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { FolderOpenIcon } from '$lib/icons';

	type GitOpsSyncFormProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		onSubmit: (detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), syncToEdit = $bindable(), onSubmit, isLoading }: GitOpsSyncFormProps = $props();

	let isEditMode = $derived(!!syncToEdit);
	let repositories = $state<GitRepository[]>([]);
	let branches = $state<BranchInfo[]>([]);
	let loadingData = $state(true);
	let loadingBranches = $state(false);
	let showFileBrowser = $state(false);

	const formSchema = z.object({
		name: z.string().min(1, m.common_name_required()),
		repositoryId: z.string().min(1, m.common_required()),
		branch: z.string().min(1, m.common_required()),
		composePath: z.string().min(1, m.common_required()),
		autoSync: z.boolean().default(true),
		syncInterval: z.number().min(1).default(5),
		enabled: z.boolean().default(true)
	});

	let formData = $derived({
		name: open && syncToEdit ? syncToEdit.name : '',
		repositoryId: open && syncToEdit ? syncToEdit.repositoryId : '',
		branch: open && syncToEdit ? syncToEdit.branch : 'main',
		composePath: open && syncToEdit ? syncToEdit.composePath : 'docker-compose.yml',
		autoSync: open && syncToEdit ? (syncToEdit.autoSync ?? true) : true,
		syncInterval: open && syncToEdit ? (syncToEdit.syncInterval ?? 5) : 5,
		enabled: open && syncToEdit ? (syncToEdit.enabled ?? true) : true
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let selectedRepository = $state<{ value: string; label: string } | undefined>(undefined);

	async function loadBranches(repositoryId: string) {
		if (!repositoryId) return;

		loadingBranches = true;
		branches = [];
		try {
			const result = await gitRepositoryService.getBranches(repositoryId);
			branches = result.branches || [];

			// Auto-select default branch if not editing
			if (!isEditMode && branches.length > 0) {
				const defaultBranch = branches.find((b) => b.isDefault);
				if (defaultBranch) {
					$inputs.branch.value = defaultBranch.name;
				}
			}
		} catch (error) {
			console.error('Failed to load branches:', error);
		} finally {
			loadingBranches = false;
		}
	}

	async function loadData() {
		loadingData = true;
		// Reset state before loading data to ensure a clean state
		selectedRepository = undefined;
		branches = [];
		loadingBranches = false;
		showFileBrowser = false;

		try {
			const reposResult = await gitRepositoryService.getRepositories({ pagination: { page: 1, limit: 100 } });
			repositories = reposResult.data || [];

			if (syncToEdit) {
				const repo = repositories.find((r) => r.id === syncToEdit.repositoryId);
				if (repo) {
					selectedRepository = { value: repo.id, label: repo.name };
					await loadBranches(repo.id);
				}
			}
		} catch (error) {
			console.error('Failed to load data:', error);
		} finally {
			loadingData = false;
		}
	}

	$effect(() => {
		if (open) {
			loadData();
			if (!isEditMode) {
				form.reset();
			}
		}
	});

	function handleSubmit() {
		const data = form.validate();
		if (!data) return;

		const payload: GitOpsSyncCreateDto | GitOpsSyncUpdateDto = {
			name: data.name,
			repositoryId: selectedRepository?.value || data.repositoryId,
			branch: data.branch,
			composePath: data.composePath,
			projectName: data.name,
			autoSync: data.autoSync,
			syncInterval: data.syncInterval,
			enabled: data.enabled
		};

		onSubmit({ sync: payload, isEditMode });
	}
</script>

<ResponsiveDialog
	bind:open
	title={isEditMode ? m.git_sync_edit_title() : m.git_sync_add_title()}
	description={isEditMode ? m.common_edit_description() : m.common_add_description()}
	contentClass="sm:max-w-2xl"
>
	{#snippet children()}
		{#if loadingData}
			<div class="flex items-center justify-center py-8">
				<Spinner class="size-6" />
			</div>
		{:else}
			<form id="sync-form" onsubmit={preventDefault(handleSubmit)} class="grid gap-y-3 py-4">
				<FormInput label={m.git_sync_name()} type="text" placeholder={m.common_name_placeholder()} bind:input={$inputs.name} />

				<div class="space-y-1.5">
					<Label for="repository">{m.git_sync_repository()}</Label>
					<Select.Root
						type="single"
						value={selectedRepository?.value}
						onValueChange={(v) => {
							if (v) {
								const repo = repositories.find((r) => r.id === v);
								if (repo) {
									selectedRepository = { value: repo.id, label: repo.name };
									$inputs.repositoryId.value = v;
									loadBranches(v);
								}
							}
						}}
					>
						<Select.Trigger id="repository" class="w-full" aria-invalid={$inputs.repositoryId.error ? 'true' : undefined}>
							<span>{selectedRepository?.label ?? m.common_select_placeholder()}</span>
						</Select.Trigger>
						<Select.Content style="width: var(--bits-select-anchor-width);">
							{#each repositories as repo}
								<Select.Item value={repo.id} class="truncate">{repo.name}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					{#if $inputs.repositoryId.error}
						<p class="mt-1 text-sm text-red-500">{$inputs.repositoryId.error}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="branch">{m.git_sync_branch()}</Label>
					{#if loadingBranches}
						<div class="flex items-center gap-2">
							<Spinner class="size-4" />
							<span class="text-muted-foreground text-sm">Loading branches...</span>
						</div>
					{:else if branches.length > 0}
						<Select.Root
							type="single"
							value={$inputs.branch.value}
							onValueChange={(v) => {
								if (v) {
									$inputs.branch.value = v;
								}
							}}
						>
							<Select.Trigger id="branch" class="w-full" aria-invalid={$inputs.branch.error ? 'true' : undefined}>
								<span>{$inputs.branch.value || m.common_select_placeholder()}</span>
							</Select.Trigger>
							<Select.Content style="width: var(--bits-select-anchor-width);">
								{#each branches as branch}
									<Select.Item value={branch.name} class="truncate">
										{branch.name}
										{#if branch.isDefault}
											<span class="text-muted-foreground ml-2 text-xs">(default)</span>
										{/if}
									</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
					{:else}
						<FormInput type="text" placeholder="main" bind:input={$inputs.branch} />
					{/if}
					{#if $inputs.branch.error}
						<p class="mt-1 text-sm text-red-500">{$inputs.branch.error}</p>
					{/if}
					<p class="text-muted-foreground text-xs">
						{branches.length > 0 ? m.git_sync_branch_select_hint() : m.git_sync_branch_manual_hint()}
					</p>
				</div>

				<div class="space-y-1.5">
					<Label for="composePath">{m.git_sync_compose_path()}</Label>
					<div class="flex gap-2">
						<div class="flex-1">
							<FormInput type="text" placeholder="docker-compose.yml" bind:input={$inputs.composePath} />
						</div>
						<Button
							type="button"
							variant="outline"
							size="icon"
							onclick={() => (showFileBrowser = true)}
							disabled={!selectedRepository?.value || !$inputs.branch.value}
							title="Browse files"
						>
							<FolderOpenIcon class="size-4" />
						</Button>
					</div>
					{#if !selectedRepository?.value || !$inputs.branch.value}
						<p class="text-muted-foreground text-xs">Select a repository and branch to browse files</p>
					{/if}
				</div>

				<SwitchWithLabel
					id="autoSyncSwitch"
					label={m.git_sync_auto_sync()}
					description={m.common_auto_sync_description()}
					error={$inputs.autoSync.error}
					bind:checked={$inputs.autoSync.value}
				/>

				<FormInput label={m.git_sync_sync_interval()} type="number" placeholder="5" bind:input={$inputs.syncInterval} />

				<SwitchWithLabel
					id="isEnabledSwitch"
					label={m.common_enabled()}
					description={m.common_enabled_description()}
					error={$inputs.enabled.error}
					bind:checked={$inputs.enabled.value}
				/>
			</form>
		{/if}
	{/snippet}

	{#snippet footer()}
		<Button
			type="button"
			class="arcane-button-cancel flex-1"
			variant="outline"
			onclick={() => (open = false)}
			disabled={isLoading}
		>
			{m.common_cancel()}
		</Button>

		<Button type="submit" form="sync-form" class="arcane-button-create flex-1" disabled={isLoading}>
			{#if isLoading}
				<Spinner class="mr-2 size-4" />
			{/if}
			{isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.resource_sync_cap() })}
		</Button>
	{/snippet}
</ResponsiveDialog>

<FileBrowserDialog
	bind:open={showFileBrowser}
	repositoryId={selectedRepository?.value || ''}
	branch={$inputs.branch.value}
	onSelect={(path) => {
		$inputs.composePath.value = path;
	}}
/>

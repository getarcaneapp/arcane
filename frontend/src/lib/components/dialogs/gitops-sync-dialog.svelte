<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import SelectWithLabel from '$lib/components/form/select-with-label.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import * as Select from '$lib/components/ui/select/index.js';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Switch } from '$lib/components/ui/switch/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import FileBrowserDialog from '$lib/components/dialogs/file-browser-dialog.svelte';
	import type { GitOpsSync, GitOpsSyncCreateDto, GitOpsSyncUpdateDto, GitRepository, BranchInfo } from '$lib/types/gitops.type';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { settingsService } from '$lib/services/settings-service';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { queryKeys } from '$lib/query/query-keys';
	import { m } from '$lib/paraglide/messages';
	import { ArrowRightIcon, FolderOpenIcon, InfoIcon } from '$lib/icons';
	import * as Alert from '$lib/components/ui/alert';
	import { createQuery } from '@tanstack/svelte-query';

	type GitOpsSyncFormProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		targetType?: string;
		onSubmit: (detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let { open = $bindable(false), syncToEdit = $bindable(), targetType, onSubmit, isLoading }: GitOpsSyncFormProps = $props();

	type GitOpsSyncTargetType = 'project' | 'swarm_stack';

	let isEditMode = $derived(!!syncToEdit);
	let showFileBrowser = $state(false);
	let selectedTargetType = $state<GitOpsSyncTargetType>('project');

	const targetTypeOptions = [
		{ value: 'project', label: m.project() },
		{ value: 'swarm_stack', label: m.swarm_stack() }
	] satisfies { value: GitOpsSyncTargetType; label: string; description?: string }[];

	function normalizeTargetType(value?: string | null): GitOpsSyncTargetType {
		return value === 'swarm_stack' ? 'swarm_stack' : 'project';
	}

	const formSchema = z.object({
		name: z.string().min(1, m.common_name_required()),
		repositoryId: z.string().min(1, m.common_required()),
		branch: z.string().min(1, m.common_required()),
		composePath: z.string().min(1, m.common_required()),
		syncDirectory: z.boolean().default(false),
		maxSyncFiles: z.coerce.number().int().nonnegative(),
		maxSyncTotalSizeMb: z.coerce.number().int().nonnegative(),
		maxSyncBinarySizeMb: z.coerce.number().int().nonnegative(),
		autoSync: z.boolean().default(true),
		syncInterval: z.number().min(1).default(5)
	});

	const bytesPerMegabyte = 1024 * 1024;

	function bytesToMegabytesInternal(value: number | undefined, fallback: number): number {
		if (value === undefined) return fallback;
		return Math.round(value / bytesPerMegabyte);
	}

	function megabytesToBytesInternal(value: number): number {
		return value * bytesPerMegabyte;
	}

	const settingsQuery = createQuery(() => ({
		queryKey: queryKeys.settings.global(),
		queryFn: () => settingsService.getSettings(),
		enabled: open,
		staleTime: 30_000
	}));

	let formData = $derived({
		name: open && syncToEdit ? syncToEdit.name : '',
		repositoryId: open && syncToEdit ? syncToEdit.repositoryId : '',
		branch: open && syncToEdit ? syncToEdit.branch : 'main',
		composePath: open && syncToEdit ? syncToEdit.composePath : 'docker-compose.yml',
		syncDirectory: open && syncToEdit ? (syncToEdit.syncDirectory ?? false) : false,
		maxSyncFiles: open && syncToEdit ? (syncToEdit.maxSyncFiles ?? 0) : (settingsQuery.data?.gitSyncMaxFiles ?? 0),
		maxSyncTotalSizeMb:
			open && syncToEdit
				? bytesToMegabytesInternal(syncToEdit.maxSyncTotalSize, 0)
				: (settingsQuery.data?.gitSyncMaxTotalSizeMb ?? 0),
		maxSyncBinarySizeMb:
			open && syncToEdit
				? bytesToMegabytesInternal(syncToEdit.maxSyncBinarySize, 0)
				: (settingsQuery.data?.gitSyncMaxBinarySizeMb ?? 0),
		autoSync: open && syncToEdit ? (syncToEdit.autoSync ?? true) : true,
		syncInterval: open && syncToEdit ? (syncToEdit.syncInterval ?? 5) : 5
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let selectedRepository = $state<{ value: string; label: string } | undefined>(undefined);
	const repositoriesQuery = createQuery(() => ({
		queryKey: queryKeys.gitRepositories.syncDialog(),
		queryFn: () => gitRepositoryService.getRepositories({ pagination: { page: 1, limit: 100 } }),
		enabled: open,
		staleTime: 0
	}));
	const repositories = $derived<GitRepository[]>(repositoriesQuery.data?.data ?? []);
	const loadingSettings = $derived(!isEditMode && (settingsQuery.isPending || settingsQuery.isFetching));
	const loadingData = $derived(repositoriesQuery.isPending || repositoriesQuery.isFetching || loadingSettings);

	const branchesQuery = createQuery(() => ({
		queryKey: queryKeys.gitRepositories.branches(selectedRepository?.value || ''),
		queryFn: () => gitRepositoryService.getBranches(selectedRepository?.value || ''),
		enabled: open && !!selectedRepository?.value,
		staleTime: 0
	}));
	const branches = $derived<BranchInfo[]>(branchesQuery.data?.branches ?? []);
	const loadingBranches = $derived(!!selectedRepository?.value && (branchesQuery.isPending || branchesQuery.isFetching));

	$effect(() => {
		if (open) {
			selectedRepository = undefined;
			showFileBrowser = false;
			selectedTargetType = isEditMode ? normalizeTargetType(syncToEdit?.targetType) : normalizeTargetType(targetType);
			if (!isEditMode) {
				form.reset();
			}
		}
	});

	$effect(() => {
		if (!open || !syncToEdit || repositories.length === 0) return;
		const repo = repositories.find((r) => r.id === syncToEdit.repositoryId);
		if (repo) {
			selectedRepository = { value: repo.id, label: repo.name };
			$inputs.repositoryId.value = repo.id;
		}
	});

	$effect(() => {
		if (!open || isEditMode || branches.length === 0) return;
		const defaultBranch = branches.find((b) => b.isDefault);
		if (defaultBranch && !$inputs.branch.value) {
			$inputs.branch.value = defaultBranch.name;
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
			targetType: selectedTargetType,
			projectName: data.name,
			syncDirectory: data.syncDirectory,
			maxSyncFiles: data.maxSyncFiles,
			maxSyncTotalSize: megabytesToBytesInternal(data.maxSyncTotalSizeMb),
			maxSyncBinarySize: megabytesToBytesInternal(data.maxSyncBinarySizeMb),
			autoSync: data.autoSync,
			syncInterval: data.syncInterval
		};

		onSubmit({ sync: payload, isEditMode });
	}
</script>

<ResponsiveDialog
	bind:open
	title={isEditMode ? m.git_sync_edit_title() : m.git_sync_add_title()}
	description={isEditMode ? m.common_edit_description() : m.common_add_description()}
	contentClass="sm:max-w-3xl"
>
	{#snippet children()}
		{#if loadingData}
			<div class="flex items-center justify-center py-8">
				<Spinner class="size-6" />
			</div>
		{:else}
			<form id="sync-form" onsubmit={preventDefault(handleSubmit)} class="grid gap-4 py-4">
				<div class="space-y-3">
					<div class="grid gap-3 sm:grid-cols-2">
						<FormInput
							label={m.git_sync_name()}
							type="text"
							placeholder={m.common_name_placeholder()}
							bind:input={$inputs.name}
						/>

						<SelectWithLabel
							id="targetType"
							label={m.webhook_target_type_label()}
							value={selectedTargetType}
							options={targetTypeOptions}
							onValueChange={(value) => (selectedTargetType = value as GitOpsSyncTargetType)}
						/>
					</div>

					<div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]">
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
										}
									}
								}}
							>
								<Select.Trigger id="repository" class="w-full" aria-invalid={$inputs.repositoryId.error ? 'true' : undefined}>
									<span>{selectedRepository?.label ?? m.common_select_placeholder()}</span>
								</Select.Trigger>
								<Select.Content style="width: var(--bits-select-anchor-width);">
									{#each repositories as repo (repo.id)}
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
								<div class="flex h-10 items-center gap-2 rounded-md border px-3">
									<Spinner class="size-4" />
									<span class="text-muted-foreground text-sm">{m.common_loading()}</span>
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
										{#each branches as branch (branch.name)}
											<Select.Item value={branch.name} class="truncate">
												{branch.name}
												{#if branch.isDefault}
													<span class="text-muted-foreground ml-2 text-xs">({m.common_default()})</span>
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
						</div>
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
								title={m.git_sync_browse_files_title()}
							>
								<FolderOpenIcon class="size-4" />
							</Button>
						</div>
					</div>
				</div>

				<div class="border-border/70 bg-muted/20 grid gap-3 rounded-lg border p-3 sm:grid-cols-[minmax(0,1fr)_8rem]">
					<div class="grid gap-3 sm:grid-cols-2">
						<div class="flex items-start gap-3">
							<Switch id="syncDirectorySwitch" bind:checked={$inputs.syncDirectory.value} />
							<div class="space-y-1">
								<Label for="syncDirectorySwitch" class="mb-0 text-sm leading-none font-medium">{m.git_sync_sync_files()}</Label>
								<p class="text-muted-foreground text-xs">{m.git_sync_sync_files_description()}</p>
								{#if $inputs.syncDirectory.error}
									<p class="text-destructive text-xs font-medium">{$inputs.syncDirectory.error}</p>
								{/if}
							</div>
						</div>

						<div class="flex items-start gap-3">
							<Switch id="autoSyncSwitch" bind:checked={$inputs.autoSync.value} />
							<div class="space-y-1">
								<Label for="autoSyncSwitch" class="mb-0 text-sm leading-none font-medium">{m.git_sync_auto_sync()}</Label>
								<p class="text-muted-foreground text-xs">{m.common_auto_sync_description()}</p>
								{#if $inputs.autoSync.error}
									<p class="text-destructive text-xs font-medium">{$inputs.autoSync.error}</p>
								{/if}
							</div>
						</div>
					</div>

					<div class="space-y-2 sm:max-w-32">
						<div class="space-y-1">
							<Label for="syncInterval" class="mb-0 text-sm leading-none font-medium">
								{m.git_sync_sync_interval()}
							</Label>
							<p class="text-muted-foreground text-xs">{m.git_sync_sync_interval_description()}</p>
						</div>
						<Input
							id="syncInterval"
							type="number"
							placeholder="5"
							bind:value={$inputs.syncInterval.value}
							aria-invalid={$inputs.syncInterval.error ? 'true' : undefined}
						/>
						{#if $inputs.syncInterval.error}
							<p class="text-destructive text-xs font-medium">{$inputs.syncInterval.error}</p>
						{/if}
					</div>
				</div>

				<Collapsible.Root class="group/collapsible">
					<Collapsible.Trigger
						type="button"
						class="text-muted-foreground hover:text-foreground flex w-full items-start justify-between gap-3 rounded-md px-1 py-1.5 text-left text-sm transition-colors"
					>
						<span class="min-w-0">
							<span class="block font-medium">{m.git_sync_per_sync_file_limits_title()}</span>
						</span>
						<ArrowRightIcon class="mt-0.5 size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
					</Collapsible.Trigger>
					<Collapsible.Content>
						<div class="border-border/70 mt-2 rounded-lg border border-dashed p-3">
							<p class="text-muted-foreground mb-3 text-xs">{m.git_sync_per_sync_file_limits_description()}</p>
							<div class="grid gap-3 sm:grid-cols-3">
								<FormInput
									label={m.git_sync_max_files_label()}
									type="number"
									placeholder="0"
									helpText={m.git_sync_max_files_per_sync_help()}
									bind:input={$inputs.maxSyncFiles}
								/>
								<FormInput
									label={m.git_sync_max_total_size_label()}
									type="number"
									placeholder="0"
									helpText={m.git_sync_max_total_size_per_sync_help()}
									bind:input={$inputs.maxSyncTotalSizeMb}
								/>
								<FormInput
									label={m.git_sync_max_binary_size_label()}
									type="number"
									placeholder="0"
									helpText={m.git_sync_max_binary_size_per_sync_help()}
									bind:input={$inputs.maxSyncBinarySizeMb}
								/>
							</div>
						</div>
					</Collapsible.Content>
				</Collapsible.Root>

				<Alert.Root class="border-border/70 bg-muted/20 text-muted-foreground py-2 [&>svg]:top-1/2 [&>svg]:-translate-y-1/2">
					<InfoIcon class="size-4" />
					<Alert.Description class="text-xs">
						{m.webhook_hint_description()}
						<a href="/settings/webhooks" class="text-foreground underline">{m.webhook_page_title()}</a>
						{m.git_sync_webhook_hint_suffix()}
					</Alert.Description>
				</Alert.Root>
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

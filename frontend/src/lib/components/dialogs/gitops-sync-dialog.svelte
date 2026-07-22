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
	import GitopsDialogFooter from '$lib/components/dialogs/gitops-dialog-footer.svelte';
	import type {
		FileTreeNode,
		GitOpsSync,
		GitOpsSyncCreateDto,
		GitOpsSyncUpdateDto,
		GitRepository,
		BranchInfo
	} from '$lib/types/automation';
	import { gitRepositoryService } from '$lib/services/git-repository-service';
	import { settingsService } from '$lib/services/settings-service';
	import { hasPermission } from '$lib/utils/auth';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '$lib/utils/settings';
	import { queryKeys } from '$lib/query/query-keys';
	import { m } from '$lib/paraglide/messages';
	import { ArrowRightIcon, CodeIcon, FolderOpenIcon, InfoIcon } from '$lib/icons';
	import * as Alert from '$lib/components/ui/alert';
	import { createQuery } from '@tanstack/svelte-query';

	type GitOpsSyncFormProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		environmentId: string;
		targetType?: string;
		onSubmit: (detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		syncToEdit = $bindable(),
		environmentId,
		targetType,
		onSubmit,
		isLoading
	}: GitOpsSyncFormProps = $props();

	type GitOpsSyncTargetType = 'project' | 'swarm_stack';

	let isEditMode = $derived(!!syncToEdit);
	let showFileBrowser = $state(false);
	let fileBrowserTarget = $state<'compose' | 'preDeployScript'>('compose');

	// Pre-deploy lifecycle hooks run arbitrary containers (with host mounts, env,
	// and network access) on every sync, so configuring them is gated behind the
	// dedicated gitops:lifecycle permission rather than the broader gitops:create
	// / gitops:update. Users without it see a read-only summary instead of the
	// editable section and never submit its fields; the backend enforces the same
	// rule as defense-in-depth.
	let canManageLifecycle = $derived(hasPermission('gitops:lifecycle', environmentId));

	// Whether the sync being edited already has a hook configured. Used to flag
	// its presence to users who can't manage it and to lock the directory-sync
	// toggle so they can't accidentally invalidate the hook.
	let hookConfigured = $derived(!!syncToEdit?.preDeployScriptPath?.trim());
	let lockSyncDirectory = $derived(!canManageLifecycle && hookConfigured);

	const composeFileFilter = (file: FileTreeNode) =>
		file.type === 'file' && (file.name.endsWith('.yml') || file.name.endsWith('.yaml'));
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
		syncInterval: z.number().min(1).default(5),
		preDeployScriptPath: z.string().default(''),
		preDeployRunnerImage: z.string().default(''),
		preDeployTimeoutSec: z.coerce.number().int().positive().default(60),
		preDeployNetworkMode: z.string().default('none'),
		preDeployEnv: z.string().default(''),
		preDeployExtraMounts: z.string().default('')
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
		queryKey: queryKeys.settings.byEnvironment(environmentId),
		queryFn: () => settingsService.getSettingsForEnvironmentMerged(environmentId),
		enabled: open,
		staleTime: 0,
		refetchOnMount: 'always'
	}));
	const lifecycleEnabled = $derived(settingsQuery.data?.lifecycleEnabled ?? false);
	const lifecycleDefaultRunnerImage = $derived(settingsQuery.data?.lifecycleDefaultRunnerImage?.trim() || 'alpine:latest');

	let formData = $derived({
		name: open && syncToEdit ? syncToEdit.name : '',
		repositoryId: open && syncToEdit ? syncToEdit.repositoryId : '',
		branch: open && syncToEdit ? syncToEdit.branch : 'main',
		composePath:
			open && syncToEdit ? syncToEdit.composePath : selectedTargetType === 'swarm_stack' ? 'compose.yml' : 'docker-compose.yml',
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
		syncInterval: open && syncToEdit ? (syncToEdit.syncInterval ?? 5) : 5,
		preDeployScriptPath: open && syncToEdit ? (syncToEdit.preDeployScriptPath ?? '') : '',
		preDeployRunnerImage: open && syncToEdit ? (syncToEdit.preDeployRunnerImage ?? '') : lifecycleDefaultRunnerImage,
		preDeployTimeoutSec: open && syncToEdit ? (syncToEdit.preDeployTimeoutSec ?? 60) : 60,
		preDeployNetworkMode: open && syncToEdit ? (syncToEdit.preDeployNetworkMode ?? 'none') : 'none',
		preDeployEnv: open && syncToEdit ? (syncToEdit.preDeployEnv ?? '') : '',
		preDeployExtraMounts: open && syncToEdit ? (syncToEdit.preDeployExtraMounts ?? '') : ''
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

	function normalizeLifecycleNetworkModeInternal(value: string | undefined | null): string {
		return value?.trim() || 'none';
	}

	function existingLifecycleSnapshotInternal(sync: GitOpsSync | null) {
		return {
			scriptPath: sync?.preDeployScriptPath?.trim() ?? '',
			runnerImage: sync?.preDeployRunnerImage?.trim() ?? '',
			timeoutSec: sync?.preDeployTimeoutSec ?? 60,
			networkMode: normalizeLifecycleNetworkModeInternal(sync?.preDeployNetworkMode),
			env: sync?.preDeployEnv?.trim() ?? '',
			extraMounts: sync?.preDeployExtraMounts?.trim() ?? ''
		};
	}

	function shouldSubmitLifecycleFieldsInternal(data: z.infer<typeof formSchema>): boolean {
		if (!lifecycleEnabled || !canManageLifecycle || selectedTargetType !== 'project') {
			return false;
		}

		const scriptPath = data.preDeployScriptPath.trim();
		if (!isEditMode) {
			return scriptPath !== '';
		}

		const existing = existingLifecycleSnapshotInternal(syncToEdit);
		if (scriptPath === '' && existing.scriptPath === '') {
			return false;
		}

		return (
			scriptPath !== existing.scriptPath ||
			data.preDeployRunnerImage.trim() !== existing.runnerImage ||
			data.preDeployTimeoutSec !== existing.timeoutSec ||
			normalizeLifecycleNetworkModeInternal(data.preDeployNetworkMode) !== existing.networkMode ||
			data.preDeployEnv.trim() !== existing.env ||
			data.preDeployExtraMounts.trim() !== existing.extraMounts
		);
	}

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
		if (!open || !syncToEdit || repositories.length === 0 || selectedRepository) return;
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

		if (shouldSubmitLifecycleFieldsInternal(data)) {
			payload.preDeployScriptPath = data.preDeployScriptPath.trim();
			payload.preDeployRunnerImage = data.preDeployRunnerImage.trim();
			payload.preDeployTimeoutSec = data.preDeployTimeoutSec;
			payload.preDeployNetworkMode = normalizeLifecycleNetworkModeInternal(data.preDeployNetworkMode);
			payload.preDeployEnv = data.preDeployEnv.trim();
			payload.preDeployExtraMounts = data.preDeployExtraMounts.trim();
		}

		onSubmit({ sync: payload, isEditMode });
	}
</script>

{#snippet BrowseFilesButton(target: 'compose' | 'preDeployScript')}
	<Button
		type="button"
		variant="outline"
		size="icon"
		onclick={() => {
			fileBrowserTarget = target;
			showFileBrowser = true;
		}}
		disabled={!selectedRepository?.value || !$inputs.branch.value}
		title={m.git_sync_browse_files_title()}
	>
		<FolderOpenIcon class="size-4" />
	</Button>
{/snippet}

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
							label={m.target_type()}
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
									<span class="text-sm text-muted-foreground">{m.common_loading()}</span>
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
													<span class="ml-2 text-xs text-muted-foreground">({m.common_default()})</span>
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
								<FormInput
									type="text"
									placeholder={selectedTargetType === 'swarm_stack' ? 'compose.yml' : 'docker-compose.yml'}
									bind:input={$inputs.composePath}
								/>
							</div>
							{@render BrowseFilesButton('compose')}
						</div>
					</div>
				</div>

				<div class="grid gap-3 rounded-lg border border-border/70 bg-muted/20 p-3 sm:grid-cols-[minmax(0,1fr)_8rem]">
					<div class="grid gap-3 sm:grid-cols-2">
						<div class="flex items-start gap-3">
							<Switch id="syncDirectorySwitch" bind:checked={$inputs.syncDirectory.value} disabled={lockSyncDirectory} />
							<div class="space-y-1">
								<Label for="syncDirectorySwitch" class="mb-0 text-sm leading-none font-medium">{m.git_sync_sync_files()}</Label>
								<p class="text-xs text-muted-foreground">{m.git_sync_sync_files_description()}</p>
								{#if lockSyncDirectory}
									<p class="text-xs text-muted-foreground italic">{m.git_sync_sync_files_locked_hint()}</p>
								{/if}
								{#if $inputs.syncDirectory.error}
									<p class="text-xs font-medium text-destructive">{$inputs.syncDirectory.error}</p>
								{/if}
							</div>
						</div>

						<div class="flex items-start gap-3">
							<Switch id="autoSyncSwitch" bind:checked={$inputs.autoSync.value} />
							<div class="space-y-1">
								<Label for="autoSyncSwitch" class="mb-0 text-sm leading-none font-medium">{m.git_sync_auto_sync()}</Label>
								<p class="text-xs text-muted-foreground">{m.common_auto_sync_description()}</p>
								{#if $inputs.autoSync.error}
									<p class="text-xs font-medium text-destructive">{$inputs.autoSync.error}</p>
								{/if}
							</div>
						</div>
					</div>

					<div class="space-y-2 sm:max-w-32">
						<div class="space-y-1">
							<Label for="syncInterval" class="mb-0 text-sm leading-none font-medium">
								{m.git_sync_sync_interval()}
							</Label>
							<p class="text-xs text-muted-foreground">{m.git_sync_sync_interval_description()}</p>
						</div>
						<Input
							id="syncInterval"
							type="number"
							placeholder="5"
							bind:value={$inputs.syncInterval.value}
							aria-invalid={$inputs.syncInterval.error ? 'true' : undefined}
						/>
						{#if $inputs.syncInterval.error}
							<p class="text-xs font-medium text-destructive">{$inputs.syncInterval.error}</p>
						{/if}
					</div>
				</div>

				<Collapsible.Root class="group/collapsible">
					<Collapsible.Trigger
						type="button"
						class="flex w-full items-start justify-between gap-3 rounded-md px-1 py-1.5 text-left text-sm text-muted-foreground transition-colors hover:text-foreground"
					>
						<span class="min-w-0">
							<span class="block font-medium">{m.git_sync_per_sync_file_limits_title()}</span>
						</span>
						<ArrowRightIcon class="mt-0.5 size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
					</Collapsible.Trigger>
					<Collapsible.Content>
						<div class="mt-2 rounded-lg border border-dashed border-border/70 p-3">
							<p class="mb-3 text-xs text-muted-foreground">{m.git_sync_per_sync_file_limits_description()}</p>
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

				{#if lifecycleEnabled && canManageLifecycle && selectedTargetType === 'project'}
					<Collapsible.Root class="group/collapsible">
						<Collapsible.Trigger
							type="button"
							class="flex w-full items-start justify-between gap-3 rounded-md px-1 py-1.5 text-left text-sm text-muted-foreground transition-colors hover:text-foreground"
						>
							<span class="min-w-0">
								<span class="block font-medium">{m.git_sync_pre_deploy_title()}</span>
							</span>
							<ArrowRightIcon class="mt-0.5 size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
						</Collapsible.Trigger>
						<Collapsible.Content>
							<div class="mt-2 space-y-3 rounded-lg border border-dashed border-border/70 p-3">
								<Alert.Root
									class="border-amber-500/30 bg-amber-500/5 text-amber-700 dark:text-amber-300 [&>svg]:top-1/2 [&>svg]:-translate-y-1/2"
								>
									<InfoIcon class="size-4" />
									<Alert.Description class="text-xs">
										{m.git_sync_pre_deploy_acknowledgement()}
									</Alert.Description>
								</Alert.Root>

								<p class="text-xs text-muted-foreground">{m.git_sync_pre_deploy_description()}</p>

								<div class="space-y-1.5">
									<Label for="preDeployScriptPath" class="text-sm font-medium">
										{m.git_sync_pre_deploy_script_path_label()}
									</Label>
									<div class="flex gap-2">
										<div class="flex-1">
											<Input
												id="preDeployScriptPath"
												type="text"
												placeholder={m.git_sync_pre_deploy_script_path_placeholder()}
												bind:value={$inputs.preDeployScriptPath.value}
												aria-invalid={$inputs.preDeployScriptPath.error ? 'true' : undefined}
											/>
										</div>
										{@render BrowseFilesButton('preDeployScript')}
									</div>
									<p class="text-xs text-muted-foreground">{m.git_sync_pre_deploy_script_path_help()}</p>
									{#if $inputs.preDeployScriptPath.error}
										<p class="text-xs font-medium text-destructive">{$inputs.preDeployScriptPath.error}</p>
									{/if}
								</div>

								<FormInput
									label={m.git_sync_pre_deploy_runner_image_label()}
									type="text"
									placeholder={m.git_sync_pre_deploy_runner_image_placeholder()}
									helpText={m.git_sync_pre_deploy_runner_image_help()}
									bind:input={$inputs.preDeployRunnerImage}
								/>

								<div class="grid gap-3 sm:grid-cols-2">
									<FormInput
										label={m.git_sync_pre_deploy_timeout_label()}
										type="number"
										placeholder="60"
										helpText={m.git_sync_pre_deploy_timeout_help()}
										bind:input={$inputs.preDeployTimeoutSec}
									/>
									<FormInput
										label={m.resource_network_cap()}
										type="text"
										placeholder={m.git_sync_pre_deploy_network_mode_placeholder()}
										helpText={m.git_sync_pre_deploy_network_mode_help()}
										bind:input={$inputs.preDeployNetworkMode}
									/>
								</div>

								<FormInput
									type="textarea"
									label={m.git_sync_pre_deploy_env_label()}
									placeholder={m.git_sync_pre_deploy_env_placeholder()}
									helpText={m.git_sync_pre_deploy_env_help()}
									bind:input={$inputs.preDeployEnv}
									class="[&_textarea]:font-mono [&_textarea]:text-xs"
								/>

								<FormInput
									type="textarea"
									label={m.git_sync_pre_deploy_extra_mounts_label()}
									placeholder={m.git_sync_pre_deploy_extra_mounts_placeholder()}
									helpText={m.git_sync_pre_deploy_extra_mounts_help()}
									bind:input={$inputs.preDeployExtraMounts}
									class="[&_textarea]:font-mono [&_textarea]:text-xs"
								/>
							</div>
						</Collapsible.Content>
					</Collapsible.Root>
				{:else if lifecycleEnabled && canManageLifecycle}
					<div
						class="flex w-full items-start justify-between gap-3 rounded-md px-1 py-1.5 text-left text-sm text-muted-foreground"
					>
						<span class="min-w-0">
							<span class="block font-medium">{m.git_sync_pre_deploy_title()}</span>
							<span class="text-xs">{m.git_sync_pre_deploy_swarm_unsupported()}</span>
						</span>
					</div>
				{:else if lifecycleEnabled}
					<div
						class="flex w-full items-start justify-between gap-3 rounded-md px-1 py-1.5 text-left text-sm text-muted-foreground"
					>
						<span class="min-w-0">
							<span class="block font-medium">{m.git_sync_pre_deploy_title()}</span>
							<span class="text-xs">{m.git_sync_pre_deploy_managed_hint()}</span>
						</span>
						<span class="mt-0.5 inline-flex shrink-0 items-center gap-1.5 text-xs">
							{#if hookConfigured}
								<CodeIcon class="size-3.5" />
								{m.git_sync_pre_deploy_status_configured()}
							{:else}
								{m.git_sync_pre_deploy_status_none()}
							{/if}
						</span>
					</div>
				{/if}

				<Alert.Root class="border-border/70 bg-muted/20 py-2 text-muted-foreground [&>svg]:top-1/2 [&>svg]:-translate-y-1/2">
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
		<GitopsDialogFooter cancelLabel={m.common_cancel()} {isLoading} onCancel={() => (open = false)}>
			{#snippet primary()}
				<Button type="submit" form="sync-form" class="arcane-button-create flex-1" disabled={isLoading}>
					{#if isLoading}
						<Spinner class="mr-2 size-4" />
					{/if}
					{isEditMode ? m.common_save_changes() : m.common_add_button({ resource: m.resource_sync_cap() })}
				</Button>
			{/snippet}
		</GitopsDialogFooter>
	{/snippet}
</ResponsiveDialog>

{#snippet composeBadge(file: FileTreeNode)}
	{#if composeFileFilter(file)}
		<span class="ml-auto rounded bg-primary/10 px-2 py-0.5 text-xs text-primary">
			{m.compose()}
		</span>
	{/if}
{/snippet}

{#snippet composeFooterHint()}
	<p class="text-xs text-muted-foreground">{m.git_sync_browse_hint()}</p>
{/snippet}

<FileBrowserDialog
	bind:open={showFileBrowser}
	repositoryId={selectedRepository?.value || ''}
	branch={$inputs.branch.value}
	description={fileBrowserTarget === 'preDeployScript'
		? m.git_sync_browse_files_description_script()
		: m.git_sync_browse_files_description()}
	rootPath={fileBrowserTarget === 'preDeployScript'
		? $inputs.composePath.value.includes('/')
			? $inputs.composePath.value.replace(/\/[^/]*$/, '')
			: ''
		: ''}
	fileFilter={fileBrowserTarget === 'preDeployScript' ? undefined : composeFileFilter}
	fileBadge={fileBrowserTarget === 'preDeployScript' ? undefined : composeBadge}
	footerHint={fileBrowserTarget === 'preDeployScript' ? undefined : composeFooterHint}
	onSelect={(path) => {
		if (fileBrowserTarget === 'preDeployScript') {
			$inputs.preDeployScriptPath.value = path;
		} else {
			$inputs.composePath.value = path;
		}
	}}
/>

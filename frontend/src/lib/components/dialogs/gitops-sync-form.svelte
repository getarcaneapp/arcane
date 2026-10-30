<script lang="ts" module>
	import { z } from 'zod/v4';

	import { m } from '#lib/paraglide/messages.js';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	export function gitOpsSyncFormSchema(isBackupMode: () => boolean) {
		return z
			.object({
				name: z.string().default(''),
				projectId: z.string().default(''),
				repositoryId: z.string().min(1, m.common_required()),
				branch: z.string().min(1, m.common_required()),
				composePath: z.string().default(''),
				backupDirectory: z.string().default(''),
				backupPaths: z.array(z.string()).default([]),
				includeEnvFile: z.boolean().default(false),
				backupOnSave: z.boolean().default(true),
				syncDirectory: z.boolean().default(false),
				pullImageAfterSync: z.boolean().default(false),
				redeployAfterSync: z.boolean().default(false),
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
			})
			.check((ctx) => {
				const value = ctx.value;
				if (isBackupMode()) {
					if (!value.projectId.trim()) {
						ctx.issues.push({ code: 'custom', message: m.project_required(), path: ['projectId'], input: value });
					}
					if (!value.backupDirectory.trim()) {
						ctx.issues.push({
							code: 'custom',
							message: m.destination_folder_required(),
							path: ['backupDirectory'],
							input: value
						});
					}
					return;
				}

				if (!value.name.trim()) {
					ctx.issues.push({ code: 'custom', message: m.common_name_required(), path: ['name'], input: value });
				}
				if (!value.composePath.trim()) {
					ctx.issues.push({ code: 'custom', message: m.common_required(), path: ['composePath'], input: value });
				}
			});
	}

	export type GitOpsSyncFormSchema = ReturnType<typeof gitOpsSyncFormSchema>;
	export type GitOpsSyncFormState = ReturnType<typeof createForm<GitOpsSyncFormSchema>>;
	export type GitOpsSyncTargetType = 'project' | 'swarm_stack';
	export type GitOpsSyncBrowseTarget = 'compose' | 'preDeployScript';
</script>

<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { RadioGroup as RadioGroupPrimitive } from 'bits-ui';
	import { untrack } from 'svelte';
	import { SvelteMap, SvelteSet } from 'svelte/reactivity';

	import FileBrowserDialog from '#lib/components/dialogs/file-browser-dialog.svelte';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { FolderOpenIcon } from '#lib/icons/index.js';
	import { gitRepositoryBranchesQuery, gitRepositoryListQuery } from '#lib/query/git-repositories.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { gitOpsSyncService } from '#lib/services/gitops-sync-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { projectWorkspaceService } from '#lib/services/project-workspace-service.js';
	import type {
		BranchInfo,
		FileTreeNode,
		GitOpsSync,
		GitOpsSyncCreate,
		GitOpsSyncMode,
		GitOpsSyncUpdate,
		GitOpsSyncWritable,
		GitRepository
	} from '#lib/types/automation.js';
	import type { Settings } from '#lib/types/settings.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { Project } from '#lib/types/swarm.js';
	import type { WorkspaceFileEntry } from '#lib/types/workspace.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { changedFields } from '#lib/utils/payload.js';
	import type { WorkspaceDisplayEntry } from '#lib/utils/workspace-files.js';

	import GitOpsSyncBackupFields from './gitops-sync-backup-fields.svelte';
	import GitOpsSyncFormFields from './gitops-sync-form-fields.svelte';
	import GitOpsSyncSourceFields from './gitops-sync-source-fields.svelte';

	type GitOpsSyncFormProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		environmentId: string;
		mode: GitOpsSyncMode;
		targetType?: string;
		projectId?: string;
		settings: Settings;
		onSubmit: (detail: { sync: GitOpsSyncCreate | GitOpsSyncUpdate; isEditMode: boolean }) => void;
	};

	let {
		open,
		syncToEdit,
		environmentId = $bindable(),
		mode = $bindable(),
		targetType,
		projectId,
		settings,
		onSubmit
	}: GitOpsSyncFormProps = $props();

	const isEditMode = $derived(!!syncToEdit);
	let showFileBrowser = $state(false);
	let fileBrowserTarget = $state<GitOpsSyncBrowseTarget>('compose');

	const canManageLifecycle = $derived(hasPermission('gitops:lifecycle', environmentId));
	const canBackup = $derived(hasPermission('gitops:backup', environmentId));
	const hookConfigured = $derived(!!syncToEdit?.preDeployScriptPath?.trim());

	const composeFileFilter = (file: FileTreeNode) =>
		file.type === 'file' && (file.name.endsWith('.yml') || file.name.endsWith('.yaml'));

	function normalizeTargetType(value?: string | null): GitOpsSyncTargetType {
		return value === 'swarm_stack' ? 'swarm_stack' : 'project';
	}

	function composePathFor(type: GitOpsSyncTargetType): string {
		return type === 'swarm_stack' ? 'compose.yml' : 'docker-compose.yml';
	}

	let selectedTargetType = $state<GitOpsSyncTargetType>(untrack(() => normalizeTargetType(syncToEdit?.targetType ?? targetType)));

	const envFileName = '.env';
	const bytesPerMegabyte = 1024 * 1024;

	function bytesToMegabytesInternal(value: number | undefined, fallback: number): number {
		if (value === undefined) return fallback;
		return Math.round(value / bytesPerMegabyte);
	}

	function megabytesToBytesInternal(value: number): number {
		return value * bytesPerMegabyte;
	}

	const lifecycleEnabled = $derived(settings.lifecycleEnabled ?? false);

	function formValues(): z.infer<GitOpsSyncFormSchema> {
		return {
			name: syncToEdit?.name ?? '',
			projectId: syncToEdit?.projectId ?? projectId ?? '',
			repositoryId: syncToEdit?.repositoryId ?? '',
			branch: syncToEdit?.branch ?? 'main',
			composePath: syncToEdit?.composePath ?? composePathFor(selectedTargetType),
			backupDirectory: syncToEdit?.backupDirectory ?? '',
			backupPaths: syncToEdit?.backupPaths ?? [],
			includeEnvFile: syncToEdit?.backupPaths?.includes(envFileName) ?? false,
			backupOnSave: syncToEdit?.backupOnSave ?? true,
			syncDirectory: syncToEdit?.syncDirectory ?? false,
			pullImageAfterSync: syncToEdit?.pullImageAfterSync ?? false,
			redeployAfterSync: syncToEdit?.redeployAfterSync ?? false,
			maxSyncFiles: syncToEdit ? (syncToEdit.maxSyncFiles ?? 0) : (settings.gitSyncMaxFiles ?? 0),
			maxSyncTotalSizeMb: syncToEdit
				? bytesToMegabytesInternal(syncToEdit.maxSyncTotalSize, 0)
				: (settings.gitSyncMaxTotalSizeMb ?? 0),
			maxSyncBinarySizeMb: syncToEdit
				? bytesToMegabytesInternal(syncToEdit.maxSyncBinarySize, 0)
				: (settings.gitSyncMaxBinarySizeMb ?? 0),
			autoSync: syncToEdit?.autoSync ?? true,
			syncInterval: syncToEdit?.syncInterval ?? 5,
			preDeployScriptPath: syncToEdit?.preDeployScriptPath ?? '',
			preDeployRunnerImage: syncToEdit
				? (syncToEdit.preDeployRunnerImage ?? '')
				: settings.lifecycleDefaultRunnerImage?.trim() || 'alpine:latest',
			preDeployTimeoutSec: syncToEdit?.preDeployTimeoutSec ?? 60,
			preDeployNetworkMode: syncToEdit?.preDeployNetworkMode ?? 'none',
			preDeployEnv: syncToEdit?.preDeployEnv ?? '',
			preDeployExtraMounts: syncToEdit?.preDeployExtraMounts ?? ''
		};
	}

	const form = createForm<GitOpsSyncFormSchema>(
		gitOpsSyncFormSchema(() => mode === 'backup'),
		untrack(formValues)
	);
	const inputs = form.inputs;

	const repositoriesQuery = createQuery(() => ({
		...gitRepositoryListQuery({ pagination: { page: 1, limit: 100 } }),
		enabled: open
	}));
	const repositories = $derived<GitRepository[]>(repositoriesQuery.data?.data ?? []);
	const loadingRepositories = $derived(repositoriesQuery.isPending || repositoriesQuery.isFetching);

	const selectedRepository = $derived.by(() => {
		let repository = repositories.find((item) => item.id === inputs.repositoryId.value);
		if (!repository && !isEditMode && repositories.length === 1) repository = repositories[0];
		if (!repository) return undefined;
		return { value: repository.id, label: repository.name };
	});

	const branchesQuery = createQuery(() => {
		const repositoryId = selectedRepository?.value || '';
		return {
			...gitRepositoryBranchesQuery(repositoryId),
			enabled: open && !!repositoryId
		};
	});
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

	function shouldSubmitLifecycleFieldsInternal(data: z.infer<GitOpsSyncFormSchema>): boolean {
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

	const selectedBranch = $derived.by(() => {
		const value = inputs.branch.value;
		if (isEditMode || branches.length === 0 || branches.some((branch) => branch.name === value)) return value;
		const fallback = branches.find((branch) => branch.isDefault) ?? (branches.length === 1 ? branches[0] : undefined);
		return fallback?.name ?? value;
	});

	const isBackupMode = $derived(mode === 'backup');

	const allDirectionOptions = [
		{ value: 'deploy', label: m.pull(), description: m.deploy_from_git_description() },
		{ value: 'backup', label: m.push(), description: m.back_up_to_git_description() }
	] satisfies { value: GitOpsSyncMode; label: string; description: string }[];
	const directionOptions = $derived(allDirectionOptions.filter((option) => option.value !== 'backup' || canBackup || isEditMode));
	const selectedDirection = $derived(directionOptions.find((option) => option.value === mode));

	const projectListOptions: SearchPaginationSortRequest = {
		pagination: { page: 1, limit: 200 },
		sort: { column: 'name', direction: 'asc' }
	};

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(environmentId, projectListOptions),
		queryFn: () => projectService.getProjectsForEnvironment(environmentId, projectListOptions),
		enabled: open
	}));

	const backupSyncListOptions: SearchPaginationSortRequest = {
		pagination: { page: 1, limit: -1 },
		filters: { mode: 'backup' }
	};

	const backupSyncsQuery = createQuery(() => ({
		queryKey: queryKeys.gitOpsSyncs.list(environmentId, backupSyncListOptions),
		queryFn: () => gitOpsSyncService.getSyncs(environmentId, backupSyncListOptions),
		enabled: open && isBackupMode && !isEditMode
	}));
	const backedUpProjectIds = $derived(
		new Set((backupSyncsQuery.data?.data ?? []).map((sync) => sync.projectId).filter((id): id is string => !!id))
	);

	const projects = $derived<Project[]>(projectsQuery.data?.data ?? []);
	const projectOptions = $derived(projects.map((project) => ({ value: project.id, label: project.name })));
	const backupProjectOptions = $derived(
		isEditMode ? projectOptions : projectOptions.filter((option) => !backedUpProjectIds.has(option.value))
	);
	const selectedProjectId = $derived.by(() => {
		const value = inputs.projectId.value;
		if (!isBackupMode || isEditMode || backupProjectOptions.some((option) => option.value === value)) return value;
		return backupProjectOptions.length === 1 ? (backupProjectOptions[0]?.value ?? '') : '';
	});
	const selectedProject = $derived(projects.find((project) => project.id === selectedProjectId));
	const suggestedBackupDirectory = $derived(selectedProject?.name ?? '');
	const effectiveBackupDirectory = $derived(inputs.backupDirectory.value.trim() || suggestedBackupDirectory);

	const workspaceQuery = createQuery(() => {
		const requestProjectId = selectedProjectId;
		return {
			queryKey: queryKeys.projects.workspace(environmentId, requestProjectId),
			queryFn: () => projectWorkspaceService.getWorkspace(requestProjectId, environmentId),
			enabled: open && isBackupMode && !!requestProjectId
		};
	});
	const workspaceFiles = $derived<WorkspaceFileEntry[]>(workspaceQuery.data?.files ?? []);
	const loadingWorkspace = $derived(!!selectedProjectId && (workspaceQuery.isPending || workspaceQuery.isFetching));

	function isEnvFileInternal(name: string): boolean {
		const lowered = name.toLowerCase();
		return lowered === envFileName || lowered.startsWith('.env.') || lowered.endsWith('.env');
	}

	function isEnvPathInternal(path: string): boolean {
		return isEnvFileInternal(path.split('/').pop() ?? '');
	}

	const initialBackupPaths = $derived.by<ReadonlySet<string>>(() => {
		const stored = syncToEdit?.projectId === selectedProjectId ? (syncToEdit?.backupPaths ?? []) : [];
		return new Set(stored.filter((path) => path !== envFileName));
	});
	const selectionOverrides = new SvelteMap<string, SvelteSet<string>>();
	const selectedPaths = $derived<ReadonlySet<string>>(selectionOverrides.get(selectedProjectId) ?? initialBackupPaths);

	function editableSelectionInternal(): SvelteSet<string> {
		const existing = selectionOverrides.get(selectedProjectId);
		if (existing) return existing;
		const created = new SvelteSet(initialBackupPaths);
		selectionOverrides.set(selectedProjectId, created);
		return created;
	}

	function ancestorSelectedInternal(path: string): boolean {
		const segments = path.split('/');
		for (let index = 1; index < segments.length; index++) {
			if (selectedPaths.has(segments.slice(0, index).join('/'))) return true;
		}
		return false;
	}

	function isNodeCheckedInternal(node: WorkspaceDisplayEntry): boolean {
		if (selectedPaths.has(node.relativePath)) return true;
		if (!node.isDirectory && isEnvFileInternal(node.name)) return false;
		return ancestorSelectedInternal(node.relativePath);
	}

	function isNodeLockedInternal(node: WorkspaceDisplayEntry): boolean {
		if (!node.isDirectory && isEnvFileInternal(node.name)) return false;
		return ancestorSelectedInternal(node.relativePath);
	}

	function toggleNodeInternal(node: WorkspaceDisplayEntry, checked: boolean) {
		if (isNodeLockedInternal(node)) return;
		const selection = editableSelectionInternal();
		if (!checked) {
			selection.delete(node.relativePath);
			return;
		}
		selection.add(node.relativePath);
		if (!node.isDirectory) return;
		for (const path of selection) {
			if (path !== node.relativePath && path.startsWith(`${node.relativePath}/`) && !isEnvPathInternal(path)) {
				selection.delete(path);
			}
		}
	}

	const backupPaths = $derived(
		[...selectedPaths, ...(inputs.includeEnvFile.value ? [envFileName] : [])].sort((a, b) => a.localeCompare(b))
	);
	const hasSelectedEnvFile = $derived([...selectedPaths].some((path) => isEnvPathInternal(path)));
	const backupNamePlaceholder = $derived(m.project_backup_name({ name: selectedProject?.name ?? '' }));
	const backupSummaryDestination = $derived(
		`${selectedRepository?.label ?? ''} · ${selectedBranch} / ${effectiveBackupDirectory}`
	);
	const showBackupSummary = $derived(!!selectedProject && !!selectedRepository && backupPaths.length > 0);

	function handleTargetTypeChange(value: GitOpsSyncTargetType) {
		const previousDefault = composePathFor(selectedTargetType);
		selectedTargetType = value;
		if (value === 'swarm_stack') inputs.projectId.value = '';
		if (inputs.composePath.value === previousDefault) {
			inputs.composePath.value = composePathFor(value);
		}
	}

	function buildBackupPayloadInternal(data: z.infer<GitOpsSyncFormSchema>): GitOpsSyncCreate | GitOpsSyncUpdate {
		const name = data.name.trim() || backupNamePlaceholder;
		const repositoryId = selectedRepository?.value || data.repositoryId;

		if (!isEditMode || !syncToEdit) {
			return {
				name,
				mode: 'backup',
				projectId: data.projectId,
				repositoryId,
				branch: data.branch,
				backupDirectory: data.backupDirectory.trim(),
				backupPaths,
				backupOnSave: data.backupOnSave,
				autoSync: data.autoSync,
				syncInterval: data.syncInterval
			};
		}

		return changedFields(
			{ ...syncToEdit, backupPaths: [...(syncToEdit.backupPaths ?? [])].sort((a, b) => a.localeCompare(b)) },
			{
				name,
				repositoryId,
				branch: data.branch,
				autoSync: data.autoSync,
				syncInterval: data.syncInterval,
				backupOnSave: data.backupOnSave,
				backupPaths
			}
		);
	}

	function handleSubmit() {
		if (selectedRepository) inputs.repositoryId.value = selectedRepository.value;
		inputs.branch.value = selectedBranch;
		if (isBackupMode) inputs.projectId.value = selectedProjectId;
		inputs.backupPaths.value = backupPaths;
		if (!isEditMode && isBackupMode && !inputs.backupDirectory.value.trim()) {
			inputs.backupDirectory.value = suggestedBackupDirectory;
		}
		const data = form.validate();
		if (!data) return;

		if (isBackupMode) {
			onSubmit({ sync: buildBackupPayloadInternal(data), isEditMode });
			return;
		}

		const fields: GitOpsSyncCreate = {
			name: data.name,
			repositoryId: selectedRepository?.value || data.repositoryId,
			branch: data.branch,
			composePath: data.composePath,
			targetType: selectedTargetType,
			projectName: selectedProject?.name ?? data.name,
			syncDirectory: data.syncDirectory,
			pullImageAfterSync: data.pullImageAfterSync,
			redeployAfterSync: data.redeployAfterSync,
			maxSyncFiles: data.maxSyncFiles,
			maxSyncTotalSize: megabytesToBytesInternal(data.maxSyncTotalSizeMb),
			maxSyncBinarySize: megabytesToBytesInternal(data.maxSyncBinarySizeMb),
			autoSync: data.autoSync,
			syncInterval: data.syncInterval
		};

		const payload: GitOpsSyncCreate | GitOpsSyncUpdate =
			isEditMode && syncToEdit
				? changedFields<GitOpsSyncUpdate, GitOpsSyncWritable>(syncToEdit, fields)
				: { ...fields, ...(data.projectId ? { projectId: data.projectId } : {}) };

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

{#snippet browseButton(target: GitOpsSyncBrowseTarget)}
	<Button
		type="button"
		variant="outline"
		size="icon"
		onclick={() => {
			fileBrowserTarget = target;
			showFileBrowser = true;
		}}
		disabled={!selectedRepository?.value || !selectedBranch}
		title={m.git_sync_browse_files_title()}
	>
		<FolderOpenIcon class="size-4" />
	</Button>
{/snippet}

{#snippet sourceFields(showComposePath: boolean)}
	<GitOpsSyncSourceFields
		{form}
		{repositories}
		{branches}
		{loadingBranches}
		{selectedRepository}
		{selectedBranch}
		{showComposePath}
		composePlaceholder={composePathFor(selectedTargetType)}
		{browseButton}
	/>
{/snippet}

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

{#if loadingRepositories}
	<div class="flex items-center justify-center py-8">
		<Spinner class="size-6" />
	</div>
{:else}
	<form id="sync-form" onsubmit={preventDefault(handleSubmit)} class="grid gap-5 py-2">
		{#if !isEditMode}
			<EnvironmentDestinationSelect bind:value={environmentId} permission="gitops:create" variant="field" />
		{/if}
		{#if isEditMode}
			<p class="text-xs text-muted-foreground">
				<span class="font-medium text-foreground">{isBackupMode ? m.push() : m.pull()}</span>
				· {isBackupMode ? m.back_up_to_git_description() : m.deploy_from_git_description()}
			</p>
		{:else}
			<div class="space-y-2">
				<RadioGroupPrimitive.Root
					class="inline-flex max-w-full flex-wrap items-center justify-start gap-1 rounded-lg border border-border/60 bg-muted/40 p-1 text-muted-foreground"
					value={mode}
					onValueChange={(value) => (mode = value as GitOpsSyncMode)}
					aria-label={m.direction()}
				>
					{#each directionOptions as option (option.value)}
						<RadioGroupPrimitive.Item
							id={`sync-mode-${option.value}`}
							value={option.value}
							class="inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap text-muted-foreground ring-offset-background transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none data-[state=checked]:bg-primary/15 data-[state=checked]:text-primary data-[state=checked]:ring-1 data-[state=checked]:ring-primary/30 dark:data-[state=checked]:text-primary-tint"
						>
							{option.label}
						</RadioGroupPrimitive.Item>
					{/each}
				</RadioGroupPrimitive.Root>
				<p class="text-xs text-muted-foreground">{selectedDirection?.description}</p>
			</div>
		{/if}

		{#if isBackupMode}
			<GitOpsSyncBackupFields
				{form}
				{isEditMode}
				{selectedProjectId}
				{backupProjectOptions}
				{backupNamePlaceholder}
				{suggestedBackupDirectory}
				{workspaceFiles}
				{loadingWorkspace}
				isNodeChecked={isNodeCheckedInternal}
				isNodeLocked={isNodeLockedInternal}
				onToggleNode={toggleNodeInternal}
				isEnvFile={isEnvFileInternal}
				{hasSelectedEnvFile}
				{backupPaths}
				{showBackupSummary}
				{backupSummaryDestination}
				source={sourceFields}
			/>
		{:else}
			<GitOpsSyncFormFields
				{form}
				{isEditMode}
				{selectedTargetType}
				onTargetTypeChange={handleTargetTypeChange}
				{projectOptions}
				lockSyncDirectory={!canManageLifecycle && hookConfigured}
				{lifecycleEnabled}
				{canManageLifecycle}
				{hookConfigured}
				source={sourceFields}
				{browseButton}
			/>
		{/if}
	</form>
{/if}

<FileBrowserDialog
	bind:open={showFileBrowser}
	repositoryId={selectedRepository?.value || ''}
	branch={selectedBranch}
	description={fileBrowserTarget === 'preDeployScript'
		? m.git_sync_browse_files_description_script()
		: m.git_sync_browse_files_description()}
	rootPath={fileBrowserTarget === 'preDeployScript' && inputs.composePath.value.includes('/')
		? inputs.composePath.value.slice(0, inputs.composePath.value.lastIndexOf('/'))
		: ''}
	fileFilter={fileBrowserTarget === 'preDeployScript' ? undefined : composeFileFilter}
	fileBadge={fileBrowserTarget === 'preDeployScript' ? undefined : composeBadge}
	footerHint={fileBrowserTarget === 'preDeployScript' ? undefined : composeFooterHint}
	onSelect={(path) => {
		if (fileBrowserTarget === 'preDeployScript') {
			inputs.preDeployScriptPath.value = path;
		} else {
			inputs.composePath.value = path;
		}
	}}
/>

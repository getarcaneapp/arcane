<script lang="ts">
	import { untrack } from 'svelte';
	import { ResponsiveDialog } from '#lib/components/ui/responsive-dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import FormInput from '#lib/components/form/form-input.svelte';
	import SelectWithLabel from '#lib/components/form/select-with-label.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import FileBrowserDialog from '#lib/components/dialogs/file-browser-dialog.svelte';
	import GitopsDialogFooter from '#lib/components/dialogs/gitops-dialog-footer.svelte';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { RadioGroup as RadioGroupPrimitive } from 'bits-ui';
	import { SvelteMap, SvelteSet } from 'svelte/reactivity';
	import type {
		FileTreeNode,
		GitOpsSync,
		GitOpsSyncCreateDto,
		GitOpsSyncMode,
		GitOpsSyncUpdateDto,
		GitRepository,
		BranchInfo
	} from '#lib/types/automation.js';
	import type { Project } from '#lib/types/swarm.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { WorkspaceFileEntry } from '#lib/types/workspace.js';
	import { gitRepositoryService } from '#lib/services/git-repository-service.js';
	import { settingsService } from '#lib/services/settings-service.js';
	import { projectService } from '#lib/services/project-service.js';
	import { projectWorkspaceService } from '#lib/services/project-workspace-service.js';
	import { environmentManagementService } from '#lib/services/env-mgmt-service.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { cn } from '#lib/utils.js';
	import { z } from 'zod/v4';
	import { createForm, preventDefault } from '#lib/utils/settings.svelte.js';

	import { queryKeys } from '#lib/query/query-keys.js';
	import { m } from '#lib/paraglide/messages.js';
	import { ArrowRightIcon, CodeIcon, FolderOpenIcon, InfoIcon } from '#lib/icons/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { createQuery } from '@tanstack/svelte-query';

	type GitOpsSyncFormProps = {
		open: boolean;
		syncToEdit: GitOpsSync | null;
		environmentId: string;
		targetType?: string;
		mode?: GitOpsSyncMode;
		projectId?: string;
		onSubmit: (detail: { sync: GitOpsSyncCreateDto | GitOpsSyncUpdateDto; isEditMode: boolean }) => void;
		isLoading: boolean;
	};

	let {
		open = $bindable(false),
		syncToEdit = $bindable(),
		environmentId,
		targetType,
		mode,
		projectId,
		onSubmit,
		isLoading
	}: GitOpsSyncFormProps = $props();

	type BackupFileNode = {
		name: string;
		path: string;
		isDirectory: boolean;
		children: BackupFileNode[];
	};

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
	let selectedTargetType = $state<GitOpsSyncTargetType>(untrack(() => normalizeTargetType(syncToEdit?.targetType ?? targetType)));

	const defaultComposePath = $derived.by(() => {
		if (selectedTargetType === 'swarm_stack') return 'compose.yml';
		return 'docker-compose.yml';
	});

	const targetTypeOptions = [
		{ value: 'project', label: m.project() },
		{ value: 'swarm_stack', label: m.swarm_stack() }
	] satisfies { value: GitOpsSyncTargetType; label: string; description?: string }[];

	function normalizeTargetType(value?: string | null): GitOpsSyncTargetType {
		return value === 'swarm_stack' ? 'swarm_stack' : 'project';
	}

	const formSchema = z
		.object({
			mode: z.enum(['deploy', 'backup']).default('deploy'),
			name: z.string().default(''),
			projectId: z.string().default(''),
			repositoryId: z.string().min(1, m.common_required()),
			branch: z.string().min(1, m.common_required()),
			composePath: z.string().default(''),
			backupDirectory: z.string().default(''),
			backupPaths: z.array(z.string()).default([]),
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
			if (value.mode === 'backup') {
				if (!value.projectId.trim()) {
					ctx.issues.push({ code: 'custom', message: m.project_required(), path: ['projectId'], input: value });
				}
				const directory = value.backupDirectory.trim();
				if (!directory) {
					ctx.issues.push({ code: 'custom', message: m.destination_folder_required(), path: ['backupDirectory'], input: value });
				} else if (directory.startsWith('/') || directory.includes('..') || /^[a-zA-Z]:/.test(directory)) {
					ctx.issues.push({ code: 'custom', message: m.destination_folder_invalid(), path: ['backupDirectory'], input: value });
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

	const bytesPerMegabyte = 1024 * 1024;

	function bytesToMegabytesInternal(value: number | undefined, fallback: number): number {
		if (value === undefined) return fallback;
		return Math.round(value / bytesPerMegabyte);
	}

	function megabytesToBytesInternal(value: number): number {
		return value * bytesPerMegabyte;
	}

	const settingsQuery = createQuery(() => {
		const requestEnvironmentId = environmentId;
		return {
			queryKey: queryKeys.settings.byEnvironment(requestEnvironmentId),
			queryFn: () => settingsService.getSettingsForEnvironmentMerged(requestEnvironmentId),
			enabled: open,
			staleTime: 0,
			refetchOnMount: 'always'
		};
	});
	const lifecycleEnabled = $derived(settingsQuery.data?.lifecycleEnabled ?? false);
	const lifecycleDefaultRunnerImage = $derived(settingsQuery.data?.lifecycleDefaultRunnerImage?.trim() || 'alpine:latest');

	const formData = $derived.by(() => {
		// New drafts use the first settings response; later refreshes preserve edits.
		if (!untrack(() => isEditMode)) settingsQuery.isFetchedAfterMount;
		return untrack(() => {
			let maxSyncFiles = settingsQuery.data?.gitSyncMaxFiles ?? 0;
			let maxSyncTotalSizeMb = settingsQuery.data?.gitSyncMaxTotalSizeMb ?? 0;
			let maxSyncBinarySizeMb = settingsQuery.data?.gitSyncMaxBinarySizeMb ?? 0;
			let preDeployRunnerImage = lifecycleDefaultRunnerImage;
			if (syncToEdit) {
				maxSyncFiles = syncToEdit.maxSyncFiles ?? 0;
				maxSyncTotalSizeMb = bytesToMegabytesInternal(syncToEdit.maxSyncTotalSize, 0);
				maxSyncBinarySizeMb = bytesToMegabytesInternal(syncToEdit.maxSyncBinarySize, 0);
				preDeployRunnerImage = syncToEdit.preDeployRunnerImage ?? '';
			}
			return {
				mode: syncToEdit?.mode ?? mode ?? 'deploy',
				name: syncToEdit?.name ?? '',
				projectId: syncToEdit?.projectId ?? projectId ?? '',
				repositoryId: syncToEdit?.repositoryId ?? '',
				branch: syncToEdit?.branch ?? 'main',
				composePath: syncToEdit?.composePath ?? defaultComposePath,
				backupDirectory: syncToEdit?.backupDirectory ?? '',
				backupPaths: syncToEdit?.backupPaths ?? [],
				backupOnSave: syncToEdit?.backupOnSave ?? true,
				syncDirectory: syncToEdit?.syncDirectory ?? false,
				pullImageAfterSync: syncToEdit?.pullImageAfterSync ?? false,
				redeployAfterSync: syncToEdit?.redeployAfterSync ?? false,
				maxSyncFiles,
				maxSyncTotalSizeMb,
				maxSyncBinarySizeMb,
				autoSync: syncToEdit?.autoSync ?? true,
				syncInterval: syncToEdit?.syncInterval ?? 5,
				preDeployScriptPath: syncToEdit?.preDeployScriptPath ?? '',
				preDeployRunnerImage,
				preDeployTimeoutSec: syncToEdit?.preDeployTimeoutSec ?? 60,
				preDeployNetworkMode: syncToEdit?.preDeployNetworkMode ?? 'none',
				preDeployEnv: syncToEdit?.preDeployEnv ?? '',
				preDeployExtraMounts: syncToEdit?.preDeployExtraMounts ?? ''
			};
		});
	});

	let form = $derived(createForm<typeof formSchema>(formSchema, formData));
	let inputs = $derived(form.inputs);

	const selectedRepository = $derived.by(() => {
		const repository = repositories.find((item) => item.id === inputs.repositoryId.value);
		if (!repository) return undefined;
		return { value: repository.id, label: repository.name };
	});
	const repositoriesQuery = createQuery(() => ({
		queryKey: queryKeys.gitRepositories.syncDialog(),
		queryFn: () => gitRepositoryService.getRepositories({ pagination: { page: 1, limit: 100 } }),
		enabled: open,
		staleTime: 0
	}));
	const repositories = $derived<GitRepository[]>(repositoriesQuery.data?.data ?? []);
	const loadingSettings = $derived(!isEditMode && (settingsQuery.isPending || settingsQuery.isFetching));
	const loadingData = $derived(repositoriesQuery.isPending || repositoriesQuery.isFetching || loadingSettings);

	const branchesQuery = createQuery(() => {
		const repositoryId = selectedRepository?.value || '';
		return {
			queryKey: queryKeys.gitRepositories.branches(repositoryId),
			queryFn: () => gitRepositoryService.getBranches(repositoryId),
			enabled: open && !!repositoryId,
			staleTime: 0
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

	const selectedBranch = $derived.by(() => {
		if (inputs.branch.value || isEditMode) return inputs.branch.value;
		return branches.find((branch) => branch.isDefault)?.name ?? '';
	});

	const selectedMode = $derived<GitOpsSyncMode>(inputs.mode.value as GitOpsSyncMode);
	const isBackupMode = $derived(selectedMode === 'backup');

	const directionOptions = [
		{ value: 'deploy', label: m.deploy_from_git(), description: m.deploy_from_git_description() },
		{ value: 'backup', label: m.back_up_to_git(), description: m.back_up_to_git_description() }
	] satisfies { value: GitOpsSyncMode; label: string; description: string }[];
	const selectedDirection = $derived(directionOptions.find((option) => option.value === selectedMode));

	const projectListOptions: SearchPaginationSortRequest = {
		pagination: { page: 1, limit: 200 },
		sort: { column: 'name', direction: 'asc' }
	};

	const projectsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.list(environmentId, projectListOptions),
		queryFn: () => projectService.getProjectsForEnvironment(environmentId, projectListOptions),
		enabled: open,
		staleTime: 0
	}));
	const projects = $derived<Project[]>(projectsQuery.data?.data ?? []);
	const selectedProjectId = $derived(inputs.projectId.value);
	const selectedProject = $derived(projects.find((project) => project.id === selectedProjectId));
	const newProjectOption = '__new__';
	const projectOptions = $derived(projects.map((project) => ({ value: project.id, label: project.name })));
	const deployProjectOptions = $derived([{ value: newProjectOption, label: m.create_new_project() }, ...projectOptions]);

	const environmentQuery = createQuery(() => ({
		queryKey: queryKeys.environments.detail(environmentId),
		queryFn: () => environmentManagementService.get(environmentId),
		enabled: open && isBackupMode,
		staleTime: 0
	}));

	function slugifyInternal(value: string): string {
		return value.trim().replace(/\s+/g, '-');
	}

	const suggestedBackupDirectory = $derived.by(() => {
		const environmentSegment = slugifyInternal(environmentQuery.data?.name ?? '');
		const projectSegment = slugifyInternal(selectedProject?.name ?? '');
		if (!environmentSegment || !projectSegment) return '';
		return `projects/${environmentSegment}/${projectSegment}`;
	});

	const effectiveBackupDirectory = $derived(inputs.backupDirectory.value.trim() || suggestedBackupDirectory);

	const workspaceQuery = createQuery(() => {
		const requestProjectId = selectedProjectId;
		return {
			queryKey: queryKeys.projects.workspace(environmentId, requestProjectId),
			queryFn: () => projectWorkspaceService.getWorkspace(requestProjectId, environmentId),
			enabled: open && isBackupMode && !!requestProjectId,
			staleTime: 0
		};
	});
	const workspaceFiles = $derived<WorkspaceFileEntry[]>(workspaceQuery.data?.files ?? []);
	const loadingWorkspace = $derived(!!selectedProjectId && (workspaceQuery.isPending || workspaceQuery.isFetching));

	const composeFileNames = ['compose.yaml', 'compose.yml', 'docker-compose.yml', 'docker-compose.yaml'];

	function isComposeFileInternal(name: string): boolean {
		const lowered = name.toLowerCase();
		return composeFileNames.includes(lowered) || /^(docker-)?compose\..+\.ya?ml$/.test(lowered);
	}

	function isEnvFileInternal(name: string): boolean {
		const lowered = name.toLowerCase();
		return lowered === '.env' || lowered.startsWith('.env.') || lowered.endsWith('.env');
	}

	function isEnvPathInternal(path: string): boolean {
		return isEnvFileInternal(path.split('/').pop() ?? '');
	}

	function sortNodesInternal(nodes: BackupFileNode[]) {
		nodes.sort((a, b) => (a.isDirectory === b.isDirectory ? a.name.localeCompare(b.name) : a.isDirectory ? -1 : 1));
		for (const node of nodes) sortNodesInternal(node.children);
	}

	const fileTree = $derived.by<BackupFileNode[]>(() => {
		const roots: BackupFileNode[] = [];
		const byPath: Record<string, BackupFileNode> = {};
		const entries = [...workspaceFiles].sort((a, b) => a.relativePath.localeCompare(b.relativePath));

		for (const entry of entries) {
			const segments = entry.relativePath.split('/').filter(Boolean);
			let parentPath = '';
			segments.forEach((segment, index) => {
				const path = parentPath ? `${parentPath}/${segment}` : segment;
				const isLeaf = index === segments.length - 1;
				const existing = byPath[path];
				if (!existing) {
					const node: BackupFileNode = { name: segment, path, isDirectory: isLeaf ? entry.isDirectory : true, children: [] };
					byPath[path] = node;
					const parent = parentPath ? byPath[parentPath] : undefined;
					if (parent) parent.children.push(node);
					else roots.push(node);
				} else if (isLeaf && entry.isDirectory) {
					existing.isDirectory = true;
				}
				parentPath = path;
			});
		}

		sortNodesInternal(roots);
		return roots;
	});

	const rootFiles = $derived(workspaceFiles.filter((entry) => !entry.isDirectory && !entry.relativePath.includes('/')));

	const entrypointPath = $derived.by(() => {
		for (const candidate of composeFileNames) {
			const found = rootFiles.find((entry) => entry.name.toLowerCase() === candidate);
			if (found) return found.relativePath;
		}
		return '';
	});

	const defaultBackupPaths = $derived(
		rootFiles.filter((entry) => isComposeFileInternal(entry.name)).map((entry) => entry.relativePath)
	);

	const initialBackupPaths = $derived.by<ReadonlySet<string>>(() => {
		const stored = syncToEdit?.projectId === selectedProjectId ? (syncToEdit?.backupPaths ?? []) : [];
		const paths = new Set(stored.length > 0 ? stored : defaultBackupPaths);
		if (entrypointPath) paths.add(entrypointPath);
		return paths;
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

	function isNodeCheckedInternal(node: BackupFileNode): boolean {
		if (selectedPaths.has(node.path)) return true;
		if (!node.isDirectory && isEnvFileInternal(node.name)) return false;
		return ancestorSelectedInternal(node.path);
	}

	function isNodeLockedInternal(node: BackupFileNode): boolean {
		if (node.path === entrypointPath) return true;
		if (!node.isDirectory && isEnvFileInternal(node.name)) return false;
		return ancestorSelectedInternal(node.path);
	}

	function toggleNodeInternal(node: BackupFileNode, checked: boolean) {
		if (isNodeLockedInternal(node)) return;
		const selection = editableSelectionInternal();
		if (!checked) {
			selection.delete(node.path);
			return;
		}
		selection.add(node.path);
		if (!node.isDirectory) return;
		for (const path of [...selection]) {
			if (path !== node.path && path.startsWith(`${node.path}/`) && !isEnvPathInternal(path)) {
				selection.delete(path);
			}
		}
	}

	const backupPaths = $derived([...selectedPaths].sort((a, b) => a.localeCompare(b)));
	const hasSelectedEnvFile = $derived(backupPaths.some((path) => isEnvPathInternal(path)));
	const backupNamePlaceholder = $derived(m.project_backup_name({ name: selectedProject?.name ?? '' }));
	const backupSummaryDestination = $derived(
		`${selectedRepository?.label ?? ''} · ${selectedBranch} / ${effectiveBackupDirectory}`
	);
	const showBackupSummary = $derived(!!selectedProject && !!selectedRepository && backupPaths.length > 0);

	function backupPathsChangedInternal(): boolean {
		const existing = [...(syncToEdit?.backupPaths ?? [])].sort((a, b) => a.localeCompare(b));
		return existing.length !== backupPaths.length || existing.some((path, index) => path !== backupPaths[index]);
	}

	function buildBackupPayloadInternal(data: z.infer<typeof formSchema>): GitOpsSyncCreateDto | GitOpsSyncUpdateDto {
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

		const update: GitOpsSyncUpdateDto = {};
		if (name !== syncToEdit.name) update.name = name;
		if (repositoryId !== syncToEdit.repositoryId) update.repositoryId = repositoryId;
		if (data.branch !== syncToEdit.branch) update.branch = data.branch;
		if (data.autoSync !== syncToEdit.autoSync) update.autoSync = data.autoSync;
		if (data.syncInterval !== syncToEdit.syncInterval) update.syncInterval = data.syncInterval;
		if (data.backupOnSave !== syncToEdit.backupOnSave) update.backupOnSave = data.backupOnSave;
		if (backupPathsChangedInternal()) update.backupPaths = backupPaths;
		return update;
	}

	function handleSubmit() {
		inputs.branch.value = selectedBranch;
		inputs.backupPaths.value = backupPaths;
		if (!isEditMode && isBackupMode && !inputs.backupDirectory.value.trim()) {
			inputs.backupDirectory.value = suggestedBackupDirectory;
		}
		const data = form.validate();
		if (!data) return;

		if (data.mode === 'backup') {
			onSubmit({ sync: buildBackupPayloadInternal(data), isEditMode });
			return;
		}

		const payload: GitOpsSyncCreateDto | GitOpsSyncUpdateDto = {
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

		if (!isEditMode && data.projectId) {
			(payload as GitOpsSyncCreateDto).projectId = data.projectId;
		}

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
		disabled={!selectedRepository?.value || !selectedBranch}
		title={m.git_sync_browse_files_title()}
	>
		<FolderOpenIcon class="size-4" />
	</Button>
{/snippet}

{#snippet backupFileNode(node: BackupFileNode, depth: number)}
	{@const checked = isNodeCheckedInternal(node)}
	{@const locked = isNodeLockedInternal(node)}
	{@const isEnvFile = !node.isDirectory && isEnvFileInternal(node.name)}
	<li>
		<div class="flex items-center gap-2 rounded-md py-1" style={`padding-left: ${depth * 1.25 + 0.25}rem`}>
			<Checkbox
				id={`backup-path-${node.path}`}
				{checked}
				disabled={locked}
				onCheckedChange={(value) => toggleNodeInternal(node, value === true)}
			/>
			<Label for={`backup-path-${node.path}`} class="mb-0 flex min-w-0 flex-1 items-center gap-2 text-sm font-normal">
				<span class={cn('truncate', node.isDirectory && 'font-medium')}>{node.name}{node.isDirectory ? '/' : ''}</span>
				{#if node.path === entrypointPath}
					<span class="shrink-0 rounded bg-primary/10 px-1.5 py-0.5 text-[10px] text-primary">{m.common_entrypoint()}</span>
				{/if}
				{#if isEnvFile}
					<span class="shrink-0 rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] text-amber-600 dark:text-amber-400">
						{m.environment_file()}
					</span>
				{/if}
			</Label>
		</div>
		{#if node.children.length > 0}
			<ul class="space-y-0.5">
				{#each node.children as child (child.path)}
					{@render backupFileNode(child, depth + 1)}
				{/each}
			</ul>
		{/if}
	</li>
{/snippet}

{#snippet repositoryAndBranchFields()}
	<div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_12rem]">
		<div class="space-y-2">
			<Label for="repository">{m.git_sync_repository()}</Label>
			<Select.Root
				type="single"
				value={selectedRepository?.value}
				onValueChange={(v) => {
					if (v) {
						const repo = repositories.find((r) => r.id === v);
						if (repo) {
							inputs.repositoryId.value = v;
						}
					}
				}}
			>
				<Select.Trigger id="repository" class="w-full" aria-invalid={inputs.repositoryId.error ? 'true' : undefined}>
					<span>{selectedRepository?.label ?? m.common_select_placeholder()}</span>
				</Select.Trigger>
				<Select.Content style="width: var(--bits-select-anchor-width);">
					{#each repositories as repo (repo.id)}
						<Select.Item value={repo.id} class="truncate">{repo.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
			{#if inputs.repositoryId.error}
				<p class="mt-1 text-sm text-red-500">{inputs.repositoryId.error}</p>
			{/if}
		</div>

		<div class="space-y-2">
			<Label for="branch">{m.git_sync_branch()}</Label>
			{#if loadingBranches}
				<div class="flex h-10 items-center gap-2 rounded-md border px-3">
					<Spinner class="size-4" />
					<span class="text-sm text-muted-foreground">{m.common_loading()}</span>
				</div>
			{:else if branches.length > 0}
				<Select.Root
					type="single"
					value={selectedBranch}
					onValueChange={(v) => {
						if (v) {
							inputs.branch.value = v;
						}
					}}
				>
					<Select.Trigger id="branch" class="w-full" aria-invalid={inputs.branch.error ? 'true' : undefined}>
						<span>{selectedBranch || m.common_select_placeholder()}</span>
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
				<FormInput type="text" placeholder="main" bind:input={inputs.branch} />
			{/if}
			{#if inputs.branch.error}
				<p class="mt-1 text-sm text-red-500">{inputs.branch.error}</p>
			{/if}
		</div>
	</div>
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
			<form id="sync-form" onsubmit={preventDefault(handleSubmit)} class="grid gap-5 py-2">
				{#if isEditMode}
					<p class="text-xs text-muted-foreground">
						<span class="font-medium text-foreground">{isBackupMode ? m.back_up_to_git() : m.deploy_from_git()}</span>
						· {isBackupMode ? m.back_up_to_git_description() : m.deploy_from_git_description()}
					</p>
				{:else}
					<div class="space-y-2">
						<RadioGroup.Root
							class="inline-flex max-w-full flex-wrap items-center justify-start gap-1 rounded-lg border border-border/60 bg-muted/40 p-1 text-muted-foreground"
							value={selectedMode}
							onValueChange={(value) => (inputs.mode.value = value as GitOpsSyncMode)}
							aria-label={m.direction()}
						>
							{#each directionOptions as option (option.value)}
								<RadioGroupPrimitive.Item
									id={`sync-mode-${option.value}`}
									value={option.value}
									class="inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap text-muted-foreground ring-offset-background transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none data-[state=checked]:bg-primary/15 data-[state=checked]:text-primary data-[state=checked]:ring-1 data-[state=checked]:ring-primary/30 dark:data-[state=checked]:text-[color-mix(in_oklch,var(--primary)_55%,white)]"
								>
									{option.label}
								</RadioGroupPrimitive.Item>
							{/each}
						</RadioGroup.Root>
						<p class="text-xs text-muted-foreground">{selectedDirection?.description}</p>
					</div>
				{/if}

				{#if isBackupMode}
					<div class="grid gap-3 sm:grid-cols-2">
						<SelectWithLabel
							id="backupProject"
							label={m.project()}
							value={selectedProjectId}
							options={projectOptions}
							disabled={isEditMode}
							error={inputs.projectId.error}
							onValueChange={(value) => {
								if (value) inputs.projectId.value = value;
							}}
						/>

						<FormInput label={m.git_sync_name()} type="text" placeholder={backupNamePlaceholder} bind:input={inputs.name} />
					</div>

					{@render repositoryAndBranchFields()}

					<div class="space-y-2">
						<Label for="backupDirectory">{m.destination_folder()}</Label>
						{#if isEditMode}
							<Input id="backupDirectory" type="text" value={inputs.backupDirectory.value} readonly />
							<p class="text-xs text-muted-foreground">{m.destination_folder_locked()}</p>
						{:else}
							<Input
								id="backupDirectory"
								type="text"
								placeholder={suggestedBackupDirectory}
								bind:value={inputs.backupDirectory.value}
								aria-invalid={inputs.backupDirectory.error ? 'true' : undefined}
							/>
							<p class="text-xs text-muted-foreground">{m.destination_folder_description()}</p>
						{/if}
						{#if inputs.backupDirectory.error}
							<p class="text-xs font-medium text-destructive">{inputs.backupDirectory.error}</p>
						{/if}
					</div>

					<div class="space-y-2">
						<Label>{m.included_files()}</Label>
						<div class="max-h-56 overflow-y-auto rounded-md border border-border/50 p-1.5">
							{#if !selectedProjectId}
								<p class="px-1 py-2 text-xs text-muted-foreground">{m.project_required()}</p>
							{:else if loadingWorkspace}
								<div class="flex items-center gap-2 px-1 py-2">
									<Spinner class="size-4" />
									<span class="text-sm text-muted-foreground">{m.common_loading()}</span>
								</div>
							{:else if fileTree.length === 0}
								<p class="px-1 py-2 text-xs text-muted-foreground">{m.no_project_files()}</p>
							{:else}
								<ul class="space-y-0.5">
									{#each fileTree as node (node.path)}
										{@render backupFileNode(node, 0)}
									{/each}
								</ul>
							{/if}
						</div>
						<p class="text-xs text-muted-foreground">{m.included_files_description()}</p>
						{#if hasSelectedEnvFile}
							<p class="text-xs text-amber-600 dark:text-amber-400">{m.environment_file_warning()}</p>
						{/if}
					</div>

					<div class="divide-y divide-border/50 border-y border-border/50">
						<div class="flex items-center justify-between gap-4 py-3">
							<div class="min-w-0">
								<Label for="backupAutoSyncSwitch" class="mb-0 text-sm font-medium">{m.automatic_backup()}</Label>
								<p class="text-xs text-muted-foreground">{m.automatic_backup_description()}</p>
							</div>
							<div class="flex shrink-0 items-center gap-3">
								{#if inputs.autoSync.value}
									<label class="flex items-center gap-1.5 text-xs text-muted-foreground" for="backupSyncInterval">
										{m.every()}
										<Input
											id="backupSyncInterval"
											type="number"
											min="1"
											class="h-8 w-16 text-center"
											bind:value={inputs.syncInterval.value}
											aria-invalid={inputs.syncInterval.error ? 'true' : undefined}
										/>
										{m.minutes()}
									</label>
								{/if}
								<Switch id="backupAutoSyncSwitch" bind:checked={inputs.autoSync.value} />
							</div>
						</div>
						{#if inputs.syncInterval.error}
							<p class="pb-2 text-xs font-medium text-destructive">{inputs.syncInterval.error}</p>
						{/if}

						<div class="flex items-center justify-between gap-4 py-3">
							<div class="min-w-0">
								<Label for="backupOnSaveSwitch" class="mb-0 text-sm font-medium">{m.back_up_on_save()}</Label>
								<p class="text-xs text-muted-foreground">{m.back_up_on_save_description()}</p>
							</div>
							<Switch id="backupOnSaveSwitch" bind:checked={inputs.backupOnSave.value} disabled={!inputs.autoSync.value} />
						</div>
					</div>

					{#if showBackupSummary}
						<div class="space-y-1 text-xs text-muted-foreground">
							<p>
								{m.commits_files_to({ count: backupPaths.length })}
								<span class="font-mono break-all text-foreground">{backupSummaryDestination}</span>
							</p>
							<p class="font-mono break-all">{backupPaths.join(', ')}</p>
						</div>
					{/if}
				{:else}
					<div class="grid gap-3 sm:grid-cols-2">
						<FormInput label={m.git_sync_name()} type="text" placeholder={m.common_name_placeholder()} bind:input={inputs.name} />

						<SelectWithLabel
							id="targetType"
							label={m.target_type()}
							value={selectedTargetType}
							options={targetTypeOptions}
							onValueChange={(value) => {
								const previousDefault = defaultComposePath;
								selectedTargetType = value as GitOpsSyncTargetType;
								if (selectedTargetType === 'swarm_stack') inputs.projectId.value = '';
								if (inputs.composePath.value === previousDefault) {
									inputs.composePath.value = defaultComposePath;
								}
							}}
						/>
					</div>

					{#if !isEditMode && selectedTargetType === 'project'}
						<SelectWithLabel
							id="deployProject"
							label={m.project()}
							description={m.link_existing_project_description()}
							value={inputs.projectId.value || newProjectOption}
							options={deployProjectOptions}
							error={inputs.projectId.error}
							onValueChange={(value) => (inputs.projectId.value = value === newProjectOption ? '' : value)}
						/>
					{/if}

					{@render repositoryAndBranchFields()}

					<div class="space-y-2">
						<Label for="composePath">{m.git_sync_compose_path()}</Label>
						<div class="flex gap-2">
							<div class="flex-1">
								<FormInput
									type="text"
									placeholder={selectedTargetType === 'swarm_stack' ? 'compose.yml' : 'docker-compose.yml'}
									bind:input={inputs.composePath}
								/>
							</div>
							{@render BrowseFilesButton('compose')}
						</div>
					</div>

					<div class="divide-y divide-border/50 border-y border-border/50">
						<div class="flex items-center justify-between gap-4 py-3">
							<div class="min-w-0">
								<Label for="autoSyncSwitch" class="mb-0 text-sm font-medium">{m.git_sync_auto_sync()}</Label>
								<p class="text-xs text-muted-foreground">{m.common_auto_sync_description()}</p>
								{#if inputs.autoSync.error}
									<p class="text-xs font-medium text-destructive">{inputs.autoSync.error}</p>
								{/if}
							</div>
							<div class="flex shrink-0 items-center gap-3">
								{#if inputs.autoSync.value}
									<label class="flex items-center gap-1.5 text-xs text-muted-foreground" for="syncInterval">
										{m.every()}
										<Input
											id="syncInterval"
											type="number"
											min="1"
											class="h-8 w-16 text-center"
											bind:value={inputs.syncInterval.value}
											aria-invalid={inputs.syncInterval.error ? 'true' : undefined}
										/>
										{m.minutes()}
									</label>
								{/if}
								<Switch id="autoSyncSwitch" bind:checked={inputs.autoSync.value} />
							</div>
						</div>
						{#if inputs.syncInterval.error}
							<p class="pb-2 text-xs font-medium text-destructive">{inputs.syncInterval.error}</p>
						{/if}

						<div class="flex items-center justify-between gap-4 py-3">
							<div class="min-w-0">
								<Label for="syncDirectorySwitch" class="mb-0 text-sm font-medium">{m.git_sync_sync_files()}</Label>
								<p class="text-xs text-muted-foreground">
									{lockSyncDirectory ? m.git_sync_sync_files_locked_hint() : m.git_sync_sync_files_description()}
								</p>
								{#if inputs.syncDirectory.error}
									<p class="text-xs font-medium text-destructive">{inputs.syncDirectory.error}</p>
								{/if}
							</div>
							<Switch id="syncDirectorySwitch" bind:checked={inputs.syncDirectory.value} disabled={lockSyncDirectory} />
						</div>

						<div class="flex items-center justify-between gap-4 py-3">
							<div class="min-w-0">
								<Label for="pullImageAfterSyncSwitch" class="mb-0 text-sm font-medium">{m.git_sync_pull_image_after_sync()}</Label
								>
								<p class="text-xs text-muted-foreground">
									{inputs.redeployAfterSync.value
										? m.git_sync_pull_image_after_sync_redundant_description()
										: m.git_sync_pull_image_after_sync_description()}
								</p>
								{#if inputs.pullImageAfterSync.error}
									<p class="text-xs font-medium text-destructive">{inputs.pullImageAfterSync.error}</p>
								{/if}
							</div>
							<Switch
								id="pullImageAfterSyncSwitch"
								bind:checked={inputs.pullImageAfterSync.value}
								disabled={inputs.redeployAfterSync.value}
							/>
						</div>

						<div class="flex items-center justify-between gap-4 py-3">
							<div class="min-w-0">
								<Label for="redeployAfterSyncSwitch" class="mb-0 text-sm font-medium">{m.git_sync_redeploy_after_sync()}</Label>
								<p class="text-xs text-muted-foreground">{m.git_sync_redeploy_after_sync_description()}</p>
								{#if inputs.redeployAfterSync.error}
									<p class="text-xs font-medium text-destructive">{inputs.redeployAfterSync.error}</p>
								{/if}
							</div>
							<Switch id="redeployAfterSyncSwitch" bind:checked={inputs.redeployAfterSync.value} />
						</div>
					</div>

					<Collapsible.Root class="group/collapsible">
						<Collapsible.Trigger
							type="button"
							class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
						>
							{m.git_sync_per_sync_file_limits_title()}
							<ArrowRightIcon class="size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
						</Collapsible.Trigger>
						<Collapsible.Content>
							<div class="space-y-3 pt-3">
								<p class="text-xs text-muted-foreground">{m.git_sync_per_sync_file_limits_description()}</p>
								<div class="grid gap-3 sm:grid-cols-3">
									<FormInput
										label={m.git_sync_max_files_label()}
										type="number"
										placeholder="0"
										helpText={m.git_sync_max_files_per_sync_help()}
										bind:input={inputs.maxSyncFiles}
									/>
									<FormInput
										label={m.git_sync_max_total_size_label()}
										type="number"
										placeholder="0"
										helpText={m.git_sync_max_total_size_per_sync_help()}
										bind:input={inputs.maxSyncTotalSizeMb}
									/>
									<FormInput
										label={m.git_sync_max_binary_size_label()}
										type="number"
										placeholder="0"
										helpText={m.git_sync_max_binary_size_per_sync_help()}
										bind:input={inputs.maxSyncBinarySizeMb}
									/>
								</div>
							</div>
						</Collapsible.Content>
					</Collapsible.Root>

					{#if lifecycleEnabled && canManageLifecycle && selectedTargetType === 'project'}
						<Collapsible.Root class="group/collapsible">
							<Collapsible.Trigger
								type="button"
								class="flex w-full items-center justify-between gap-3 py-1 text-left text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
							>
								{m.git_sync_pre_deploy_title()}
								<ArrowRightIcon class="size-4 shrink-0 transition-transform group-data-[state=open]/collapsible:rotate-90" />
							</Collapsible.Trigger>
							<Collapsible.Content>
								<div class="space-y-3 pt-3">
									<Alert.Root
										class="border-amber-500/30 bg-amber-500/5 text-amber-700 dark:text-amber-300 [&>svg]:top-1/2 [&>svg]:-translate-y-1/2"
									>
										<InfoIcon class="size-4" />
										<Alert.Description class="text-xs">
											{m.git_sync_pre_deploy_acknowledgement()}
										</Alert.Description>
									</Alert.Root>

									<p class="text-xs text-muted-foreground">{m.git_sync_pre_deploy_description()}</p>

									<div class="space-y-2">
										<Label for="preDeployScriptPath" class="text-sm font-medium">
											{m.git_sync_pre_deploy_script_path_label()}
										</Label>
										<div class="flex gap-2">
											<div class="flex-1">
												<Input
													id="preDeployScriptPath"
													type="text"
													placeholder={m.git_sync_pre_deploy_script_path_placeholder()}
													bind:value={inputs.preDeployScriptPath.value}
													aria-invalid={inputs.preDeployScriptPath.error ? 'true' : undefined}
												/>
											</div>
											{@render BrowseFilesButton('preDeployScript')}
										</div>
										<p class="text-xs text-muted-foreground">{m.git_sync_pre_deploy_script_path_help()}</p>
										{#if inputs.preDeployScriptPath.error}
											<p class="text-xs font-medium text-destructive">{inputs.preDeployScriptPath.error}</p>
										{/if}
									</div>

									<FormInput
										label={m.git_sync_pre_deploy_runner_image_label()}
										type="text"
										placeholder={m.git_sync_pre_deploy_runner_image_placeholder()}
										helpText={m.git_sync_pre_deploy_runner_image_help()}
										bind:input={inputs.preDeployRunnerImage}
									/>

									<div class="grid gap-3 sm:grid-cols-2">
										<FormInput
											label={m.git_sync_pre_deploy_timeout_label()}
											type="number"
											placeholder="60"
											helpText={m.git_sync_pre_deploy_timeout_help()}
											bind:input={inputs.preDeployTimeoutSec}
										/>
										<FormInput
											label={m.resource_network_cap()}
											type="text"
											placeholder={m.git_sync_pre_deploy_network_mode_placeholder()}
											helpText={m.git_sync_pre_deploy_network_mode_help()}
											bind:input={inputs.preDeployNetworkMode}
										/>
									</div>

									<FormInput
										type="textarea"
										label={m.git_sync_pre_deploy_env_label()}
										placeholder={m.git_sync_pre_deploy_env_placeholder()}
										helpText={m.git_sync_pre_deploy_env_help()}
										bind:input={inputs.preDeployEnv}
										class="[&_textarea]:font-mono [&_textarea]:text-xs"
									/>

									<FormInput
										type="textarea"
										label={m.git_sync_pre_deploy_extra_mounts_label()}
										placeholder={m.git_sync_pre_deploy_extra_mounts_placeholder()}
										helpText={m.git_sync_pre_deploy_extra_mounts_help()}
										bind:input={inputs.preDeployExtraMounts}
										class="[&_textarea]:font-mono [&_textarea]:text-xs"
									/>
								</div>
							</Collapsible.Content>
						</Collapsible.Root>
					{:else if lifecycleEnabled && canManageLifecycle}
						<p class="text-xs text-muted-foreground">
							<span class="font-medium">{m.git_sync_pre_deploy_title()}</span> · {m.git_sync_pre_deploy_swarm_unsupported()}
						</p>
					{:else if lifecycleEnabled}
						<p class="flex items-center justify-between gap-3 text-xs text-muted-foreground">
							<span
								><span class="font-medium">{m.git_sync_pre_deploy_title()}</span> · {m.git_sync_pre_deploy_managed_hint()}</span
							>
							<span class="inline-flex shrink-0 items-center gap-1.5">
								{#if hookConfigured}
									<CodeIcon class="size-3.5" />
									{m.common_configured()}
								{:else}
									{m.common_not_configured()}
								{/if}
							</span>
						</p>
					{/if}

					<p class="text-xs text-muted-foreground">
						{m.webhook_hint_description()}
						<a href="/settings/webhooks" class="text-foreground underline">{m.webhook_page_title()}</a>
						{m.git_sync_webhook_hint_suffix()}
					</p>
				{/if}
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
					{isEditMode
						? isBackupMode
							? m.common_save()
							: m.common_save_changes()
						: isBackupMode
							? m.save_and_back_up()
							: m.common_add_button({ resource: m.resource_sync_cap() })}
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
	branch={selectedBranch}
	description={fileBrowserTarget === 'preDeployScript'
		? m.git_sync_browse_files_description_script()
		: m.git_sync_browse_files_description()}
	rootPath={fileBrowserTarget === 'preDeployScript'
		? inputs.composePath.value.includes('/')
			? inputs.composePath.value.replace(/\/[^/]*$/, '')
			: ''
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

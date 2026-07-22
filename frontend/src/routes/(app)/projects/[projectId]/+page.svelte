<script lang="ts">
	import type { IncludeFile, Project } from '$lib/types/swarm';
	import type { ProjectFileChange } from '$lib/types/project-files';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import {
		ArrowLeftIcon,
		ArrowDownIcon,
		ArrowRightIcon,
		CreateFileIcon,
		TrashIcon,
		BoxIcon,
		ProjectsIcon,
		LayersIcon,
		SettingsIcon,
		FileTextIcon,
		AlertIcon,
		GlobeIcon,
		CodeIcon,
		ArrowsUpDownIcon,
		SearchIcon
	} from '$lib/icons';
	import { type TabItem } from '$lib/components/tab-bar/index.js';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import ActionButtons from '$lib/components/action-buttons.svelte';
	import { Badge } from '$lib/components/ui/badge';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { getStatusVariant, getThemedIconUrl } from '$lib/utils/docker';
	import { capitalizeFirstLetter } from '$lib/utils/formatting';
	import { page } from '$app/state';
	import { mode } from 'mode-watcher';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/api';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { z } from 'zod/v4';
	import { createForm } from '$lib/utils/settings';
	import { m } from '$lib/paraglide/messages';
	import { toGitCommitUrl } from '$lib/utils/navigation';
	import { toSafeHref } from '$lib/utils/navigation';
	import { PersistedState } from 'runed';
	import { useUrlTab } from '$lib/hooks/use-url-tab.svelte';
	import ComposeFileEditorPanel from '$lib/components/compose-file-editor-panel.svelte';
	import EditableName from '../components/EditableName.svelte';
	import ProjectFileTreePanel from '../components/ProjectFileTreePanel.svelte';
	import EditorTabStrip from '../components/EditorTabStrip.svelte';
	import ProjectContainersTable from '../components/ProjectContainersTable.svelte';
	import CodePanel from '../components/CodePanel.svelte';
	import ProjectsLogsPanel from '../components/ProjectLogsPanel.svelte';
	import ResizableSplit from '$lib/components/resizable-split.svelte';
	import { Switch } from '$lib/components/ui/switch';
	import { untrack } from 'svelte';
	import { projectService } from '$lib/services/project-service';
	import { imageService } from '$lib/services/image-service';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { queryKeys } from '$lib/query/query-keys';
	import { RefreshIcon } from '$lib/icons';
	import IconImage from '$lib/components/icon-image.svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import ProjectUpdateItem from '$lib/components/project-update-item.svelte';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import {
		applyProjectFileChangesForDisplay,
		isProjectFileSelectionUnder,
		planProjectFileCreate,
		planProjectFileMove,
		planProjectFileRename,
		projectFileBasename,
		projectFileLanguage,
		projectFilePathMatches,
		remapProjectFileRecord,
		remapSelectedProjectFileKey,
		removeProjectFileRecord
	} from '../components/project-file-tree-utils';
	import { composeTreeSplitProps, extractComposeYamlName } from '$lib/utils/compose-flow';

	let { data } = $props();
	let projectId = $derived(data.projectId);
	const queryClient = useQueryClient();

	let isLoading = $state({
		deploying: false,
		stopping: false,
		restarting: false,
		removing: false,
		importing: false,
		redeploying: false,
		destroying: false,
		pulling: false,
		saving: false,
		syncing: false,
		archiving: false
	});

	const envId = $derived(environmentStore.selected?.id || '0');
	const canUpdateProject = $derived(hasPermission('projects:update', envId));
	const canViewProjectLogs = $derived(hasPermission('projects:logs', envId));
	// Project lifecycle permissions are evaluated per-button inside
	// <ActionButtons/> directly; no need to derive them here.

	let includeFilesState = $state<Record<string, string>>({});
	let loadedIncludeFileContents = $state<Record<string, string>>({});
	let loadedDirectoryFileContents = $state<Record<string, string>>({});
	let managedProjectFileChanges = $state<ProjectFileChange[]>([]);
	let managedProjectFileContents = $state<Record<string, string>>({});
	let loadedManagedProjectFileContents = $state<Record<string, string>>({});
	let managedProjectFileLoadErrors = $state<Record<string, string>>({});
	let managedProjectFileLoading = $state<Record<string, boolean>>({});
	let projectFilePromises: Record<string, Promise<IncludeFile> | undefined> = {};
	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));

	const projectDetailQuery = createQuery(() => ({
		queryKey: queryKeys.projects.detail(envId, projectId),
		queryFn: () => projectService.getProjectForEnvironment(envId, projectId),
		initialData: data.project,
		refetchOnMount: false
	}));

	// The file tree walk can be slow on large projects, so it loads lazily and
	// never blocks navigation; +page.ts prefetches this key without awaiting.
	const projectFilesQuery = createQuery(() => ({
		queryKey: queryKeys.projects.files(envId, projectId),
		queryFn: () => projectService.getProjectFiles(envId, projectId)
	}));

	const lifecycleSyncQuery = createQuery(() => {
		const syncId = data.project?.gitOpsManagedBy;
		return {
			queryKey: queryKeys.gitOpsSyncs.detail(envId, syncId ?? 'none'),
			queryFn: () => gitOpsSyncService.getSync(envId, syncId!),
			enabled: !!syncId,
			staleTime: 30_000
		};
	});

	const lifecycleSync = $derived(lifecycleSyncQuery.data);
	const hasLifecycleHook = $derived(
		!!(lifecycleSync?.preDeployScriptPath && lifecycleSync.preDeployScriptPath.trim().length > 0)
	);

	const formSchema = z
		.object({
			name: z.string().min(1, m.compose_project_name_required()),
			composeContent: z.string().min(1, m.compose_content_is_required()),
			envContent: z.string().optional().default(''),
			overrideContent: z.string().optional().default('')
		})
		.superRefine((data, ctx) => {
			const currentServerName = project?.name ?? '';
			if (data.name !== currentServerName && !/^[a-z0-9_-]+$/i.test(data.name)) {
				ctx.addIssue({
					code: z.ZodIssueCode.custom,
					path: ['name'],
					message: m.compose_project_name_invalid_with_underscores()
				});
			}
		});

	const initialFormData = untrack(() => ({
		name: data.editorState.originalName,
		composeContent: data.editorState.originalComposeContent,
		envContent: data.editorState.originalEnvContent || '',
		overrideContent: data.editorState.originalOverrideContent || ''
	}));

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, initialFormData);

	function withLoadedProjectFileContent(details: Project | null | undefined): Project | null {
		if (!details) return null;

		return {
			...details,
			includeFiles: (details.includeFiles ?? []).map((file) => ({
				...file,
				content: file.content ?? loadedIncludeFileContents[file.relativePath]
			})),
			directoryFiles: (details.directoryFiles ?? []).map((file) => ({
				...file,
				content: file.content ?? loadedDirectoryFileContents[file.relativePath]
			})),
			projectFiles: (details.projectFiles ?? []).map((file) => ({
				...file,
				content: file.content ?? loadedManagedProjectFileContents[file.relativePath]
			}))
		};
	}

	const project = $derived.by(() => {
		const detail = projectDetailQuery.data ?? data.project;
		if (!detail) return null;
		return withLoadedProjectFileContent({ ...detail, ...(projectFilesQuery.data ?? {}) });
	});
	const projectImageRefs = $derived.by(() => getProjectImageRefs(project));
	const serverName = $derived(project?.name ?? '');
	const serverComposeContent = $derived(project?.composeContent ?? '');
	const serverEnvContent = $derived(project?.envContent ?? '');
	const serverOverrideContent = $derived(project?.overrideContent ?? '');
	const serverIncludeFiles = $derived.by(() =>
		Object.fromEntries(
			(project?.includeFiles ?? []).flatMap((file) =>
				file.content === undefined ? [] : [[file.relativePath, file.content] as const]
			)
		)
	);
	const managedProjectFiles = $derived.by(() =>
		applyProjectFileChangesForDisplay(project?.projectFiles ?? [], managedProjectFileChanges)
	);
	const managedProjectFilePaths = $derived.by(() => new Set(managedProjectFiles.map((file) => file.relativePath)));
	const changedManagedProjectFilePaths = $derived.by(() =>
		Object.keys(managedProjectFileContents).filter(
			(relativePath) => managedProjectFileContents[relativePath] !== loadedManagedProjectFileContents[relativePath]
		)
	);

	const composeYamlName = $derived(extractComposeYamlName($inputs.composeContent.value));
	// The compose file's top-level `name:` is authoritative; surface it as the
	// effective name without writing to form state reactively.
	const effectiveName = $derived(composeYamlName ?? $inputs.name.value);

	let hasChanges = $derived(
		effectiveName !== serverName ||
			$inputs.composeContent.value !== serverComposeContent ||
			$inputs.overrideContent.value !== serverOverrideContent ||
			$inputs.envContent.value !== serverEnvContent ||
			Object.entries(includeFilesState).some(([relativePath, content]) => content !== serverIncludeFiles[relativePath]) ||
			managedProjectFileChanges.length > 0 ||
			changedManagedProjectFilePaths.length > 0
	);

	let isGitOpsManaged = $derived(!!project?.gitOpsManagedBy);
	let hasBuildDirective = $derived(!!project?.hasBuildDirective);

	let canEditName = $derived(
		canUpdateProject &&
			!project?.isArchived &&
			!isGitOpsManaged &&
			!isLoading.saving &&
			!composeYamlName &&
			project?.status !== 'running' &&
			project?.status !== 'partially running'
	);
	let canEditCompose = $derived(canUpdateProject && !project?.isArchived && !isGitOpsManaged);
	// Override edits are blocked for GitOps-managed projects, mirroring canEditCompose.
	let canEditOverride = $derived(canUpdateProject && !project?.isArchived && !isGitOpsManaged);
	let canEditEnv = $derived(canUpdateProject && !project?.isArchived);
	let canEditProjectFiles = $derived(canUpdateProject && !project?.isArchived && !isGitOpsManaged);
	let composeFileName = $derived(project?.composeFileName || 'compose.yaml');
	// Set when the user opts to add an override to a project that has none yet.
	let overrideEditorRequested = $state(false);
	// The backend only sets overrideFileName when an override file exists on disk.
	// The display name falls back to the default for the "add override" flow.
	let overrideExists = $derived(!!project?.overrideFileName);
	let overrideFileName = $derived(project?.overrideFileName || 'compose.override.yaml');
	// The override editor surfaces only when an override exists or the user asked
	// to add one this session; otherwise the UI shows an "add override" affordance.
	let overrideActive = $derived(overrideExists || overrideEditorRequested);
	let archiveRequiresStopped = $derived(
		!!project &&
			!project.isArchived &&
			(Number(project.runningCount) > 0 ||
				project.status === 'running' ||
				project.status === 'partially running' ||
				project.status === 'deploying' ||
				project.status === 'restarting')
	);

	let autoScrollStackLogs = $state(true);

	type ProjectTab = 'services' | 'compose' | 'logs';
	let selectedTab = $state<ProjectTab>('compose');
	let composeOpen = $state(true);
	let envOpen = $state(true);
	let overrideOpen = $state(false);
	// Editor action toggles for the encapsulated override editor in the classic
	// view, mirroring the buttons CodePanel's card header normally provides.
	let overrideOutlineOpen = $state(false);
	let overrideDiffOpen = $state(false);
	let overrideCommandPaletteOpen = $state(false);
	let includeFilesPanelStates = $state<Record<string, boolean>>({});
	let selectedFilePreference = $state<'compose' | 'env' | 'override' | string>('compose');
	let openTabsPreference = $state<string[]>(['compose']);
	let treeOutlineOpen = $state(false);
	let treeDiffOpen = $state(false);
	let treeCommandPaletteOpen = $state(false);
	let layoutMode = $state<'classic' | 'tree'>('classic');
	let selectedIncludeTabPreference = $state<string | null>(null);
	let treePaneWidth = $state(420);
	let composeSplitWidth = $state<number | null>(null);
	const minComposePaneWidth = 360;
	const minEnvPaneWidth = 280;

	let composeHasErrors = $state(false);
	let overrideHasErrors = $state(false);
	let envHasErrors = $state(false);
	let includeFilesHasErrors = $state<Record<string, boolean>>({});
	let managedProjectFileHasErrors = $state<Record<string, boolean>>({});
	let composeValidationReady = $state(false);
	let overrideValidationReady = $state(false);
	let envValidationReady = $state(false);
	let includeFilesValidationReady = $state<Record<string, boolean>>({});
	let managedProjectFileValidationReady = $state<Record<string, boolean>>({});
	const includeFilePaths = $derived.by(() => new Set((project?.includeFiles ?? []).map((file) => file.relativePath)));
	const directoryFilePaths = $derived.by(() => new Set((project?.directoryFiles ?? []).map((file) => file.relativePath)));
	const selectedFile = $derived.by(() => {
		const current = selectedFilePreference;
		if (current === 'override') return overrideActive ? 'override' : 'compose';
		if (current === 'compose' || current === 'env') return current;
		if (current.startsWith('file:')) {
			const relativePath = current.slice(5);
			return managedProjectFilePaths.has(relativePath) ? current : 'compose';
		}
		if (current.startsWith('dir:')) {
			return directoryFilePaths.has(current.slice(4)) ? current : 'compose';
		}
		return includeFilePaths.has(current) ? current : 'compose';
	});
	const selectedManagedProjectFilePath = $derived(selectedFile.startsWith('file:') ? selectedFile.slice(5) : '');
	const selectedManagedProjectFile = $derived.by(() =>
		selectedManagedProjectFilePath
			? managedProjectFiles.find((file) => file.relativePath === selectedManagedProjectFilePath)
			: undefined
	);
	const openTabs = $derived.by(() => {
		const valid = openTabsPreference.filter((key) => {
			if (key === 'compose' || key === 'env') return true;
			// Drop a lingering override tab once the override is gone (e.g. deleted
			// via a blank save) and no add is in progress.
			if (key === 'override') return overrideActive;
			if (!key.startsWith('file:')) return false;
			// fallow-ignore-next-line code-duplication -- managed-file predicate; script-level, diverges per page
			const entry = managedProjectFiles.find((file) => file.relativePath === key.slice(5));
			return !!entry && !entry.isDirectory;
		});
		return valid.length > 0 ? valid : ['compose'];
	});
	const activeTreeTab = $derived(openTabs.includes(selectedFile) ? selectedFile : (openTabs[0] ?? 'compose'));
	const treeTabs = $derived(
		openTabs.map((key) => ({
			key,
			label: treeTabLabel(key),
			title: treeTabTitle(key),
			iconClass:
				key === 'compose'
					? 'text-blue-500'
					: key === 'override'
						? 'text-purple-500'
						: key === 'env'
							? 'text-green-500'
							: 'text-muted-foreground',
			pending: treeTabPending(key)
		}))
	);
	// Validity check covers both include files (editable) and directory files
	// (read-only — currently used to surface the pre-deploy script alongside
	// includes in classic layout). A path that no longer exists in either set
	// is forgotten so a removed file doesn't leave a dangling tab selection.
	const selectedIncludeTab = $derived.by(() => {
		if (!selectedIncludeTabPreference) return null;
		if (includeFilePaths.has(selectedIncludeTabPreference)) return selectedIncludeTabPreference;
		if (directoryFilePaths.has(selectedIncludeTabPreference)) return selectedIncludeTabPreference;
		return null;
	});
	let composeHasChanges = $derived($inputs.composeContent.value !== serverComposeContent);
	let overrideHasChanges = $derived($inputs.overrideContent.value !== serverOverrideContent);
	let envHasChanges = $derived($inputs.envContent.value !== serverEnvContent);
	let changedIncludeFilePaths = $derived.by(() =>
		Object.keys(includeFilesState).filter((relativePath) => includeFilesState[relativePath] !== serverIncludeFiles[relativePath])
	);

	let hasAnyErrors = $derived(
		(composeHasChanges && (!composeValidationReady || composeHasErrors)) ||
			(overrideHasChanges && (!overrideValidationReady || overrideHasErrors)) ||
			(envHasChanges && (!envValidationReady || envHasErrors)) ||
			changedIncludeFilePaths.some(
				(relativePath) => !includeFilesValidationReady[relativePath] || !!includeFilesHasErrors[relativePath]
			)
	);

	let canSave = $derived(canUpdateProject && !project?.isArchived && hasChanges && !hasAnyErrors);

	const tabItems = $derived<TabItem[]>([
		{
			value: 'services',
			label: m.services(),
			icon: LayersIcon,
			badge: project?.serviceCount
		},
		{
			value: 'compose',
			label: m.common_configuration(),
			icon: SettingsIcon
		},
		...(canViewProjectLogs
			? [
					{
						value: 'logs',
						label: m.common_logs(),
						icon: FileTextIcon,
						disabled: project?.status !== 'running'
					}
				]
			: [])
	]);

	let nameInputRef = $state<HTMLInputElement | null>(null);

	type ComposeUIPrefs = {
		tab: 'services' | 'compose' | 'logs';
		composeOpen: boolean;
		overrideOpen: boolean;
		envOpen: boolean;
		autoScroll: boolean;
		layoutMode: 'classic' | 'tree';
		selectedFile?: 'compose' | 'env' | 'override' | string;
		openTabs?: string[];
	};

	type RebaseEditorDraftOptions = {
		preserveEditableDrafts?: boolean;
		preserveManagedProjectFileContents?: boolean;
		clearLoadedFileCache?: boolean;
	};

	type RefreshProjectDetailsOptions = RebaseEditorDraftOptions & {
		forceRebaseDraft?: boolean;
	};

	const defaultComposeUIPrefs: ComposeUIPrefs = {
		tab: 'compose',
		composeOpen: true,
		overrideOpen: false,
		envOpen: true,
		autoScroll: true,
		layoutMode: 'classic',
		selectedFile: 'compose',
		openTabs: ['compose']
	};

	let prefs: PersistedState<ComposeUIPrefs> | null = null;
	let lastPrefsProjectId = $state<string | null>(null);
	const urlTab = useUrlTab<ProjectTab>({
		validTabs: () => tabItems.map((tab) => tab.value as ProjectTab),
		defaultTab: () => selectedTab,
		ready: () => lastPrefsProjectId === project?.id
	});
	const activeTab = $derived(urlTab.value);

	function ensureIncludeFileUiState(relativePath: string) {
		if (includeFilesPanelStates[relativePath] === undefined) {
			includeFilesPanelStates = {
				...includeFilesPanelStates,
				[relativePath]: true
			};
		}
		if (includeFilesHasErrors[relativePath] === undefined) {
			includeFilesHasErrors = {
				...includeFilesHasErrors,
				[relativePath]: false
			};
		}
		if (includeFilesValidationReady[relativePath] === undefined) {
			includeFilesValidationReady = {
				...includeFilesValidationReady,
				[relativePath]: false
			};
		}
	}

	function ensureManagedProjectFileUiState(relativePath: string) {
		if (managedProjectFileHasErrors[relativePath] === undefined) {
			managedProjectFileHasErrors = {
				...managedProjectFileHasErrors,
				[relativePath]: false
			};
		}
		if (managedProjectFileValidationReady[relativePath] === undefined) {
			managedProjectFileValidationReady = {
				...managedProjectFileValidationReady,
				[relativePath]: true
			};
		}
	}

	function getProjectImageRefs(details?: Project | null): string[] {
		const refs = new Set<string>();

		for (const service of details?.services ?? []) {
			const imageRef = service.image?.trim();
			if (imageRef) {
				refs.add(imageRef);
			}
		}

		if (refs.size === 0) {
			for (const service of details?.runtimeServices ?? []) {
				const imageRef = service.image?.trim();
				if (imageRef) {
					refs.add(imageRef);
				}
			}
		}

		return [...refs];
	}

	function getProjectIncludeFileContents(details: Project | null | undefined): Record<string, string> {
		return Object.fromEntries(
			(details?.includeFiles ?? []).flatMap((file) =>
				file.content === undefined ? [] : [[file.relativePath, file.content] as const]
			)
		);
	}

	function getDirtyIncludeDrafts(): Record<string, string> {
		return Object.fromEntries(
			Object.entries(includeFilesState).filter(([relativePath, content]) => content !== serverIncludeFiles[relativePath])
		);
	}

	function clearLoadedProjectFileCache() {
		loadedIncludeFileContents = {};
		loadedDirectoryFileContents = {};
		loadedManagedProjectFileContents = {};
		managedProjectFileContents = {};
		managedProjectFileLoadErrors = {};
		managedProjectFileLoading = {};
		projectFilePromises = {};
	}

	function rebaseEditorDraft(details: Project, options: RebaseEditorDraftOptions = {}) {
		const envDraft = $inputs.envContent.value;
		const shouldPreserveEnvDraft = options.preserveEditableDrafts === true && envDraft !== serverEnvContent;
		const dirtyIncludeDrafts = options.preserveEditableDrafts === true ? getDirtyIncludeDrafts() : {};

		if (options.clearLoadedFileCache === true) {
			clearLoadedProjectFileCache();
		}

		const normalizedProject = withLoadedProjectFileContent(details);
		if (!normalizedProject) return;
		// A slim save response (no file changes) carries no projectFiles; the tree is
		// unchanged server-side, so keep the current contents instead of deriving
		// them from an empty tree.
		const savedManagedProjectFileContents =
			options.preserveManagedProjectFileContents === true
				? details.projectFiles === undefined
					? { ...managedProjectFileContents }
					: Object.fromEntries(
							(normalizedProject.projectFiles ?? []).flatMap((file) => {
								if (file.isDirectory) return [];
								const content =
									loadedManagedProjectFileContents[file.relativePath] ?? managedProjectFileContents[file.relativePath];
								return content === undefined ? [] : [[file.relativePath, content] as const];
							})
						)
				: {};

		$inputs.name.value = normalizedProject.name || '';
		$inputs.composeContent.value = normalizedProject.composeContent || '';
		$inputs.overrideContent.value = normalizedProject.overrideContent || '';
		$inputs.envContent.value = shouldPreserveEnvDraft ? envDraft : normalizedProject.envContent || '';
		managedProjectFileChanges = [];
		managedProjectFileContents = savedManagedProjectFileContents;
		// Seed the per-file UI-state records for every retained path. A mounted
		// CodePanel binds these entries; handing a bound $bindable-with-fallback
		// prop an undefined entry throws props_invalid_value and kills the page's
		// effect tree (stale editor shown until a full refresh).
		managedProjectFileHasErrors = Object.fromEntries(
			Object.keys(savedManagedProjectFileContents).map((relativePath) => [relativePath, false])
		);
		managedProjectFileValidationReady = Object.fromEntries(
			Object.keys(savedManagedProjectFileContents).map((relativePath) => [relativePath, true])
		);
		managedProjectFileLoadErrors = {};
		managedProjectFileLoading = {};

		const freshIncludeFiles = getProjectIncludeFileContents(normalizedProject);
		includeFilesState = {
			...freshIncludeFiles,
			...Object.fromEntries(
				Object.entries(dirtyIncludeDrafts).filter(([relativePath]) =>
					(normalizedProject.includeFiles ?? []).some((file) => file.relativePath === relativePath)
				)
			)
		};
	}

	async function syncProjectQueries(updatedProject: Project) {
		const currentEnvId = envId ?? (await environmentStore.getCurrentEnvironmentId());

		// The save response is slim (no directory walks), so merge it into the
		// cached detail instead of replacing it wholesale.
		queryClient.setQueryData(
			queryKeys.projects.detail(currentEnvId, updatedProject.id),
			(old: Project | undefined) => ({ ...(old ?? {}), ...updatedProject }) as Project
		);
		if (updatedProject.projectFiles || updatedProject.fileTreeRevision) {
			// A file-changes save returns a fresh tree + revision; sync it into the
			// files cache so the next file save doesn't hit a revision conflict.
			queryClient.setQueryData(
				queryKeys.projects.files(currentEnvId, updatedProject.id),
				(old: Project | undefined) =>
					({
						...(old ?? {}),
						projectFiles: updatedProject.projectFiles,
						fileTreeRevision: updatedProject.fileTreeRevision,
						fileTreeTruncated: updatedProject.fileTreeTruncated
					}) as Project
			);
		} else {
			await queryClient.invalidateQueries({ queryKey: queryKeys.projects.files(currentEnvId, updatedProject.id) });
		}
		await Promise.all([
			queryClient.invalidateQueries({ queryKey: ['projects', currentEnvId] }),
			queryClient.invalidateQueries({ queryKey: queryKeys.projects.statusCounts(currentEnvId) })
		]);
	}

	const checkProjectUpdatesMutation = createMutation(() => ({
		mutationKey: queryKeys.projects.detailCheckUpdates(envId ?? '0', projectId),
		mutationFn: async () => {
			if (projectImageRefs.length === 0) {
				return {};
			}
			return imageService.checkMultipleImages(projectImageRefs);
		},
		onSuccess: async (results) => {
			const currentEnvId = envId ?? (await environmentStore.getCurrentEnvironmentId());
			const firstError = Object.values(results)
				.find((result) => !!result?.error?.trim())
				?.error?.trim();
			const hasErrors = !!firstError;
			const toastOptions = activityToastOptions(extractActivityId(results));
			if (hasErrors) {
				toast.error(firstError || m.containers_check_updates_failed(), toastOptions);
			} else {
				toast.success(m.images_update_check_completed(), toastOptions);
			}
			await Promise.all([
				refreshProjectDetails(),
				queryClient.invalidateQueries({ queryKey: ['projects', currentEnvId] }),
				queryClient.invalidateQueries({ queryKey: queryKeys.projects.statusCounts(currentEnvId) })
			]);
		},
		onError: () => {
			toast.error(m.containers_check_updates_failed());
		}
	}));

	$effect(() => {
		if (!project?.id) return;
		if (lastPrefsProjectId === project.id) return;

		const prefsStorageKey = `arcane.compose.ui:${project.id}`;
		const hadStoredPrefs = sessionStorage.getItem(prefsStorageKey) !== null;
		// The tree/classic auto-detect needs the lazily loaded file tree; without
		// stored prefs, wait for the files query to settle before finalizing.
		if (!hadStoredPrefs && !(projectFilesQuery.isSuccess || projectFilesQuery.isError)) return;

		lastPrefsProjectId = project.id;
		prefs = new PersistedState<ComposeUIPrefs>(prefsStorageKey, defaultComposeUIPrefs, {
			storage: 'session',
			syncTabs: false
		});
		const cur = prefs.current ?? {};
		selectedTab = cur.tab ?? defaultComposeUIPrefs.tab;
		composeOpen = cur.composeOpen ?? defaultComposeUIPrefs.composeOpen;
		// Expanding the override collapses the compose editor (accordion), so the
		// override defaults to collapsed to keep compose primary on load.
		overrideOpen = cur.overrideOpen ?? false;
		envOpen = cur.envOpen ?? defaultComposeUIPrefs.envOpen;
		autoScrollStackLogs = cur.autoScroll ?? defaultComposeUIPrefs.autoScroll;
		selectedFilePreference = cur.selectedFile ?? defaultComposeUIPrefs.selectedFile ?? 'compose';
		openTabsPreference = cur.openTabs && cur.openTabs.length > 0 ? cur.openTabs : [selectedFilePreference];

		// Auto-detect layout mode based on includeFiles or directoryFiles. PersistedState
		// always materializes the defaults, so only trust the stored layoutMode when this
		// project actually had persisted prefs.
		const hasIncludes = project?.includeFiles && project.includeFiles.length > 0;
		const hasDirectoryFiles = project?.directoryFiles && project.directoryFiles.length > 0;
		const hasProjectFiles = project?.projectFiles && project.projectFiles.length > 0;
		const defaultMode = hasIncludes || hasDirectoryFiles || hasProjectFiles ? 'tree' : 'classic';
		layoutMode = hadStoredPrefs ? (cur.layoutMode ?? defaultMode) : defaultMode;
		// PersistedState seeds storage with the defaults on first mount; persist the
		// resolved state so the auto-detected layout survives the next visit.
		if (!hadStoredPrefs) {
			persistPrefs();
		}
	});

	function isDeletedByProjectFileChanges(relativePath: string, changes: ProjectFileChange[]): boolean {
		return changes.some((change) => change.operation === 'delete' && projectFilePathMatches(relativePath, change.relativePath));
	}

	function buildProjectFileSaveChanges(): ProjectFileChange[] {
		const changes = managedProjectFileChanges.map((change) => ({ ...change }));
		const contentChanges = new Map<string, string>();

		for (const relativePath of changedManagedProjectFilePaths) {
			const content = managedProjectFileContents[relativePath];
			if (content !== undefined) {
				contentChanges.set(relativePath, content);
			}
		}

		const createFilePaths = new Set<string>();
		for (const change of changes) {
			if (change.operation === 'create_file') {
				createFilePaths.add(change.relativePath);
				change.content = contentChanges.get(change.relativePath) ?? change.content ?? '';
			}
		}

		for (const [relativePath, content] of contentChanges.entries()) {
			if (createFilePaths.has(relativePath) || isDeletedByProjectFileChanges(relativePath, changes)) {
				continue;
			}
			changes.push({
				operation: 'update_file',
				relativePath,
				content
			});
		}

		return changes;
	}

	function buildIncludeFileSaveUpdates(): Array<{ relativePath: string; content: string }> {
		return changedIncludeFilePaths.flatMap((relativePath) => {
			const content = includeFilesState[relativePath];
			return content === undefined ? [] : [{ relativePath, content }];
		});
	}

	async function handleSaveChanges() {
		if (!project || !hasChanges) return;
		if (project.isArchived) {
			toast.error(m.projects_archive_edit_blocked());
			return;
		}
		if (hasAnyErrors) {
			toast.error(m.templates_validation_error());
			return;
		}

		const formValues = form.data();
		const validated = isGitOpsManaged ? formValues : form.validate();
		if (!validated) return;

		const { composeContent, envContent, overrideContent } = validated;
		const namePayload = isGitOpsManaged ? undefined : effectiveName;
		const composePayload = isGitOpsManaged ? undefined : composeContent;
		const envPayload = envContent !== serverEnvContent ? envContent : undefined;
		// Blank override => delete, which the backend treats as a no-op when none
		// exists, so we can always send it (except for read-only GitOps projects).
		const overridePayload = isGitOpsManaged ? undefined : overrideContent;
		const fileChangesPayload = buildProjectFileSaveChanges();
		const includeFileUpdates = buildIncludeFileSaveUpdates();
		const fileTreeRevision = fileChangesPayload.length > 0 ? project.fileTreeRevision : undefined;

		handleApiResultWithCallbacks({
			result: await tryCatch(
				(async () => {
					let updatedProject = await projectService.updateProject(
						projectId,
						namePayload,
						composePayload,
						envPayload,
						overridePayload,
						fileTreeRevision,
						fileChangesPayload
					);

					// The file-tree changes are committed server-side at this point. Rebase the
					// local tree state and project query immediately — if an include-file update
					// below fails, the already-applied changes must not stay queued with a stale
					// fileTreeRevision, or every retry would be rejected with a 409 conflict.
					loadedManagedProjectFileContents = {
						...loadedManagedProjectFileContents,
						...managedProjectFileContents
					};
					rebaseEditorDraft(updatedProject, { preserveManagedProjectFileContents: true, preserveEditableDrafts: true });
					await syncProjectQueries(updatedProject);

					for (const update of includeFileUpdates) {
						updatedProject = await projectService.updateProjectIncludeFile(projectId, update.relativePath, update.content);
					}
					return updatedProject;
				})()
			),
			message: m.common_save_failed(),
			setLoadingState: (value) => (isLoading.saving = value),
			onSuccess: async (updatedProject: Project) => {
				loadedIncludeFileContents = {
					...loadedIncludeFileContents,
					...Object.fromEntries(
						Object.entries(includeFilesState).filter(([relativePath]) =>
							(updatedProject.includeFiles ?? []).some((file) => file.relativePath === relativePath)
						)
					)
				};
				rebaseEditorDraft(updatedProject, { preserveManagedProjectFileContents: true });
				await syncProjectQueries(updatedProject);
				toast.success(
					m.common_update_success({ resource: m.project() }),
					activityToastOptions(extractActivityId(updatedProject))
				);
			}
		});
	}

	function saveNameIfChanged() {
		if (project?.isArchived) return;
		if (effectiveName === serverName) return;
		const validated = form.validate();
		if (!validated) return;
		handleSaveChanges();
	}

	async function handleArchiveToggle() {
		if (!project) return;
		const archiving = !project.isArchived;
		if (archiving && archiveRequiresStopped) {
			toast.error(m.projects_archive_requires_stopped());
			return;
		}

		isLoading.archiving = true;
		try {
			const result = await tryCatch(
				archiving ? projectService.archiveProject(project.id) : projectService.unarchiveProject(project.id)
			);
			await handleApiResultWithCallbacks({
				result,
				message: archiving ? m.compose_archive_failed() : m.compose_unarchive_failed(),
				onSuccess: async () => {
					toast.success(archiving ? m.compose_archive_success() : m.compose_unarchive_success());
					await refreshProjectDetails();
					const currentEnvId = envId ?? (await environmentStore.getCurrentEnvironmentId());
					await Promise.all([
						queryClient.invalidateQueries({ queryKey: ['projects', currentEnvId] }),
						queryClient.invalidateQueries({ queryKey: queryKeys.projects.statusCounts(currentEnvId) })
					]);
				}
			});
		} finally {
			isLoading.archiving = false;
		}
	}

	function persistPrefs() {
		if (!prefs) return;
		prefs.current = {
			tab: selectedTab,
			composeOpen,
			overrideOpen,
			envOpen,
			autoScroll: autoScrollStackLogs,
			layoutMode,
			selectedFile,
			openTabs
		};
	}

	type ProjectFileKind = 'include' | 'directory' | 'managed';

	function getProjectFileKey(projectId: string, kind: ProjectFileKind, relativePath: string): string {
		return `${projectId}:${kind}:${relativePath}`;
	}

	function updateLoadedProjectFile(kind: ProjectFileKind, relativePath: string, content: string) {
		if (kind === 'include') {
			ensureIncludeFileUiState(relativePath);
			loadedIncludeFileContents = {
				...loadedIncludeFileContents,
				[relativePath]: content
			};
			if (includeFilesState[relativePath] === undefined) {
				includeFilesState = {
					...includeFilesState,
					[relativePath]: content
				};
			}
			return;
		}

		loadedDirectoryFileContents = {
			...loadedDirectoryFileContents,
			[relativePath]: content
		};
	}

	function updateLoadedManagedProjectFile(relativePath: string, content: string) {
		ensureManagedProjectFileUiState(relativePath);
		loadedManagedProjectFileContents = {
			...loadedManagedProjectFileContents,
			[relativePath]: content
		};
		if (managedProjectFileContents[relativePath] === undefined) {
			managedProjectFileContents = {
				...managedProjectFileContents,
				[relativePath]: content
			};
		}
		managedProjectFileLoadErrors = removeProjectFileRecord(managedProjectFileLoadErrors, relativePath);
	}

	function getProjectFileResource(kind: ProjectFileKind, relativePath: string): IncludeFile | Promise<IncludeFile> {
		const currentProjectId = project?.id;
		if (!currentProjectId) {
			throw new Error('Project is not loaded');
		}
		if (kind === 'include') {
			ensureIncludeFileUiState(relativePath);
		} else if (kind === 'managed') {
			ensureManagedProjectFileUiState(relativePath);
			if (managedProjectFileContents[relativePath] !== undefined) {
				return {
					path: relativePath,
					relativePath,
					content: managedProjectFileContents[relativePath]
				};
			}
		}

		const targetFile =
			kind === 'include'
				? project?.includeFiles?.find((file) => file.relativePath === relativePath)
				: kind === 'directory'
					? project?.directoryFiles?.find((file) => file.relativePath === relativePath)
					: managedProjectFiles.find((file) => file.relativePath === relativePath);

		if (!targetFile) {
			throw new Error('Project file not found');
		}

		if (targetFile.content !== undefined) {
			if (kind === 'managed') {
				updateLoadedManagedProjectFile(relativePath, targetFile.content ?? '');
			} else {
				updateLoadedProjectFile(kind, relativePath, targetFile.content ?? '');
			}
			return targetFile;
		}

		const requestKey = getProjectFileKey(currentProjectId, kind, relativePath);
		const existingPromise = projectFilePromises[requestKey];
		if (existingPromise) {
			return existingPromise;
		}

		const promise = (async () => {
			const file = await projectService.getProjectFile(currentProjectId, relativePath);
			if (kind === 'managed') {
				updateLoadedManagedProjectFile(relativePath, file.content ?? '');
			} else {
				updateLoadedProjectFile(kind, relativePath, file.content ?? '');
			}
			return {
				...file,
				content: file.content ?? ''
			};
		})().finally(() => {
			delete projectFilePromises[requestKey];
		});

		projectFilePromises[requestKey] = promise;

		return promise;
	}

	function isManagedDirectoryKey(key: string): boolean {
		if (!key.startsWith('file:')) return false;
		return managedProjectFiles.find((file) => file.relativePath === key.slice(5))?.isDirectory === true;
	}

	function openFileTab(key: string) {
		// The file panel's bound UI-state entries must exist before mount; binding
		// an undefined entry to a $bindable-with-fallback prop throws props_invalid_value.
		if (key.startsWith('file:') && !isManagedDirectoryKey(key)) {
			ensureManagedProjectFileUiState(key.slice(5));
		}
		if (!isManagedDirectoryKey(key) && !openTabsPreference.includes(key)) {
			openTabsPreference = [...openTabsPreference, key];
		}
		selectedFilePreference = key;
		if (layoutMode === 'tree') {
			persistPrefs();
		}
	}

	function closeFileTab(key: string) {
		// Closing a not-yet-saved override tab cancels the add outright — there's
		// nothing on disk to keep, so the override reverts to the add affordance.
		if (key === 'override' && !overrideExists) {
			overrideEditorRequested = false;
			$inputs.overrideContent.value = '';
		}
		const index = openTabs.indexOf(key);
		const remaining = openTabs.filter((tab) => tab !== key);
		openTabsPreference = openTabsPreference.filter((tab) => tab !== key);
		if (selectedFile === key) {
			selectedFilePreference = remaining[Math.min(Math.max(index - 1, 0), remaining.length - 1)] ?? 'compose';
		}
		persistPrefs();
	}

	function treeTabLabel(key: string): string {
		if (key === 'compose') return composeFileName;
		if (key === 'override') return overrideFileName;
		if (key === 'env') return '.env';
		return projectFileBasename(key.startsWith('file:') ? key.slice(5) : key);
	}

	function treeTabTitle(key: string): string {
		if (key === 'compose') return composeFileName;
		if (key === 'override') return overrideFileName;
		if (key === 'env') return '.env';
		return key.startsWith('file:') ? key.slice(5) : key;
	}

	function treeTabPending(key: string): boolean {
		if (key === 'compose') return composeHasChanges;
		if (key === 'override') return overrideHasChanges;
		if (key === 'env') return envHasChanges;
		if (!key.startsWith('file:')) return false;
		const relativePath = key.slice(5);
		return (
			changedManagedProjectFilePaths.includes(relativePath) ||
			managedProjectFiles.find((file) => file.relativePath === relativePath)?.pending === true
		);
	}

	function selectManagedProjectFile(key: string) {
		openFileTab(key);
	}

	// Reveal the override editor for a project that has no override yet: mark it
	// active (so the tab/panel renders), expand it, and focus its tab.
	function handleAddOverride() {
		overrideEditorRequested = true;
		overrideOpen = true;
		openFileTab('override');
	}

	// Remove the override. A not-yet-saved "add" is discarded client-side; an
	// existing on-disk override is deleted by persisting the now-blank content
	// (the backend treats a blank override as a delete). Either way the editor
	// collapses back to the "add override" affordance.
	async function handleRemoveOverride() {
		overrideEditorRequested = false;
		overrideOpen = false;
		const existed = overrideExists;
		$inputs.overrideContent.value = '';
		if (existed) {
			await handleSaveChanges();
		}
	}

	async function loadManagedProjectFileDraft(relativePath: string) {
		if (!relativePath || managedProjectFileContents[relativePath] !== undefined || managedProjectFileLoading[relativePath]) {
			return;
		}

		managedProjectFileLoading = {
			...managedProjectFileLoading,
			[relativePath]: true
		};
		managedProjectFileLoadErrors = removeProjectFileRecord(managedProjectFileLoadErrors, relativePath);

		try {
			const file = await getProjectFileResource('managed', relativePath);
			updateLoadedManagedProjectFile(relativePath, file.content ?? '');
		} catch (error) {
			managedProjectFileLoadErrors = {
				...managedProjectFileLoadErrors,
				[relativePath]: error instanceof Error ? error.message : String(error)
			};
		} finally {
			managedProjectFileLoading = removeProjectFileRecord(managedProjectFileLoading, relativePath);
		}
	}

	$effect(() => {
		const relativePath = selectedManagedProjectFilePath;
		const entry = selectedManagedProjectFile;
		const hasContent = relativePath ? managedProjectFileContents[relativePath] !== undefined : true;
		const isLoadingFile = relativePath ? managedProjectFileLoading[relativePath] === true : false;
		const hasLoadError = relativePath ? managedProjectFileLoadErrors[relativePath] !== undefined : false;

		if (!relativePath || !entry || entry.isDirectory || hasContent || isLoadingFile || hasLoadError) {
			return;
		}

		void loadManagedProjectFileDraft(relativePath);
	});

	function remapManagedProjectFileState(oldPath: string, newPath: string) {
		managedProjectFileContents = remapProjectFileRecord(managedProjectFileContents, oldPath, newPath);
		loadedManagedProjectFileContents = remapProjectFileRecord(loadedManagedProjectFileContents, oldPath, newPath);
		managedProjectFileHasErrors = remapProjectFileRecord(managedProjectFileHasErrors, oldPath, newPath);
		managedProjectFileValidationReady = remapProjectFileRecord(managedProjectFileValidationReady, oldPath, newPath);
		managedProjectFileLoadErrors = remapProjectFileRecord(managedProjectFileLoadErrors, oldPath, newPath);
		managedProjectFileLoading = remapProjectFileRecord(managedProjectFileLoading, oldPath, newPath);
		includeFilesState = remapProjectFileRecord(includeFilesState, oldPath, newPath);
		loadedIncludeFileContents = remapProjectFileRecord(loadedIncludeFileContents, oldPath, newPath);
		includeFilesPanelStates = remapProjectFileRecord(includeFilesPanelStates, oldPath, newPath);
		includeFilesHasErrors = remapProjectFileRecord(includeFilesHasErrors, oldPath, newPath);
		includeFilesValidationReady = remapProjectFileRecord(includeFilesValidationReady, oldPath, newPath);
		openTabsPreference = openTabsPreference.map((tab) => remapSelectedProjectFileKey(tab, oldPath, newPath) ?? tab);
		const remappedSelection = remapSelectedProjectFileKey(selectedFile, oldPath, newPath);
		if (remappedSelection) {
			selectedFilePreference = remappedSelection;
		}
	}

	function removeManagedProjectFileState(relativePath: string) {
		managedProjectFileContents = removeProjectFileRecord(managedProjectFileContents, relativePath);
		loadedManagedProjectFileContents = removeProjectFileRecord(loadedManagedProjectFileContents, relativePath);
		managedProjectFileHasErrors = removeProjectFileRecord(managedProjectFileHasErrors, relativePath);
		managedProjectFileValidationReady = removeProjectFileRecord(managedProjectFileValidationReady, relativePath);
		managedProjectFileLoadErrors = removeProjectFileRecord(managedProjectFileLoadErrors, relativePath);
		managedProjectFileLoading = removeProjectFileRecord(managedProjectFileLoading, relativePath);
		includeFilesState = removeProjectFileRecord(includeFilesState, relativePath);
		loadedIncludeFileContents = removeProjectFileRecord(loadedIncludeFileContents, relativePath);
		includeFilesPanelStates = removeProjectFileRecord(includeFilesPanelStates, relativePath);
		includeFilesHasErrors = removeProjectFileRecord(includeFilesHasErrors, relativePath);
		includeFilesValidationReady = removeProjectFileRecord(includeFilesValidationReady, relativePath);
		openTabsPreference = openTabsPreference.filter((tab) => !isProjectFileSelectionUnder(tab, relativePath));
		if (isProjectFileSelectionUnder(selectedFile, relativePath)) {
			selectedFilePreference = openTabs[0] ?? 'compose';
		}
	}

	function createManagedProjectFile(parentPath: string, name: string, content = '') {
		const relativePath = planProjectFileCreate(managedProjectFilePaths, parentPath, name, composeFileName);
		if (!relativePath) return;
		managedProjectFileChanges = [...managedProjectFileChanges, { operation: 'create_file', relativePath, content }];
		managedProjectFileContents = { ...managedProjectFileContents, [relativePath]: content };
		loadedManagedProjectFileContents = { ...loadedManagedProjectFileContents, [relativePath]: content };
		managedProjectFileLoadErrors = removeProjectFileRecord(managedProjectFileLoadErrors, relativePath);
		ensureManagedProjectFileUiState(relativePath);
		openFileTab(`file:${relativePath}`);
	}

	function createManagedProjectFolder(parentPath: string, name: string) {
		const relativePath = planProjectFileCreate(managedProjectFilePaths, parentPath, name, composeFileName);
		if (!relativePath) return;
		managedProjectFileChanges = [...managedProjectFileChanges, { operation: 'create_folder', relativePath }];
		selectedFilePreference = `file:${relativePath}`;
	}

	function renameManagedProjectFile(relativePath: string, newName: string) {
		const plan = planProjectFileRename(managedProjectFilePaths, relativePath, newName, composeFileName);
		if (!plan) return;
		managedProjectFileChanges = [...managedProjectFileChanges, { operation: 'rename', relativePath, newName: plan.newName }];
		remapManagedProjectFileState(relativePath, plan.newPath);
	}

	function moveManagedProjectFile(relativePath: string, newParentPath: string) {
		const entry = managedProjectFiles.find((file) => file.relativePath === relativePath);
		const newPath = planProjectFileMove(entry, managedProjectFilePaths, relativePath, newParentPath);
		if (!newPath) return;
		managedProjectFileChanges = [...managedProjectFileChanges, { operation: 'move', relativePath, newParentPath }];
		remapManagedProjectFileState(relativePath, newPath);
	}

	function deleteManagedProjectFile(relativePath: string) {
		const entry = managedProjectFiles.find((file) => file.relativePath === relativePath);
		if (!entry) return;
		managedProjectFileChanges = [
			...managedProjectFileChanges,
			{ operation: 'delete', relativePath, recursive: entry.isDirectory }
		];
		removeManagedProjectFileState(relativePath);
	}

	function toggleIncludeFileTab(relativePath: string) {
		ensureIncludeFileUiState(relativePath);
		selectedIncludeTabPreference = selectedIncludeTab === relativePath ? null : relativePath;
	}

	const allComposeContents = $derived.by(() => {
		return [$inputs.composeContent.value, $inputs.overrideContent.value, ...Object.values(includeFilesState)].filter(
			(value) => value.length > 0
		);
	});
	const codeEditorContext = $derived({
		envContent: $inputs.envContent.value,
		composeContents: allComposeContents,
		globalVariables: globalVariableMap
	});

	async function refreshProjectDetails(options: RefreshProjectDetailsOptions = {}) {
		if (!projectId) return;
		await handleApiResultWithCallbacks({
			result: await tryCatch(projectService.getProject(projectId)),
			message: m.common_refresh_failed({ resource: m.project() }),
			onSuccess: async (updatedProject) => {
				if (options.forceRebaseDraft || !hasChanges) {
					rebaseEditorDraft(updatedProject, options);
				}
				await syncProjectQueries(updatedProject);
			}
		});
	}

	async function handleSyncFromGit() {
		if (!envId || !project?.gitOpsManagedBy) return;
		isLoading.syncing = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.performSync(envId, project.gitOpsManagedBy)),
			message: m.git_sync_failed(),
			setLoadingState: (value) => (isLoading.syncing = value),
			onSuccess: async () => {
				await refreshProjectDetails({
					forceRebaseDraft: true,
					preserveEditableDrafts: true,
					clearLoadedFileCache: true
				});
				await queryClient.invalidateQueries({ queryKey: queryKeys.gitOpsSyncs.all });
				toast.success(m.git_sync_success());
			}
		});
	}

	async function handleCheckProjectUpdates() {
		await checkProjectUpdatesMutation.mutateAsync();
	}

	function formatUrlLabel(raw: string): string {
		const trimmed = raw.trim();
		if (!trimmed) return raw;
		try {
			const parsed = new URL(trimmed);
			return parsed.host || parsed.hostname || trimmed;
		} catch {
			return trimmed;
		}
	}

	const backUrl = $derived.by(() => {
		const from = page.url.searchParams.get('from');
		const sourceEnvironmentId = page.url.searchParams.get('environmentId');

		if (from === 'gitops' && sourceEnvironmentId) {
			return `/environments/${sourceEnvironmentId}/gitops`;
		}

		return '/projects';
	});

	function composePanelProps() {
		return {
			title: composeFileName,
			language: 'yaml',
			validationMode: 'compose',
			error: $inputs.composeContent.error ?? undefined,
			readOnly: !canEditCompose,
			fileId: `project:${projectId}:compose`,
			originalValue: serverComposeContent,
			enableDiff: true,
			editorContext: codeEditorContext
		} as const;
	}

	function overridePanelProps() {
		return {
			title: overrideFileName,
			language: 'yaml',
			validationMode: 'compose',
			error: $inputs.overrideContent.error ?? undefined,
			readOnly: !canEditOverride,
			fileId: `project:${projectId}:override`,
			originalValue: serverOverrideContent,
			enableDiff: true,
			editorContext: codeEditorContext
		} as const;
	}

	function envPanelProps() {
		return {
			title: '.env',
			language: 'env',
			validationMode: 'env',
			error: $inputs.envContent.error ?? undefined,
			readOnly: !canEditEnv,
			fileId: `project:${projectId}:env`,
			originalValue: serverEnvContent,
			enableDiff: true,
			editorContext: codeEditorContext
		} as const;
	}
</script>

{#if project}
	{#snippet archiveButton(compact: boolean)}
		<ArcaneButton
			action="base"
			icon={BoxIcon}
			size={compact ? 'icon' : undefined}
			showLabel={!compact}
			loading={isLoading.archiving}
			onclick={handleArchiveToggle}
			disabled={archiveRequiresStopped}
			title={archiveRequiresStopped ? m.projects_archive_requires_stopped() : undefined}
			customLabel={project?.isArchived ? m.projects_unarchive() : m.projects_archive()}
			class={compact ? 'xl:hidden' : 'hidden xl:inline-flex'}
		/>
	{/snippet}
	<TabbedPageLayout
		{backUrl}
		backLabel={m.common_back()}
		{tabItems}
		selectedTab={activeTab}
		onTabChange={(value: string) => {
			selectedTab = value as ProjectTab;
			urlTab.select(value);
			persistPrefs();
		}}
	>
		{#snippet headerInfo()}
			<div class="flex min-w-0 items-start gap-2.5">
				<IconImage
					src={getThemedIconUrl(project, mode.current)}
					alt={project.name}
					fallback={ProjectsIcon}
					class="size-6"
					containerClass="size-9 bg-transparent ring-0"
				/>
				<div class="min-w-0 flex-1">
					<div class="flex min-h-9 min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
						<EditableName
							bind:value={$inputs.name.value}
							displayValue={effectiveName}
							bind:ref={nameInputRef}
							variant="inline"
							error={$inputs.name.error ?? undefined}
							originalValue={serverName}
							canEdit={canEditName}
							disabledMessage={composeYamlName ? m.compose_project_name_defined_in_yaml() : undefined}
							onCommit={saveNameIfChanged}
							class="max-w-[10rem] min-w-0 sm:max-w-[14rem] md:max-w-[18rem] lg:max-w-[22rem]"
						/>
						{#if project.status}
							{@const showTooltip = project.status.toLowerCase() === 'unknown' && project.statusReason}
							{#if showTooltip}
								<ArcaneTooltip.Root>
									<ArcaneTooltip.Trigger>
										<Badge variant={getStatusVariant(project.status)} minWidth="20">
											{capitalizeFirstLetter(project.status)}
										</Badge>
									</ArcaneTooltip.Trigger>
									<ArcaneTooltip.Content>
										<p class="max-w-xs text-xs">{project.statusReason}</p>
									</ArcaneTooltip.Content>
								</ArcaneTooltip.Root>
							{:else}
								<Badge variant={getStatusVariant(project.status)} minWidth="20">
									{capitalizeFirstLetter(project.status)}
								</Badge>
							{/if}
						{/if}
						{#if project.isArchived}
							<Badge variant="gray" minWidth="20">{m.projects_archived_badge()}</Badge>
						{/if}
						<ProjectUpdateItem
							updateInfo={project.updateInfo}
							onCheck={handleCheckProjectUpdates}
							checking={checkProjectUpdatesMutation.isPending}
							disabled={!!project.isArchived}
						/>
					</div>

					{#if project.urls && project.urls.length > 0}
						<div class="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
							{#each project.urls as url, i (i)}
								<a
									class="inline-flex h-6 max-w-[10rem] min-w-0 items-center gap-1.5 rounded-[var(--radius)] border border-sky-700/20 bg-background/70 px-2.5 text-[12px] font-semibold ring-offset-background transition-colors hover:border-sky-700/40 hover:bg-sky-500/10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none sm:max-w-[14rem] md:max-w-[18rem] dark:border-sky-400/40 dark:bg-sky-500/20 dark:text-sky-100 dark:hover:border-sky-300/60 dark:hover:bg-sky-500/30"
									href={toSafeHref(url)}
									target="_blank"
									rel="noopener noreferrer"
									title={url}
								>
									<GlobeIcon class="size-3 text-sky-500" />
									<span class="truncate">{formatUrlLabel(url)}</span>
								</a>
							{/each}
						</div>
					{/if}

					{#if project.lastSyncCommit}
						{@const commitUrl = project.gitRepositoryURL
							? toGitCommitUrl(project.gitRepositoryURL, project.lastSyncCommit)
							: null}
						<div class="mt-1 flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
							<div class="flex items-center gap-1.5">
								<span class="hidden sm:inline">{m.commit()}:</span>
								{#if commitUrl}
									<a
										href={commitUrl}
										target="_blank"
										class="font-mono transition-colors hover:text-primary sm:rounded sm:bg-muted sm:px-1.5 sm:py-0.5"
									>
										{project.lastSyncCommit}
									</a>
								{:else}
									<span class="font-mono sm:rounded sm:bg-muted sm:px-1.5 sm:py-0.5">
										{project.lastSyncCommit}
									</span>
								{/if}
							</div>
						</div>
					{/if}
				</div>
			</div>
		{/snippet}

		{#snippet headerActions()}
			<div class="flex items-center gap-2">
				{#if hasChanges && canUpdateProject}
					<ArcaneButton
						action="save"
						loading={isLoading.saving}
						onclick={handleSaveChanges}
						disabled={!canSave}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
						class="hidden xl:inline-flex"
					/>
					<ArcaneButton
						action="save"
						size="icon"
						showLabel={false}
						loading={isLoading.saving}
						onclick={handleSaveChanges}
						disabled={!canSave}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
						class="xl:hidden"
					/>
				{/if}
				<IfPermitted perm="projects:archive">
					{@render archiveButton(false)}
					{@render archiveButton(true)}
				</IfPermitted>
				<ActionButtons
					id={project.id}
					name={project.name}
					type="project"
					itemState={project.status}
					{hasBuildDirective}
					desktopVariant="adaptive"
					disableRedeploy={!!project.redeployDisabled}
					bind:startLoading={isLoading.deploying}
					bind:stopLoading={isLoading.stopping}
					bind:restartLoading={isLoading.restarting}
					bind:removeLoading={isLoading.removing}
					bind:redeployLoading={isLoading.redeploying}
					onActionComplete={() => {
						void refreshProjectDetails();
					}}
					onRefresh={() => refreshProjectDetails()}
				/>
			</div>
		{/snippet}

		{#snippet tabContent()}
			<Tabs.Content value="services" class="h-full">
				<ProjectContainersTable services={project.runtimeServices} {projectId} onRefresh={() => refreshProjectDetails()} />
			</Tabs.Content>

			<Tabs.Content value="compose" class="h-full min-h-0">
				<div class="flex h-full min-h-0 flex-col">
					{#if isGitOpsManaged}
						<Alert.Root variant="default" class="mb-4">
							<AlertIcon class="size-4" />
							<div class="flex flex-col items-start justify-between gap-4 sm:flex-row sm:items-center">
								<div class="flex-1">
									<Alert.Title>{m.git()} {m.read_only_label()}</Alert.Title>
									<Alert.Description>
										{m.git_managed_readonly_alert()}
										<br />
										<div class="mt-2 flex flex-col gap-1">
											{#if project.lastSyncCommit}
												{@const commitUrl = project.gitRepositoryURL
													? toGitCommitUrl(project.gitRepositoryURL, project.lastSyncCommit)
													: null}
												<div class="flex items-center gap-1.5 font-mono text-xs">
													<span class="text-muted-foreground">{m.commit()}:</span>
													{#if commitUrl}
														<a
															href={commitUrl}
															target="_blank"
															class="rounded bg-muted px-1.5 py-0.5 transition-colors hover:text-primary"
														>
															{project.lastSyncCommit}
														</a>
													{:else}
														<span class="rounded bg-muted px-1.5 py-0.5">{project.lastSyncCommit}</span>
													{/if}
												</div>
											{/if}
											{#if hasLifecycleHook && lifecycleSync?.preDeployScriptPath}
												<div class="flex items-center gap-1.5 font-mono text-xs">
													<span class="text-muted-foreground">{m.git_sync_pre_deploy_title()}:</span>
													<span class="rounded bg-muted px-1.5 py-0.5">{lifecycleSync.preDeployScriptPath}</span>
													<span class="text-muted-foreground">
														{m.lifecycle_inline_runner_summary({
															image: lifecycleSync.preDeployRunnerImage || 'alpine:latest',
															network: lifecycleSync.preDeployNetworkMode || 'none'
														})}
													</span>
												</div>
											{/if}
											<span class="text-xs text-muted-foreground">
												{m.git_managed_env_note()}
											</span>
										</div>
									</Alert.Description>
								</div>
								{#if canUpdateProject}
									<ArcaneButton
										action="base"
										tone="outline-primary"
										loading={isLoading.syncing}
										onclick={handleSyncFromGit}
										icon={RefreshIcon}
										customLabel={m.git_sync_from_git()}
										loadingLabel={m.common_syncing()}
										class="shrink-0"
									/>
								{/if}
							</div>
						</Alert.Root>
					{/if}
					<div class="mb-2 flex shrink-0 items-center justify-end gap-2">
						<label
							for="layout-mode-toggle"
							class="cursor-pointer text-xs text-muted-foreground"
							title={m.project_view_description()}
						>
							{m.workspace()}
						</label>
						<Switch
							id="layout-mode-toggle"
							checked={layoutMode === 'tree'}
							aria-label={m.project_view_description()}
							onCheckedChange={(checked) => {
								layoutMode = checked ? 'tree' : 'classic';
								if (checked) {
									openFileTab('compose');
									selectedIncludeTabPreference = null;
								}
								persistPrefs();
							}}
						/>
					</div>

					<div class="min-h-0 flex-1">
						{#if layoutMode === 'tree'}
							<div class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-card">
								<ResizableSplit
									class="h-full min-h-0 flex-1"
									{...composeTreeSplitProps}
									bind:size={treePaneWidth}
									ariaLabel={m.compose_editor_resize_files_panel()}
									persistKey={`arcane.compose.split:${project.id}:tree`}
									onResizeEnd={persistPrefs}
								>
									{#snippet first()}
										<ProjectFileTreePanel
											{composeFileName}
											{overrideFileName}
											showOverride={overrideActive}
											onAddOverride={canEditOverride ? handleAddOverride : undefined}
											entries={managedProjectFiles}
											{selectedFile}
											disabled={!canEditProjectFiles}
											readOnlyMessage={isGitOpsManaged ? m.project_files_readonly_git() : undefined}
											onSelect={selectManagedProjectFile}
											onCreateFile={createManagedProjectFile}
											onCreateFolder={createManagedProjectFolder}
											onRename={renameManagedProjectFile}
											onMove={moveManagedProjectFile}
											onDelete={deleteManagedProjectFile}
										/>
									{/snippet}

									{#snippet second()}
										<div class="flex h-full min-h-0 flex-1 flex-col">
											<!-- fallow-ignore-next-line code-duplication -- compose editor tree panel; per-page bindings/persistKey/file-rendering diverge -->
											<EditorTabStrip tabs={treeTabs} activeKey={activeTreeTab} onSelect={openFileTab} onClose={closeFileTab}>
												{#snippet actions()}
													<ComposeFileEditorPanel
														outlineOpen={treeOutlineOpen}
														outlineLabel={m.compose_editor_toggle_outline()}
														onToggleOutline={() => (treeOutlineOpen = !treeOutlineOpen)}
														diffOpen={treeDiffOpen}
														diffLabel={m.compose_editor_toggle_diff()}
														onToggleDiff={() => (treeDiffOpen = !treeDiffOpen)}
														commandPaletteLabel={m.compose_editor_command_palette()}
														onOpenCommandPalette={() => (treeCommandPaletteOpen = true)}
													/>
												{/snippet}
											</EditorTabStrip>
											<div class="flex min-h-0 flex-1 flex-col">
												{#key activeTreeTab}
													{#if activeTreeTab === 'compose'}
														<CodePanel
															variant="plain"
															{...composePanelProps()}
															bind:open={composeOpen}
															bind:value={$inputs.composeContent.value}
															bind:hasErrors={composeHasErrors}
															bind:validationReady={composeValidationReady}
															bind:outlineOpen={treeOutlineOpen}
															bind:diffOpen={treeDiffOpen}
															bind:commandPaletteOpen={treeCommandPaletteOpen}
														/>
													{:else if activeTreeTab === 'override'}
														<div class="flex min-h-0 flex-1 flex-col">
															<div class="flex shrink-0 items-center justify-between gap-2 border-b border-border px-3 py-2">
																<p class="text-xs text-muted-foreground">{m.compose_override_hint()}</p>
																{#if canEditOverride}
																	<button
																		type="button"
																		class="flex shrink-0 items-center gap-1 text-xs font-medium text-muted-foreground hover:text-destructive"
																		onclick={handleRemoveOverride}
																	>
																		<TrashIcon class="size-3.5 shrink-0" />
																		<span>{m.compose_override_remove()}</span>
																	</button>
																{/if}
															</div>
															<CodePanel
																variant="plain"
																open={true}
																{...overridePanelProps()}
																bind:value={$inputs.overrideContent.value}
																bind:hasErrors={overrideHasErrors}
																bind:validationReady={overrideValidationReady}
																bind:outlineOpen={treeOutlineOpen}
																bind:diffOpen={treeDiffOpen}
																bind:commandPaletteOpen={treeCommandPaletteOpen}
															/>
														</div>
													{:else if activeTreeTab === 'env'}
														<CodePanel
															variant="plain"
															{...envPanelProps()}
															bind:open={envOpen}
															bind:value={$inputs.envContent.value}
															bind:hasErrors={envHasErrors}
															bind:validationReady={envValidationReady}
															bind:outlineOpen={treeOutlineOpen}
															bind:diffOpen={treeDiffOpen}
															bind:commandPaletteOpen={treeCommandPaletteOpen}
														/>
													{:else if activeTreeTab.startsWith('file:')}
														{@const relativePath = activeTreeTab.slice(5)}
														{#if managedProjectFileLoadErrors[relativePath]}
															<div class="flex h-full min-h-0 items-center justify-center px-4 text-sm text-destructive">
																{managedProjectFileLoadErrors[relativePath]}
															</div>
														{:else if managedProjectFileContents[relativePath] === undefined}
															<div class="flex h-full min-h-0 items-center justify-center text-muted-foreground">
																{m.common_loading()}
															</div>
														{:else}
															<CodePanel
																variant="plain"
																open={true}
																title={relativePath}
																language={projectFileLanguage(relativePath)}
																validationMode="none"
																bind:value={managedProjectFileContents[relativePath]}
																readOnly={!canEditProjectFiles}
																bind:hasErrors={managedProjectFileHasErrors[relativePath]}
																bind:validationReady={managedProjectFileValidationReady[relativePath]}
																fileId={`project:${projectId}:file:${relativePath}`}
																originalValue={loadedManagedProjectFileContents[relativePath] ?? ''}
																enableDiff={true}
																editorContext={codeEditorContext}
																bind:outlineOpen={treeOutlineOpen}
																bind:diffOpen={treeDiffOpen}
																bind:commandPaletteOpen={treeCommandPaletteOpen}
															/>
														{/if}
													{/if}
												{/key}
											</div>
										</div>
									{/snippet}
								</ResizableSplit>
							</div>
						{:else}
							<div class="flex h-full min-h-0 flex-col gap-4">
								{#if (project?.includeFiles && project.includeFiles.length > 0) || (hasLifecycleHook && lifecycleSync?.preDeployScriptPath && directoryFilePaths.has(lifecycleSync.preDeployScriptPath))}
									<div class="rounded-lg border border-border bg-card">
										<div class="scrollbar-hide flex gap-2 overflow-x-auto border-b border-border p-2">
											{#each project?.includeFiles ?? [] as includeFile (includeFile.relativePath)}
												<ArcaneButton
													action="base"
													tone={selectedIncludeTab === includeFile.relativePath ? 'outline-primary' : 'ghost'}
													size="sm"
													class="shrink-0"
													onclick={() => toggleIncludeFileTab(includeFile.relativePath)}
													icon={FileTextIcon}
													customLabel={includeFile.relativePath}
												/>
											{/each}
											{#if hasLifecycleHook && lifecycleSync?.preDeployScriptPath && directoryFilePaths.has(lifecycleSync.preDeployScriptPath)}
												{@const scriptPath = lifecycleSync.preDeployScriptPath}
												<ArcaneButton
													action="base"
													tone={selectedIncludeTab === scriptPath ? 'outline-primary' : 'ghost'}
													size="sm"
													class="shrink-0"
													onclick={() => toggleIncludeFileTab(scriptPath)}
													icon={CodeIcon}
													customLabel={scriptPath}
												/>
											{/if}
										</div>
									</div>
								{/if}

								{#if selectedIncludeTab}
									{@const includeFile = project?.includeFiles?.find((f) => f.relativePath === selectedIncludeTab)}
									{@const dirFile = !includeFile
										? project?.directoryFiles?.find((f) => f.relativePath === selectedIncludeTab)
										: undefined}
									{@const fileKind = includeFile ? 'include' : 'directory'}
									{#await getProjectFileResource(fileKind, selectedIncludeTab)}
										<div class="flex h-full min-h-0 items-center justify-center rounded-lg border text-muted-foreground">
											{m.common_loading()}
										</div>
									{:then loaded}
										{#if includeFile}
											<CodePanel
												bind:open={includeFilesPanelStates[includeFile.relativePath]}
												title={includeFile.relativePath}
												language="yaml"
												validationMode="compose"
												bind:value={includeFilesState[includeFile.relativePath]}
												bind:hasErrors={includeFilesHasErrors[includeFile.relativePath]}
												bind:validationReady={includeFilesValidationReady[includeFile.relativePath]}
												fileId={`project:${projectId}:include:${includeFile.relativePath}`}
												originalValue={serverIncludeFiles[includeFile.relativePath] ?? ''}
												enableDiff={true}
												editorContext={codeEditorContext}
											/>
										{:else if dirFile}
											<CodePanel
												open={true}
												title={loaded.relativePath}
												language="env"
												value={loaded.content ?? ''}
												readOnly={true}
											/>
										{/if}
									{:catch error}
										<div class="flex h-full min-h-0 items-center justify-center rounded-lg border px-4 text-sm text-destructive">
											{error instanceof Error ? error.message : String(error)}
										</div>
									{/await}
								{:else}
									<ResizableSplit
										class="min-h-0 flex-1 lg:gap-2"
										firstClass="flex min-h-0 flex-col"
										secondClass="flex min-h-0 flex-col"
										bind:size={composeSplitWidth}
										minSize={minComposePaneWidth}
										minSecondSize={minEnvPaneWidth}
										defaultRatio={0.6}
										stackBelow={1024}
										ariaLabel={m.compose_editor_resize_compose_env()}
										persistKey={`arcane.compose.split:${project.id}:classic`}
										onResizeEnd={persistPrefs}
									>
										{#snippet first()}
											{@const overrideExpanded = overrideActive && overrideOpen}
											<div class="flex min-h-0 flex-1 flex-col gap-2">
												{#if overrideExpanded}
													<button
														type="button"
														class="flex shrink-0 items-center gap-2 rounded-lg border border-border bg-card px-2 py-2 text-sm font-medium hover:text-foreground"
														aria-expanded="false"
														onclick={() => (overrideOpen = false)}
													>
														<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
														<CodeIcon class="size-4 shrink-0 text-muted-foreground" />
														<span class="truncate">{composeFileName}</span>
														{#if composeHasChanges}
															<span
																class="size-1.5 shrink-0 rounded-full bg-primary"
																role="img"
																aria-label={m.common_unsaved_changes()}
															></span>
														{/if}
													</button>
												{:else}
													<div class="flex min-h-0 flex-1 flex-col">
														<CodePanel
															{...composePanelProps()}
															bind:open={composeOpen}
															bind:value={$inputs.composeContent.value}
															bind:hasErrors={composeHasErrors}
															bind:validationReady={composeValidationReady}
														/>
													</div>
												{/if}
												{#if overrideActive}
													<div
														class="flex min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-card {overrideExpanded
															? 'flex-1'
															: 'shrink-0'}"
													>
														<div
															class="flex shrink-0 items-center gap-1 px-2 py-1 {overrideExpanded
																? 'border-b border-border'
																: ''}"
														>
															<button
																type="button"
																class="flex min-w-0 flex-1 items-center gap-2 py-1 text-sm font-medium hover:text-foreground"
																aria-expanded={overrideOpen}
																onclick={() => (overrideOpen = !overrideOpen)}
															>
																{#if overrideOpen}
																	<ArrowDownIcon class="size-4 shrink-0 text-muted-foreground" />
																{:else}
																	<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
																{/if}
																<FileTextIcon class="size-4 shrink-0 text-muted-foreground" />
																<span class="truncate">{overrideFileName}</span>
																{#if overrideHasChanges}
																	<span
																		class="size-1.5 shrink-0 rounded-full bg-primary"
																		role="img"
																		aria-label={m.common_unsaved_changes()}
																	></span>
																{/if}
																{#if !overrideOpen}
																	<span class="hidden truncate text-xs font-normal text-muted-foreground sm:inline"
																		>{m.compose_override_hint()}</span
																	>
																{/if}
															</button>
															{#if overrideExpanded}
																<ArcaneButton
																	action="base"
																	tone={overrideOutlineOpen ? 'outline-primary' : 'ghost'}
																	size="icon"
																	showLabel={false}
																	icon={FileTextIcon}
																	customLabel={m.compose_editor_toggle_outline()}
																	onclick={() => (overrideOutlineOpen = !overrideOutlineOpen)}
																/>
																<ArcaneButton
																	action="base"
																	tone={overrideDiffOpen ? 'outline-primary' : 'ghost'}
																	size="icon"
																	showLabel={false}
																	icon={ArrowsUpDownIcon}
																	customLabel={m.compose_editor_toggle_diff()}
																	onclick={() => (overrideDiffOpen = !overrideDiffOpen)}
																/>
																<ArcaneButton
																	action="base"
																	tone="ghost"
																	size="icon"
																	showLabel={false}
																	icon={SearchIcon}
																	customLabel={m.compose_editor_command_palette()}
																	onclick={() => (overrideCommandPaletteOpen = true)}
																/>
															{/if}
															{#if canEditOverride}
																<button
																	type="button"
																	class="flex shrink-0 items-center p-1.5 text-muted-foreground hover:text-destructive"
																	onclick={handleRemoveOverride}
																	aria-label={m.compose_override_remove()}
																>
																	<TrashIcon class="size-4 shrink-0" />
																</button>
															{/if}
														</div>
														{#if overrideExpanded}
															<div class="flex min-h-0 flex-1 flex-col">
																<CodePanel
																	{...overridePanelProps()}
																	variant="plain"
																	bind:value={$inputs.overrideContent.value}
																	bind:hasErrors={overrideHasErrors}
																	bind:validationReady={overrideValidationReady}
																	bind:outlineOpen={overrideOutlineOpen}
																	bind:diffOpen={overrideDiffOpen}
																	bind:commandPaletteOpen={overrideCommandPaletteOpen}
																/>
															</div>
														{/if}
													</div>
												{:else if canEditOverride}
													<button
														type="button"
														class="flex shrink-0 items-center gap-2 rounded-lg border border-dashed border-border px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-card hover:text-foreground"
														onclick={handleAddOverride}
													>
														<CreateFileIcon class="size-4 shrink-0" />
														<span>{m.compose_override_add()}</span>
													</button>
												{/if}
											</div>
										{/snippet}

										{#snippet second()}
											<div class="flex min-h-0 flex-1 flex-col">
												<CodePanel
													{...envPanelProps()}
													bind:open={envOpen}
													bind:value={$inputs.envContent.value}
													bind:hasErrors={envHasErrors}
													bind:validationReady={envValidationReady}
												/>
											</div>
										{/snippet}
									</ResizableSplit>
								{/if}
							</div>
						{/if}
					</div>
				</div>
			</Tabs.Content>

			<Tabs.Content value="logs" class="h-full">
				{#if project.status == 'running'}
					<ProjectsLogsPanel projectId={project.id} bind:autoScroll={autoScrollStackLogs} />
				{:else}
					<div class="py-12 text-center text-muted-foreground">{m.compose_logs_title()} {m.common_disabled()}</div>
				{/if}
			</Tabs.Content>
		{/snippet}
	</TabbedPageLayout>
{:else}
	<div class="flex min-h-screen items-center justify-center">
		<div class="text-center">
			<div class="mb-6 inline-flex rounded-full bg-muted/50 p-6">
				<ProjectsIcon class="size-10 text-muted-foreground" />
			</div>
			<h2 class="mb-3 text-2xl font-medium">
				{data.error ? m.common_action_failed() : m.common_not_found_title({ resource: m.project() })}
			</h2>
			<p class="mb-8 max-w-md text-center text-muted-foreground">
				{data.error || m.common_not_found_description({ resource: m.project().toLowerCase() })}
			</p>
			<ArcaneButton
				action="base"
				tone="outline"
				href="/projects"
				icon={ArrowLeftIcon}
				customLabel={m.common_back_to({ resource: m.projects_title() })}
			/>
		</div>
	</div>
{/if}

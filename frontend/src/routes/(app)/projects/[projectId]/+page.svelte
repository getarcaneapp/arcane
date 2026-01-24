<script lang="ts">
	import type { Project } from '$lib/types/project.type';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as TreeView from '$lib/components/ui/tree-view/index.js';
	import * as Card from '$lib/components/ui/card';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import {
		ArrowLeftIcon,
		ProjectsIcon,
		LayersIcon,
		SettingsIcon,
		FileTextIcon,
		FileSymlinkIcon,
		FilePenIcon,
		AddIcon,
		TrashIcon,
		AlertIcon,
		FolderOpenIcon,
		CloseIcon
	} from '$lib/icons';
	import * as ContextMenu from '$lib/components/ui/context-menu/index.js';
	import ResponsiveDialog from '$lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import { type TabItem } from '$lib/components/tab-bar/index.js';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import ActionButtons from '$lib/components/action-buttons.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { getStatusVariant } from '$lib/utils/status.utils';
	import { capitalizeFirstLetter } from '$lib/utils/string.utils';
	import { invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { z } from 'zod/v4';
	import { createForm } from '$lib/utils/form.utils';
	import { m } from '$lib/paraglide/messages';
	import { PersistedState } from 'runed';
	import EditableName from '../components/EditableName.svelte';
	import ServicesGrid from '../components/ServicesGrid.svelte';
	import CodePanel from '../components/CodePanel.svelte';
	import ProjectsLogsPanel from '../components/ProjectLogsPanel.svelte';
	import { Resizable } from '$lib/components/resizable';
	import { untrack, onMount } from 'svelte';
	import { projectService } from '$lib/services/project-service';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { RefreshIcon } from '$lib/icons';

	let { data } = $props();
	let projectId = $derived(data.projectId);
	let project = $state(untrack(() => data.project));
	let editorState = $derived(data.editorState);

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
		syncing: false
	});

	const envId = $derived(environmentStore.selected?.id);

	let originalName = $state(untrack(() => data.editorState.originalName));
	let originalComposeContent = $state(untrack(() => data.editorState.originalComposeContent));
	let originalEnvContent = $state(untrack(() => data.editorState.originalEnvContent || ''));
	let includeFilesState = $state<Record<string, string>>({});
	let originalIncludeFiles = $state<Record<string, string>>({});
	let customFilesState = $state<Record<string, string>>({});
	let originalCustomFiles = $state<Record<string, string>>({});
	let isAddingCustomFile = $state(false);
	let newCustomFileName = $state('');
	let newFileInputRef = $state<HTMLInputElement | null>(null);

	const formSchema = z.object({
		name: z
			.string()
			.min(1, m.compose_project_name_required())
			.regex(/^[a-z0-9_-]+$/i, m.compose_project_name_invalid_with_underscores()),
		composeContent: z.string().min(1, m.compose_compose_content_required()),
		envContent: z.string().optional().default('')
	});

	let formData = $derived({
		name: editorState.name,
		composeContent: editorState.composeContent,
		envContent: editorState.envContent || ''
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	let hasChanges = $derived(
		$inputs.name.value !== originalName ||
			$inputs.composeContent.value !== originalComposeContent ||
			$inputs.envContent.value !== originalEnvContent ||
			JSON.stringify(includeFilesState) !== JSON.stringify(originalIncludeFiles) ||
			JSON.stringify(customFilesState) !== JSON.stringify(originalCustomFiles)
	);

	let isGitOpsManaged = $derived(!!project?.gitOpsManagedBy);
	let canEditName = $derived(
		!isGitOpsManaged && !isLoading.saving && project?.status !== 'running' && project?.status !== 'partially running'
	);
	let canEditCompose = $derived(!isGitOpsManaged);

	let autoScrollStackLogs = $state(true);

	let selectedTab = $state<'services' | 'compose' | 'logs'>('compose');
	let mobileFileDrawerOpen = $state(false);
	const minTreePaneWidth = 180;
	const minEditorPaneWidth = 280;

	// Split view: left and right pane files (desktop)
	let leftPaneFile = $state<string>('compose');
	let rightPaneFile = $state<string | null>('env');

	// Mobile: single file selector (shows compose+env stacked if compose or env selected)
	let mobileSelectedFile = $state<string>('compose');

	// Check if mobile should show the stacked compose/env view
	let mobileShowsMainFiles = $derived(mobileSelectedFile === 'compose' || mobileSelectedFile === 'env');

	// Determine if we're in split view mode (desktop)
	let isSplitView = $derived(rightPaneFile !== null);
	let hasExtraFiles = $derived(
		(project?.includeFiles && project.includeFiles.length > 0) || (project?.customFiles && project.customFiles.length > 0)
	);

	// Check if a file is currently in split view (desktop)
	function isFileInSplitView(file: string): boolean {
		return file === leftPaneFile || file === rightPaneFile;
	}

	// Check if a file is selected on mobile
	function isMobileFileSelected(file: string): boolean {
		// compose and env are both "selected" when either is the active file
		if (mobileShowsMainFiles && (file === 'compose' || file === 'env')) {
			return true;
		}
		return file === mobileSelectedFile;
	}

	// Add file to split view (right pane)
	function addToSplitView(file: string) {
		// Don't allow same file in both panes
		if (file === leftPaneFile) return;
		rightPaneFile = file;
		persistPrefs();
	}

	// Remove right pane from split view
	function removeFromSplitView() {
		rightPaneFile = null;
		persistPrefs();
	}

	// Select file (opens in left pane on desktop, or sets mobile selection)
	function selectFile(file: string) {
		// Desktop: manage split view
		// If file is already in right pane, swap panes
		if (file === rightPaneFile) {
			const temp = leftPaneFile;
			leftPaneFile = file;
			rightPaneFile = temp;
		} else {
			leftPaneFile = file;
			// If the new left pane file is the same as right, close split
			if (rightPaneFile === file) {
				rightPaneFile = null;
			}
		}

		// Mobile: update selected file
		mobileSelectedFile = file;

		persistPrefs();
	}

	const tabItems = $derived<TabItem[]>([
		{
			value: 'services',
			label: m.compose_nav_services(),
			icon: LayersIcon,
			badge: project?.serviceCount
		},
		{
			value: 'compose',
			label: m.common_configuration(),
			icon: SettingsIcon
		},
		{
			value: 'logs',
			label: m.compose_nav_logs(),
			icon: FileTextIcon,
			disabled: project?.status !== 'running'
		}
	]);

	let nameInputRef = $state<HTMLInputElement | null>(null);

	type ComposeUIPrefs = {
		tab: 'services' | 'compose' | 'logs';
		autoScroll: boolean;
		leftPaneFile: string;
		rightPaneFile: string | null;
		mobileSelectedFile: string;
	};

	const defaultComposeUIPrefs: ComposeUIPrefs = {
		tab: 'compose',
		autoScroll: true,
		leftPaneFile: 'compose',
		rightPaneFile: 'env',
		mobileSelectedFile: 'compose'
	};

	let prefs: PersistedState<ComposeUIPrefs> | null = null;
	let initializedProjectId: string | null = null;

	$effect(() => {
		project = data.project;
	});

	// Initialize prefs on mount and when project ID changes
	onMount(() => {
		initializePrefs();
	});

	// Also initialize if project changes after mount
	$effect(() => {
		const currentProjectId = project?.id;
		if (currentProjectId && currentProjectId !== initializedProjectId) {
			initializePrefs();
		}
	});

	function initializePrefs() {
		const currentProjectId = project?.id;
		if (!currentProjectId) return;
		if (currentProjectId === initializedProjectId) return;

		initializedProjectId = currentProjectId;

		// Create new PersistedState for this project
		prefs = new PersistedState<ComposeUIPrefs>(`arcane.compose.ui:${currentProjectId}`, defaultComposeUIPrefs, {
			storage: 'session',
			syncTabs: false
		});

		// Read persisted values - use explicit undefined checks since null is a valid value for rightPaneFile
		const cur = prefs.current ?? {};
		selectedTab = cur.tab ?? defaultComposeUIPrefs.tab;
		autoScrollStackLogs = cur.autoScroll ?? defaultComposeUIPrefs.autoScroll;
		leftPaneFile = cur.leftPaneFile ?? defaultComposeUIPrefs.leftPaneFile;
		// rightPaneFile can be null (meaning no split view), so only use default if undefined
		rightPaneFile = 'rightPaneFile' in cur ? cur.rightPaneFile : defaultComposeUIPrefs.rightPaneFile;
		mobileSelectedFile = cur.mobileSelectedFile ?? defaultComposeUIPrefs.mobileSelectedFile;
	}

	// Initialize file states when project data changes
	$effect(() => {
		// Initialize include file states
		if (project?.includeFiles) {
			const newIncludeState: Record<string, string> = {};
			project.includeFiles.forEach((file) => {
				newIncludeState[file.path] = file.content;
			});
			includeFilesState = newIncludeState;
			originalIncludeFiles = { ...newIncludeState };
		}

		// Initialize custom file states
		if (project?.customFiles) {
			const newCustomState: Record<string, string> = {};
			project.customFiles.forEach((file) => {
				newCustomState[file.path] = file.content;
			});
			customFilesState = newCustomState;
			originalCustomFiles = { ...newCustomState };
		}
	});

	async function handleSaveChanges() {
		if (!project || !hasChanges) return;

		const formValues = form.data();
		const validated = isGitOpsManaged ? formValues : form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;
		const namePayload = isGitOpsManaged ? undefined : name;
		const composePayload = isGitOpsManaged ? undefined : composeContent;

		// First update the main project files
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.updateProject(projectId, namePayload, composePayload, envContent)),
			message: m.common_save_failed(),
			setLoadingState: (value) => (isLoading.saving = value),
			onSuccess: async (updatedStack: Project) => {
				// Then update any changed include files
				for (const filePath of Object.keys(includeFilesState)) {
					if (includeFilesState[filePath] !== originalIncludeFiles[filePath]) {
						const includeResult = await tryCatch(
							projectService.updateProjectIncludeFile(projectId, filePath, includeFilesState[filePath])
						);
						if (includeResult.error) {
							toast.error(includeResult.error.message || m.common_update_failed({ resource: filePath }));
							return;
						}
					}
				}

				// Then update any changed custom files
				for (const filePath of Object.keys(customFilesState)) {
					if (customFilesState[filePath] !== originalCustomFiles[filePath]) {
						const customResult = await tryCatch(
							projectService.updateProjectCustomFile(projectId, filePath, customFilesState[filePath])
						);
						if (customResult.error) {
							toast.error(m.common_update_failed({ resource: filePath }));
							return;
						}
					}
				}

				toast.success(m.common_update_success({ resource: m.project() }));
				originalName = updatedStack.name;
				originalComposeContent = $inputs.composeContent.value;
				originalEnvContent = $inputs.envContent.value;
				originalIncludeFiles = { ...includeFilesState };
				originalCustomFiles = { ...customFilesState };
				await new Promise((resolve) => setTimeout(resolve, 200));
				await invalidateAll();
			}
		});
	}

	function startAddingCustomFile() {
		isAddingCustomFile = true;
		newCustomFileName = '';
		// Focus the input after it renders
		requestAnimationFrame(() => {
			newFileInputRef?.focus();
		});
	}

	function cancelAddingCustomFile() {
		isAddingCustomFile = false;
		newCustomFileName = '';
	}

	async function handleAddCustomFile() {
		const filePath = newCustomFileName.trim();

		if (!filePath) {
			cancelAddingCustomFile();
			return;
		}

		const result = await tryCatch(projectService.createProjectCustomFile(projectId, filePath));
		if (result.error) {
			toast.error(m.project_custom_file_add_failed({ error: result.error.message || m.common_unknown() }));
			return;
		}

		toast.success(m.project_custom_file_add_success({ path: filePath }));
		isAddingCustomFile = false;
		newCustomFileName = '';
		await invalidateAll();
	}

	function handleNewFileKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			handleAddCustomFile();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			cancelAddingCustomFile();
		}
	}

	function handleRemoveCustomFile(filePath: string) {
		openConfirmDialog({
			title: m.project_custom_file_remove_title(),
			message: m.project_custom_file_remove_message({ path: filePath }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async (checkboxStates) => {
					const deleteFromDisk = checkboxStates['deleteFromDisk'] === true;
					const result = await tryCatch(projectService.removeProjectCustomFile(projectId, filePath, deleteFromDisk));
					if (result.error) {
						toast.error(m.project_custom_file_remove_failed({ error: result.error.message || m.common_unknown() }));
						return;
					}

					// Remove from local state
					delete customFilesState[filePath];
					delete originalCustomFiles[filePath];

					// Reset panes if the removed file was active
					const customFileKey = `custom:${filePath}`;
					if (leftPaneFile === customFileKey) {
						leftPaneFile = 'compose';
					}
					if (rightPaneFile === customFileKey) {
						rightPaneFile = null;
					}

					toast.success(m.project_custom_file_removed({ path: filePath }));
					await invalidateAll();
				}
			},
			checkboxes: [{ id: 'deleteFromDisk', label: m.project_custom_file_delete_from_disk(), initialState: false }]
		});
	}

	function saveNameIfChanged() {
		if ($inputs.name.value === originalName) return;
		const validated = form.validate();
		if (!validated) return;
		handleSaveChanges();
	}

	function persistPrefs() {
		if (!prefs) return;
		prefs.current = {
			tab: selectedTab,
			autoScroll: autoScrollStackLogs,
			leftPaneFile,
			rightPaneFile,
			mobileSelectedFile
		};
	}

	async function refreshProjectDetails() {
		if (!projectId) return;
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.getProject(projectId)),
			message: m.common_refresh_failed({ resource: m.project() }),
			onSuccess: (updatedProject) => {
				project = updatedProject;
			}
		});
	}

	async function handleSyncFromGit() {
		if (!envId || !project?.gitOpsManagedBy) return;
		isLoading.syncing = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(gitOpsSyncService.performSync(envId, project.gitOpsManagedBy)),
			message: m.git_sync_failed(),
			setLoadingState: (value) => (isLoading.syncing = value),
			onSuccess: async () => {
				toast.success(m.git_sync_success());
				await invalidateAll();
			}
		});
	}
</script>

{#snippet fileEditor(file: string, isRightPane: boolean = false)}
	{#snippet closeButton()}
		{#if isRightPane}
			<Button
				variant="ghost"
				size="icon"
				class="text-muted-foreground hover:text-foreground size-7"
				onclick={() => removeFromSplitView()}
				title={m.project_close_split_pane()}
			>
				<CloseIcon class="size-4" />
			</Button>
		{/if}
	{/snippet}

	{#if file === 'compose'}
		<CodePanel
			open={true}
			title="compose.yaml"
			language="yaml"
			bind:value={$inputs.composeContent.value}
			error={$inputs.composeContent.error ?? undefined}
			readOnly={!canEditCompose}
		>
			{#snippet headerAction()}
				{@render closeButton()}
			{/snippet}
		</CodePanel>
	{:else if file === 'env'}
		<CodePanel
			open={true}
			title=".env"
			language="env"
			bind:value={$inputs.envContent.value}
			error={$inputs.envContent.error ?? undefined}
		>
			{#snippet headerAction()}
				{@render closeButton()}
			{/snippet}
		</CodePanel>
	{:else if file.startsWith('custom:')}
		{@const customPath = file.replace('custom:', '')}
		{@const customFile = project?.customFiles?.find((f) => f.path === customPath)}
		{#if customFile && customFile.path in customFilesState}
			<CodePanel open={true} language="yaml" bind:value={customFilesState[customFile.path]}>
				{#snippet headerTitle()}
					<span class="truncate" title={customFile.path}>{customFile.path}</span>
				{/snippet}
				{#snippet headerAction()}
					<div class="flex shrink-0 items-center gap-1">
						<Button
							variant="ghost"
							size="icon"
							class="text-muted-foreground hover:text-destructive size-7"
							onclick={() => handleRemoveCustomFile(customFile.path)}
							title={m.common_remove()}
						>
							<TrashIcon class="size-4" />
						</Button>
						{@render closeButton()}
					</div>
				{/snippet}
			</CodePanel>
		{/if}
	{:else}
		{@const includeFile = project?.includeFiles?.find((f) => f.path === file)}
		{#if includeFile && includeFile.path in includeFilesState}
			<CodePanel open={true} language="yaml" bind:value={includeFilesState[includeFile.path]}>
				{#snippet headerTitle()}
					<span class="truncate" title={includeFile.path}>{includeFile.path}</span>
				{/snippet}
				{#snippet headerAction()}
					<div class="flex shrink-0 items-center gap-1">
						{@render closeButton()}
					</div>
				{/snippet}
			</CodePanel>
		{/if}
	{/if}
{/snippet}

{#snippet mobileEditorPane()}
	<div class="flex h-full min-h-0 flex-1 flex-col gap-4">
		{#if mobileShowsMainFiles}
			<div class="flex min-h-0 flex-1 flex-col">
				{@render fileEditor('compose', false)}
			</div>
			<div class="flex min-h-0 flex-1 flex-col">
				{@render fileEditor('env', false)}
			</div>
		{:else}
			{@render fileEditor(mobileSelectedFile, false)}
		{/if}
	</div>
{/snippet}

{#if project}
	<TabbedPageLayout
		backUrl="/projects"
		backLabel={m.common_back()}
		{tabItems}
		{selectedTab}
		onTabChange={(value) => {
			selectedTab = value as 'services' | 'compose' | 'logs';
			persistPrefs();
		}}
	>
		{#snippet headerInfo()}
			<div class="flex items-center gap-2">
				<EditableName
					bind:value={$inputs.name.value}
					bind:ref={nameInputRef}
					variant="inline"
					error={$inputs.name.error ?? undefined}
					originalValue={originalName}
					canEdit={canEditName}
					onCommit={saveNameIfChanged}
					class="hidden sm:block"
				/>
				<EditableName
					bind:value={$inputs.name.value}
					bind:ref={nameInputRef}
					variant="block"
					error={$inputs.name.error ?? undefined}
					originalValue={originalName}
					canEdit={canEditName}
					onCommit={saveNameIfChanged}
					class="block sm:hidden"
				/>
				{#if project.status}
					{@const showTooltip = project.status.toLowerCase() === 'unknown' && project.statusReason}
					<StatusBadge
						variant={getStatusVariant(project.status)}
						text={capitalizeFirstLetter(project.status)}
						tooltip={showTooltip ? project.statusReason : undefined}
					/>
				{/if}
			</div>
			<div class="mt-0.5 flex items-center gap-4">
				{#if project.createdAt}
					<p class="text-muted-foreground hidden text-xs sm:block">
						{m.common_created()}: {new Date(project.createdAt ?? '').toLocaleDateString()}
					</p>
				{/if}
				{#if project.lastSyncCommit}
					<div class="text-muted-foreground flex items-center gap-1.5 text-xs">
						<span class="hidden sm:inline">{m.git_sync_commit()}:</span>
						{#if project.gitRepositoryURL}
							<a
								href="{project.gitRepositoryURL.replace(/\.git$/, '')}/commit/{project.lastSyncCommit}"
								target="_blank"
								class="hover:text-primary sm:bg-muted font-mono transition-colors sm:rounded sm:px-1.5 sm:py-0.5"
							>
								{project.lastSyncCommit}
							</a>
						{:else}
							<span class="sm:bg-muted font-mono sm:rounded sm:px-1.5 sm:py-0.5">
								{project.lastSyncCommit}
							</span>
						{/if}
					</div>
				{/if}
			</div>
		{/snippet}

		{#snippet headerActions()}
			<div class="flex items-center gap-2">
				{#if hasChanges}
					<ArcaneButton
						action="save"
						loading={isLoading.saving}
						onclick={handleSaveChanges}
						disabled={!hasChanges}
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
						disabled={!hasChanges}
						customLabel={m.common_save()}
						loadingLabel={m.common_saving()}
						class="xl:hidden"
					/>
				{/if}
				<ActionButtons
					id={project.id}
					name={project.name}
					type="project"
					itemState={project.status}
					desktopVariant="adaptive"
					bind:startLoading={isLoading.deploying}
					bind:stopLoading={isLoading.stopping}
					bind:restartLoading={isLoading.restarting}
					bind:removeLoading={isLoading.removing}
					bind:redeployLoading={isLoading.redeploying}
					onActionComplete={() => invalidateAll()}
					onRefresh={refreshProjectDetails}
				/>
			</div>
		{/snippet}

		{#snippet tabContent()}
			<Tabs.Content value="services" class="h-full">
				<ServicesGrid services={project.runtimeServices} {projectId} />
			</Tabs.Content>

			<Tabs.Content value="compose" class="h-full min-h-0">
				<div class="flex h-full min-h-0 flex-col">
					{#if isGitOpsManaged}
						<Alert.Root variant="default" class="mb-4">
							<AlertIcon class="size-4" />
							<div class="flex flex-col items-start justify-between gap-4 sm:flex-row sm:items-center">
								<div class="flex-1">
									<Alert.Title>{m.git_title()} {m.read_only_label()}</Alert.Title>
									<Alert.Description>
										{m.git_managed_readonly_alert()}
										<br />
										<div class="mt-2 flex flex-col gap-1">
											{#if project.lastSyncCommit}
												<div class="flex items-center gap-1.5 font-mono text-xs">
													<span class="text-muted-foreground">{m.git_sync_commit()}:</span>
													{#if project.gitRepositoryURL}
														<a
															href="{project.gitRepositoryURL.replace(/\.git$/, '')}/commit/{project.lastSyncCommit}"
															target="_blank"
															class="bg-muted hover:text-primary rounded px-1.5 py-0.5 transition-colors"
														>
															{project.lastSyncCommit}
														</a>
													{:else}
														<span class="bg-muted rounded px-1.5 py-0.5">{project.lastSyncCommit}</span>
													{/if}
												</div>
											{/if}
											<span class="text-muted-foreground text-xs">
												{m.git_managed_env_note()}
											</span>
										</div>
									</Alert.Description>
								</div>
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
							</div>
						</Alert.Root>
					{/if}
					<ResponsiveDialog bind:open={mobileFileDrawerOpen} title={m.project_files()} variant="sheet">
						{#snippet trigger()}
							<Button variant="outline" size="sm" class="mb-4 gap-2 lg:hidden">
								<FolderOpenIcon class="size-4" />
								<span>{m.project_files()}</span>
								{#if hasExtraFiles}
									<span class="bg-muted text-muted-foreground rounded-full px-1.5 py-0.5 text-xs">
										{(project?.includeFiles?.length ?? 0) + (project?.customFiles?.length ?? 0)}
									</span>
								{/if}
							</Button>
						{/snippet}
						<TreeView.Root class="space-y-1 p-1">
							<TreeView.Folder name={m.project_main_files()} open class="[&>div]:space-y-1">
								<TreeView.File
									name="compose.yaml"
									onclick={() => {
										selectFile('compose');
										mobileFileDrawerOpen = false;
									}}
									class="hover:bg-accent min-h-[44px] w-full rounded-lg px-3 py-2.5 text-base transition-colors {isMobileFileSelected(
										'compose'
									)
										? 'bg-accent'
										: ''}"
								>
									{#snippet icon()}
										<FileTextIcon class="size-5 text-blue-500" />
									{/snippet}
								</TreeView.File>
								<TreeView.File
									name=".env"
									onclick={() => {
										selectFile('env');
										mobileFileDrawerOpen = false;
									}}
									class="hover:bg-accent min-h-[44px] w-full rounded-lg px-3 py-2.5 text-base transition-colors {isMobileFileSelected(
										'env'
									)
										? 'bg-accent'
										: ''}"
								>
									{#snippet icon()}
										<FileTextIcon class="size-5 text-green-500" />
									{/snippet}
								</TreeView.File>
							</TreeView.Folder>

							{#if project?.includeFiles && project.includeFiles.length > 0}
								<TreeView.Folder name={m.project_includes()} class="[&>div]:space-y-1">
									{#each project.includeFiles as includeFile (includeFile.path)}
										<TreeView.File
											name={includeFile.path}
											onclick={() => {
												selectFile(includeFile.path);
												mobileFileDrawerOpen = false;
											}}
											class="hover:bg-accent min-h-[44px] w-full rounded-lg px-3 py-2.5 text-base transition-colors {isMobileFileSelected(
												includeFile.path
											)
												? 'bg-accent'
												: ''}"
										>
											{#snippet icon()}
												<FileSymlinkIcon class="size-5 text-amber-500" />
											{/snippet}
										</TreeView.File>
									{/each}
								</TreeView.Folder>
							{/if}

							<TreeView.Folder name={m.project_custom_files()} class="[&>div]:space-y-1">
								{#if project?.customFiles && project.customFiles.length > 0}
									{#each project.customFiles as customFile (customFile.path)}
										<TreeView.File
											name={customFile.path}
											onclick={() => {
												selectFile(`custom:${customFile.path}`);
												mobileFileDrawerOpen = false;
											}}
											class="hover:bg-accent min-h-[44px] w-full rounded-lg px-3 py-2.5 text-base transition-colors {isMobileFileSelected(
												`custom:${customFile.path}`
											)
												? 'bg-accent'
												: ''}"
										>
											{#snippet icon()}
												<FilePenIcon class="size-5 text-purple-500" />
											{/snippet}
										</TreeView.File>
									{/each}
								{/if}
								{#if isAddingCustomFile}
									<div class="flex min-h-[44px] items-center gap-2 rounded-lg px-3 py-2.5">
										<FilePenIcon class="size-5 shrink-0 text-purple-500" />
										<input
											bind:this={newFileInputRef}
											bind:value={newCustomFileName}
											onkeydown={handleNewFileKeydown}
											onblur={cancelAddingCustomFile}
											placeholder={m.project_custom_file_path_placeholder()}
											class="text-foreground placeholder:text-muted-foreground border-b-primary h-6 w-full min-w-0 border-b bg-transparent text-base outline-none"
										/>
									</div>
								{:else}
									<button
										class="hover:bg-accent text-muted-foreground hover:text-foreground flex min-h-[44px] w-full cursor-pointer items-center gap-2 rounded-lg px-3 py-2.5 text-base transition-colors"
										onclick={startAddingCustomFile}
									>
										<AddIcon class="size-5" />
										<span>{m.project_custom_file_add_button()}...</span>
									</button>
								{/if}
							</TreeView.Folder>
						</TreeView.Root>
					</ResponsiveDialog>

					<div class="hidden min-h-0 flex-1 lg:flex">
						<Resizable.PaneGroup
							orientation="horizontal"
							persistKey={`arcane.compose.resizable:${project.id}:unified`}
							onLayoutChange={persistPrefs}
							class="flex min-h-0 flex-1"
						>
							<Resizable.Pane
								id="file-tree"
								minSize={minTreePaneWidth}
								defaultSize={240}
								collapsible
								class="flex min-h-0 flex-col"
							>
								<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
									<Card.Header icon={FileTextIcon} class="shrink-0 items-center">
										<Card.Title>
											<h2 class="truncate">{m.project_files()}</h2>
										</Card.Title>
									</Card.Header>
									<Card.Content class="min-h-0 flex-1 overflow-auto p-2">
										<TreeView.Root class="min-w-max p-2 whitespace-nowrap">
											<TreeView.Folder name={m.project_main_files()} open>
												<ContextMenu.Root>
													<ContextMenu.Trigger class="w-full">
														<TreeView.File
															name="compose.yaml"
															onclick={() => selectFile('compose')}
															class={isFileInSplitView('compose') ? 'bg-accent' : ''}
														>
															{#snippet icon()}
																<FileTextIcon class="size-4 text-blue-500" />
															{/snippet}
														</TreeView.File>
													</ContextMenu.Trigger>
													<ContextMenu.Content class="min-w-[160px]">
														<ContextMenu.Item onclick={() => selectFile('compose')}>
															{m.common_open()}
														</ContextMenu.Item>
														{#if leftPaneFile !== 'compose'}
															<ContextMenu.Item onclick={() => addToSplitView('compose')}>{m.project_open_in_split_view()}</ContextMenu.Item>
														{/if}
														{#if rightPaneFile === 'compose'}
															<ContextMenu.Item onclick={removeFromSplitView}>{m.project_close_split_pane()}</ContextMenu.Item>
														{/if}
													</ContextMenu.Content>
												</ContextMenu.Root>
												<ContextMenu.Root>
													<ContextMenu.Trigger class="w-full">
														<TreeView.File
															name=".env"
															onclick={() => selectFile('env')}
															class={isFileInSplitView('env') ? 'bg-accent' : ''}
														>
															{#snippet icon()}
																<FileTextIcon class="size-4 text-green-500" />
															{/snippet}
														</TreeView.File>
													</ContextMenu.Trigger>
													<ContextMenu.Content class="min-w-[160px]">
														<ContextMenu.Item onclick={() => selectFile('env')}>
															{m.common_open()}
														</ContextMenu.Item>
														{#if leftPaneFile !== 'env'}
															<ContextMenu.Item onclick={() => addToSplitView('env')}>{m.project_open_in_split_view()}</ContextMenu.Item>
														{/if}
														{#if rightPaneFile === 'env'}
															<ContextMenu.Item onclick={removeFromSplitView}>{m.project_close_split_pane()}</ContextMenu.Item>
														{/if}
													</ContextMenu.Content>
												</ContextMenu.Root>
											</TreeView.Folder>

											{#if project?.includeFiles && project.includeFiles.length > 0}
												<TreeView.Folder name={m.project_includes()}>
													{#each project.includeFiles as includeFile (includeFile.path)}
														<ContextMenu.Root>
															<ContextMenu.Trigger class="w-full">
																<TreeView.File
																	name={includeFile.path}
																	onclick={() => selectFile(includeFile.path)}
																	class={isFileInSplitView(includeFile.path) ? 'bg-accent' : ''}
																>
																	{#snippet icon()}
																		<FileSymlinkIcon class="size-4 text-amber-500" />
																	{/snippet}
																</TreeView.File>
															</ContextMenu.Trigger>
															<ContextMenu.Content class="min-w-[160px]">
																<ContextMenu.Item onclick={() => selectFile(includeFile.path)}>
																	{m.common_open()}
																</ContextMenu.Item>
																{#if leftPaneFile !== includeFile.path}
																	<ContextMenu.Item onclick={() => addToSplitView(includeFile.path)}>
																		{m.project_open_in_split_view()}
																	</ContextMenu.Item>
																{/if}
																{#if rightPaneFile === includeFile.path}
																	<ContextMenu.Item onclick={removeFromSplitView}>{m.project_close_split_pane()}</ContextMenu.Item>
																{/if}
															</ContextMenu.Content>
														</ContextMenu.Root>
													{/each}
												</TreeView.Folder>
											{/if}

											<TreeView.Folder name={m.project_custom_files()}>
												{#if project?.customFiles && project.customFiles.length > 0}
													{#each project.customFiles as customFile (customFile.path)}
														{@const customFileKey = `custom:${customFile.path}`}
														<ContextMenu.Root>
															<ContextMenu.Trigger class="w-full">
																<TreeView.File
																	name={customFile.path}
																	onclick={() => selectFile(customFileKey)}
																	class={isFileInSplitView(customFileKey) ? 'bg-accent' : ''}
																>
																	{#snippet icon()}
																		<FilePenIcon class="size-4 text-purple-500" />
																	{/snippet}
																</TreeView.File>
															</ContextMenu.Trigger>
															<ContextMenu.Content class="min-w-[160px]">
																<ContextMenu.Item onclick={() => selectFile(customFileKey)}>
																	{m.common_open()}
																</ContextMenu.Item>
																{#if leftPaneFile !== customFileKey}
																	<ContextMenu.Item onclick={() => addToSplitView(customFileKey)}>
																		{m.project_open_in_split_view()}
																	</ContextMenu.Item>
																{/if}
																{#if rightPaneFile === customFileKey}
																	<ContextMenu.Item onclick={removeFromSplitView}>{m.project_close_split_pane()}</ContextMenu.Item>
																{/if}
																<ContextMenu.Separator />
																<ContextMenu.Item onclick={() => handleRemoveCustomFile(customFile.path)} variant="destructive">
																	{m.common_remove()}
																</ContextMenu.Item>
															</ContextMenu.Content>
														</ContextMenu.Root>
													{/each}
												{/if}
												{#if isAddingCustomFile}
													<div class="flex items-center gap-2 rounded px-2 py-1">
														<FilePenIcon class="size-4 shrink-0 text-purple-500" />
														<input
															bind:this={newFileInputRef}
															bind:value={newCustomFileName}
															onkeydown={handleNewFileKeydown}
															onblur={cancelAddingCustomFile}
															placeholder={m.project_custom_file_path_placeholder()}
															class="text-foreground placeholder:text-muted-foreground border-b-primary h-5 w-full min-w-0 border-b bg-transparent text-xs outline-none"
														/>
													</div>
												{:else}
													<button
														class="hover:bg-accent text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1 text-xs"
														onclick={startAddingCustomFile}
													>
														<AddIcon class="size-4" />
														<span>{m.project_custom_file_add_button()}...</span>
													</button>
												{/if}
											</TreeView.Folder>
										</TreeView.Root>
									</Card.Content>
								</Card.Root>
							</Resizable.Pane>

							<Resizable.Handle index={0} collapsible="before" />

							<Resizable.Pane id="left-editor" minSize={minEditorPaneWidth} defaultSize={560} flex class="flex min-h-0 flex-col">
								{@render fileEditor(leftPaneFile, false)}
							</Resizable.Pane>

							{#if isSplitView && rightPaneFile}
								<Resizable.Handle index={1} collapsible="after" />

								<Resizable.Pane
									id="right-editor"
									minSize={minEditorPaneWidth}
									defaultSize={280}
									collapsible
									class="flex min-h-0 flex-col"
								>
									{@render fileEditor(rightPaneFile, true)}
								</Resizable.Pane>
							{/if}
						</Resizable.PaneGroup>
					</div>

					<div class="flex min-h-0 flex-1 flex-col lg:hidden">
						{@render mobileEditorPane()}
					</div>
				</div>
			</Tabs.Content>

			<Tabs.Content value="logs" class="h-full">
				{#if project.status == 'running'}
					<ProjectsLogsPanel projectId={project.id} bind:autoScroll={autoScrollStackLogs} />
				{:else}
					<div class="text-muted-foreground py-12 text-center">{m.compose_logs_title()} {m.common_disabled()}</div>
				{/if}
			</Tabs.Content>
		{/snippet}
	</TabbedPageLayout>
{:else}
	<div class="flex min-h-screen items-center justify-center">
		<div class="text-center">
			<div class="bg-muted/50 mb-6 inline-flex rounded-full p-6">
				<ProjectsIcon class="text-muted-foreground size-10" />
			</div>
			<h2 class="mb-3 text-2xl font-medium">
				{data.error ? m.common_action_failed() : m.common_not_found_title({ resource: m.project() })}
			</h2>
			<p class="text-muted-foreground mb-8 max-w-md text-center">
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

<script lang="ts">
	import type { Project } from '$lib/types/project.type';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as TreeView from '$lib/components/ui/tree-view/index.js';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import * as Card from '$lib/components/ui/card';
	import * as Alert from '$lib/components/ui/alert/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
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
		UnlinkIcon,
		AlertIcon
	} from '$lib/icons';
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
	import SwitchWithLabel from '$lib/components/form/labeled-switch.svelte';
	import { untrack } from 'svelte';
	import { projectService } from '$lib/services/project-service';
	import { gitOpsSyncService } from '$lib/services/gitops-sync-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
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
	let customFilesPanelStates = $state<Record<string, boolean>>({});
	let showAddCustomFileDialog = $state(false);
	let newCustomFileName = $state('');

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
	let composeOpen = $state(true);
	let envOpen = $state(true);
	let includeFilesPanelStates = $state<Record<string, boolean>>({});
	let selectedFile = $state<'compose' | 'env' | string>('compose');
	let layoutMode = $state<'classic' | 'tree'>('classic');
	let selectedIncludeTab = $state<string | null>(null);

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
		composeOpen: boolean;
		envOpen: boolean;
		autoScroll: boolean;
		layoutMode: 'classic' | 'tree';
	};

	const defaultComposeUIPrefs: ComposeUIPrefs = {
		tab: 'compose',
		composeOpen: true,
		envOpen: true,
		autoScroll: true,
		layoutMode: 'classic'
	};

	let prefs: PersistedState<ComposeUIPrefs> | null = null;

	$effect(() => {
		project = data.project;
	});

	$effect(() => {
		if (!project?.id) return;
		prefs = new PersistedState<ComposeUIPrefs>(`arcane.compose.ui:${project.id}`, defaultComposeUIPrefs, {
			storage: 'session',
			syncTabs: false
		});
		const cur = prefs.current ?? {};
		selectedTab = cur.tab ?? defaultComposeUIPrefs.tab;
		composeOpen = cur.composeOpen ?? defaultComposeUIPrefs.composeOpen;
		envOpen = cur.envOpen ?? defaultComposeUIPrefs.envOpen;
		autoScrollStackLogs = cur.autoScroll ?? defaultComposeUIPrefs.autoScroll;

		// Auto-detect layout mode based on includeFiles
		const hasIncludes = project?.includeFiles && project.includeFiles.length > 0;
		const defaultMode = hasIncludes ? 'tree' : 'classic';
		layoutMode = cur.layoutMode ?? defaultMode;

		// Initialize include file states
		if (project?.includeFiles) {
			const newIncludeState: Record<string, string> = {};
			project.includeFiles.forEach((file) => {
				newIncludeState[file.relativePath] = file.content;
				if (!(file.relativePath in includeFilesPanelStates)) {
					includeFilesPanelStates[file.relativePath] = true;
				}
			});
			includeFilesState = newIncludeState;
			originalIncludeFiles = { ...newIncludeState };
		}

		// Initialize custom file states
		if (project?.customFiles) {
			const newCustomState: Record<string, string> = {};
			project.customFiles.forEach((file) => {
				newCustomState[file.path] = file.content;
				if (!(file.path in customFilesPanelStates)) {
					customFilesPanelStates[file.path] = true;
				}
			});
			customFilesState = newCustomState;
			originalCustomFiles = { ...newCustomState };
		}
	});

	async function handleSaveChanges() {
		if (!project || !hasChanges) return;

		const validated = form.validate();
		if (!validated) return;

		const { name, composeContent, envContent } = validated;

		// First update the main project files
		handleApiResultWithCallbacks({
			result: await tryCatch(projectService.updateProject(projectId, name, composeContent, envContent)),
			message: m.common_save_failed(),
			setLoadingState: (value) => (isLoading.saving = value),
			onSuccess: async (updatedStack: Project) => {
				// Then update any changed include files
				for (const relativePath of Object.keys(includeFilesState)) {
					if (includeFilesState[relativePath] !== originalIncludeFiles[relativePath]) {
						const includeResult = await tryCatch(
							projectService.updateProjectIncludeFile(projectId, relativePath, includeFilesState[relativePath])
						);
						if (includeResult.error) {
							toast.error(includeResult.error.message || m.common_update_failed({ resource: relativePath }));
							return;
						}
					}
				}

				// Then update any changed custom files
				for (const relativePath of Object.keys(customFilesState)) {
					if (customFilesState[relativePath] !== originalCustomFiles[relativePath]) {
						const customResult = await tryCatch(
							projectService.updateProjectCustomFile(projectId, relativePath, customFilesState[relativePath])
						);
						if (customResult.error) {
							toast.error(m.common_update_failed({ resource: relativePath }));
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

	async function handleAddCustomFile() {
		if (!newCustomFileName.trim()) {
			toast.error('Please enter a file name');
			return;
		}

		const relativePath = newCustomFileName.trim();
		const result = await tryCatch(projectService.createProjectCustomFile(projectId, relativePath));
		if (result.error) {
			toast.error(`Failed to add file: ${result.error.message || 'Unknown error'}`);
			return;
		}

		toast.success(`Added ${relativePath}`);
		showAddCustomFileDialog = false;
		newCustomFileName = '';
		await invalidateAll();
	}

	async function handleRemoveCustomFile(filePath: string) {
		const result = await tryCatch(projectService.removeProjectCustomFile(projectId, filePath));
		if (result.error) {
			toast.error(`Failed to remove file: ${result.error.message || 'Unknown error'}`);
			return;
		}

		// Remove from local state
		delete customFilesState[filePath];
		delete originalCustomFiles[filePath];
		delete customFilesPanelStates[filePath];

		// Reset selected file if it was the removed one
		if (selectedFile === `custom:${filePath}`) {
			selectedFile = 'compose';
		}

		toast.success(`Removed ${filePath}`);
		await invalidateAll();
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
			composeOpen,
			envOpen,
			autoScroll: autoScrollStackLogs,
			layoutMode
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
					<div class="mb-4 shrink-0">
						<SwitchWithLabel
							id="layout-mode-toggle"
							checked={layoutMode === 'tree'}
							label={layoutMode === 'tree' ? m.tree_view() : m.classic()}
							description={m.project_view_description()}
							onCheckedChange={(checked) => {
								layoutMode = checked ? 'tree' : 'classic';
								if (checked) {
									selectedFile = 'compose';
									selectedIncludeTab = null;
								}
								persistPrefs();
							}}
						/>
					</div>

					<div class="min-h-0 flex-1">
						{#if layoutMode === 'tree'}
							<div class="flex h-full min-h-0 flex-col gap-4 lg:flex-row">
								<Card.Root class="flex min-h-0 w-full flex-1 flex-col overflow-hidden lg:w-fit lg:max-w-xs lg:min-w-48">
									<Card.Header icon={FileTextIcon} class="shrink-0 items-center">
										<Card.Title>
											<h2>{m.project_files()}</h2>
										</Card.Title>
									</Card.Header>
									<Card.Content class="min-h-0 flex-1 overflow-y-auto p-2">
										<TreeView.Root class="p-2">
											<TreeView.File
												name="compose.yaml"
												onclick={() => (selectedFile = 'compose')}
												class={selectedFile === 'compose' ? 'bg-accent' : ''}
											>
												{#snippet icon()}
													<FileTextIcon class="size-4 text-blue-500" />
												{/snippet}
											</TreeView.File>

											<TreeView.File
												name=".env"
												onclick={() => (selectedFile = 'env')}
												class={selectedFile === 'env' ? 'bg-accent' : ''}
											>
												{#snippet icon()}
													<FileTextIcon class="size-4 text-green-500" />
												{/snippet}
											</TreeView.File>

											{#if project?.includeFiles && project.includeFiles.length > 0}
												<TreeView.Folder name={m.project_includes()}>
													{#each project.includeFiles as includeFile (includeFile.relativePath)}
														<TreeView.File
															name={includeFile.relativePath}
															onclick={() => (selectedFile = includeFile.relativePath)}
															class={selectedFile === includeFile.relativePath ? 'bg-accent' : ''}
														>
															{#snippet icon()}
																<FileSymlinkIcon class="size-4 text-amber-500" />
															{/snippet}
														</TreeView.File>
													{/each}
												</TreeView.Folder>
											{/if}

											<TreeView.Folder name="Custom Files">
												{#if project?.customFiles && project.customFiles.length > 0}
													{#each project.customFiles as customFile (customFile.path)}
														<TreeView.File
															name={customFile.path}
															onclick={() => (selectedFile = `custom:${customFile.path}`)}
															class={selectedFile === `custom:${customFile.path}` ? 'bg-accent' : ''}
														>
															{#snippet icon()}
																<FilePenIcon class="size-4 text-purple-500" />
															{/snippet}
														</TreeView.File>
													{/each}
												{/if}
												<button
													class="hover:bg-accent text-muted-foreground hover:text-foreground flex w-full cursor-pointer items-center gap-2 rounded px-2 py-1 text-xs"
													onclick={() => (showAddCustomFileDialog = true)}
												>
													<AddIcon class="size-4" />
													<span>Add file...</span>
												</button>
											</TreeView.Folder>
										</TreeView.Root>
									</Card.Content>
								</Card.Root>

								<div class="flex h-full min-h-0 flex-1 flex-col">
									{#if selectedFile === 'compose'}
										<CodePanel
											bind:open={composeOpen}
											title="compose.yaml"
											language="yaml"
											bind:value={$inputs.composeContent.value}
											error={$inputs.composeContent.error ?? undefined}
											readOnly={!canEditCompose}
										/>
									{:else if selectedFile === 'env'}
										<CodePanel
											bind:open={envOpen}
											title=".env"
											language="env"
											bind:value={$inputs.envContent.value}
											error={$inputs.envContent.error ?? undefined}
										/>
									{:else if selectedFile.startsWith('custom:')}
										{@const customPath = selectedFile.replace('custom:', '')}
										{@const customFile = project?.customFiles?.find((f) => f.path === customPath)}
										{#if customFile}
											<div class="flex h-full min-h-0 flex-col">
												<div class="mb-2 flex items-center justify-between">
													<div class="flex items-center gap-2">
														<FilePenIcon class="size-4 text-purple-500" />
														<span class="text-sm font-medium">{customFile.path}</span>
													</div>
													<Button
														variant="ghost"
														size="sm"
														class="text-muted-foreground hover:text-foreground"
														onclick={() => handleRemoveCustomFile(customFile.path)}
													>
														<UnlinkIcon class="size-4" />
													</Button>
												</div>
												<CodePanel
													bind:open={customFilesPanelStates[customFile.path]}
													title={customFile.path}
													language="yaml"
													bind:value={customFilesState[customFile.path]}
												/>
											</div>
										{/if}
									{:else}
										{@const includeFile = project?.includeFiles?.find((f) => f.relativePath === selectedFile)}
										{#if includeFile}
											<CodePanel
												bind:open={includeFilesPanelStates[includeFile.relativePath]}
												title={includeFile.relativePath}
												language="yaml"
												bind:value={includeFilesState[includeFile.relativePath]}
											/>
										{/if}
									{/if}
								</div>
							</div>
						{:else}
							<div class="flex h-full min-h-0 flex-col gap-4">
								{#if (project?.includeFiles && project.includeFiles.length > 0) || (project?.customFiles && project.customFiles.length > 0)}
									<div class="border-border bg-card rounded-lg border">
										<div class="border-border scrollbar-hide flex gap-2 overflow-x-auto border-b p-2">
											{#if project?.includeFiles}
												{#each project.includeFiles as includeFile (includeFile.relativePath)}
													<ArcaneButton
														action="base"
														tone={selectedIncludeTab === includeFile.relativePath ? 'outline-primary' : 'ghost'}
														size="sm"
														class="flex-shrink-0"
														onclick={() => {
															selectedIncludeTab =
																selectedIncludeTab === includeFile.relativePath ? null : includeFile.relativePath;
														}}
														icon={FileSymlinkIcon}
														customLabel={includeFile.relativePath}
													/>
												{/each}
											{/if}
											{#if project?.customFiles}
												{#each project.customFiles as customFile (customFile.path)}
													<ArcaneButton
														action="base"
														tone={selectedIncludeTab === `custom:${customFile.path}` ? 'outline-primary' : 'ghost'}
														size="sm"
														class="flex-shrink-0"
														onclick={() => {
															selectedIncludeTab =
																selectedIncludeTab === `custom:${customFile.path}` ? null : `custom:${customFile.path}`;
														}}
														icon={FilePenIcon}
														customLabel={customFile.path}
													/>
												{/each}
											{/if}
											<ArcaneButton
												action="base"
												tone="ghost"
												size="sm"
												class="text-muted-foreground flex-shrink-0"
												onclick={() => (showAddCustomFileDialog = true)}
												icon={AddIcon}
												customLabel="Add file"
											/>
										</div>
									</div>
								{/if}

								{#if selectedIncludeTab}
									{#if selectedIncludeTab.startsWith('custom:')}
										{@const customPath = selectedIncludeTab.replace('custom:', '')}
										{@const customFile = project?.customFiles?.find((f) => f.path === customPath)}
										{#if customFile}
											<div class="flex min-h-0 flex-1 flex-col">
												<div class="mb-2 flex items-center justify-between">
													<div class="flex items-center gap-2">
														<FilePenIcon class="size-4 text-purple-500" />
														<span class="text-sm font-medium">{customFile.path}</span>
													</div>
													<Button
														variant="ghost"
														size="sm"
														class="text-muted-foreground hover:text-foreground"
														onclick={() => handleRemoveCustomFile(customFile.path)}
													>
														<UnlinkIcon class="size-4" />
													</Button>
												</div>
												<CodePanel
													bind:open={customFilesPanelStates[customFile.path]}
													title={customFile.path}
													language="yaml"
													bind:value={customFilesState[customFile.path]}
												/>
											</div>
										{/if}
									{:else}
										{@const includeFile = project?.includeFiles?.find((f) => f.relativePath === selectedIncludeTab)}
										{#if includeFile}
											<CodePanel
												bind:open={includeFilesPanelStates[includeFile.relativePath]}
												title={includeFile.relativePath}
												language="yaml"
												bind:value={includeFilesState[includeFile.relativePath]}
											/>
										{/if}
									{/if}
								{:else}
									<div class="flex min-h-0 flex-1 flex-col gap-4 lg:grid lg:grid-cols-5 lg:grid-rows-1">
										<div class="flex min-h-0 flex-1 flex-col lg:col-span-3">
											<CodePanel
												bind:open={composeOpen}
												title="compose.yaml"
												language="yaml"
												bind:value={$inputs.composeContent.value}
												error={$inputs.composeContent.error ?? undefined}
												readOnly={!canEditCompose}
											/>
										</div>

										<div class="flex min-h-0 flex-1 flex-col lg:col-span-2">
											<CodePanel
												bind:open={envOpen}
												title=".env"
												language="env"
												bind:value={$inputs.envContent.value}
												error={$inputs.envContent.error ?? undefined}
											/>
										</div>
									</div>
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

<Dialog.Root bind:open={showAddCustomFileDialog}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Add Custom File</Dialog.Title>
			<Dialog.Description>
				Add an existing file or create a new empty file. If the file already exists, its content will be preserved. Use relative
				paths like "config/settings.yaml" or absolute paths if configured.
			</Dialog.Description>
		</Dialog.Header>
		<div class="grid gap-4 py-4">
			<div class="grid gap-2">
				<label for="custom-file-name" class="text-sm font-medium">File Path</label>
				<Input
					id="custom-file-name"
					placeholder="config/my-file.yaml"
					bind:value={newCustomFileName}
					onkeydown={(e) => {
						if (e.key === 'Enter') {
							handleAddCustomFile();
						}
					}}
				/>
			</div>
		</div>
		<Dialog.Footer>
			<Button variant="outline" onclick={() => (showAddCustomFileDialog = false)}>Cancel</Button>
			<Button onclick={handleAddCustomFile}>Create File</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

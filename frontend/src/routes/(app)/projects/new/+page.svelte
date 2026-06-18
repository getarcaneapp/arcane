<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import {
		ArrowLeftIcon,
		ArrowsUpDownIcon,
		TerminalIcon,
		TemplateIcon,
		AddIcon,
		GitBranchIcon,
		FileTextIcon,
		SearchIcon
	} from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto, invalidateAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { preventDefault, createForm } from '$lib/utils/settings';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import { m } from '$lib/paraglide/messages';
	import { projectService } from '$lib/services/project-service.js';
	import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { ArrowDownIcon as ChevronDown } from '$lib/icons';
	import CodePanel from '../components/CodePanel.svelte';
	import EditableName from '../components/EditableName.svelte';
	import ProjectFileTreePanel from '../components/ProjectFileTreePanel.svelte';
	import EditorTabStrip from '../components/EditorTabStrip.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { ComposeEditorSplit } from '$lib/components/compose';
	import ResizableSplit from '$lib/components/resizable-split.svelte';
	import { Switch } from '$lib/components/ui/switch';
	import DockerRunConverterDialog from '$lib/components/compose/docker-run-converter-dialog.svelte';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import type { ProjectFileDraft } from '$lib/types/project-files';
	import {
		isProjectFileSelectionUnder,
		planProjectFileCreate,
		planProjectFileMove,
		planProjectFileRename,
		projectFileBasename,
		projectFileLanguage,
		projectFilePathMatches,
		remapProjectFilePath,
		remapProjectFileRecord,
		remapSelectedProjectFileKey,
		removeProjectFileRecord,
		type ManagedProjectFileEntry
	} from '../components/project-file-tree-utils';
	import {
		createComposeEditorSchema,
		createComposeTemplateDialogFlow,
		dropdownContentClass,
		dropdownItemClass,
		extractComposeYamlName,
		submitComposeResourceForm,
		templateBtnClass,
		templateNameSlug
	} from '$lib/utils/compose-flow';
	import {
		getTemplateEditorValidationState,
		hasTemplateEditorErrors,
		validateTemplateEditorForm
	} from '$lib/utils/template-editor';

	let { data } = $props();

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canCreateProject = $derived(hasPermission('projects:create', currentEnvId));

	let ui = $state({
		saving: false,
		converting: false,
		creatingTemplate: false,
		showTemplateDialog: false,
		showConverterDialog: false,
		isLoadingTemplateContent: false
	});

	const formSchema = createComposeEditorSchema(m.compose_project_name_required());

	// Initial form values intentionally come from the page load data once.
	// svelte-ignore state_referenced_locally
	const formData = {
		name: data.selectedTemplate ? templateNameSlug(data.selectedTemplate.name) : '',
		composeContent: data.defaultTemplate || '',
		envContent: data.envTemplate || ''
	};

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, formData);

	let composeOpen = $state(true);
	let envOpen = $state(true);
	let layoutMode = $state<'classic' | 'tree'>('classic');
	let selectedProjectFile = $state<'compose' | 'env' | string>('compose');
	let treePaneWidth = $state(420);
	const minTreePaneWidth = 200;
	const maxTreePaneWidth = 480;
	const minEditorPaneWidth = 360;
	let newProjectFiles = $state<ProjectFileDraft[]>([]);
	let newProjectFileContents = $state<Record<string, string>>({});
	let newProjectFileHasErrors = $state<Record<string, boolean>>({});
	let newProjectFileValidationReady = $state<Record<string, boolean>>({});
	let validation = $state({
		composeHasErrors: false,
		envHasErrors: false,
		composeValidationReady: false,
		envValidationReady: false
	});

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));
	const newProjectFileEntries = $derived.by<ManagedProjectFileEntry[]>(() =>
		newProjectFiles.map((file) => ({
			path: file.relativePath,
			relativePath: file.relativePath,
			name: projectFileBasename(file.relativePath),
			isDirectory: !!file.isDirectory,
			size: file.isDirectory ? 0 : (newProjectFileContents[file.relativePath]?.length ?? 0),
			content: file.isDirectory ? undefined : (newProjectFileContents[file.relativePath] ?? ''),
			pending: true
		}))
	);
	const newProjectFilePaths = $derived.by(() => new Set(newProjectFileEntries.map((file) => file.relativePath)));
	let openProjectTabs = $state<string[]>(['compose']);
	let treeOutlineOpen = $state(false);
	let treeDiffOpen = $state(false);
	let treeCommandPaletteOpen = $state(false);
	const openTabs = $derived.by(() => {
		const valid = openProjectTabs.filter((key) => {
			if (key === 'compose' || key === 'env') return true;
			if (!key.startsWith('file:')) return false;
			const entry = newProjectFileEntries.find((file) => file.relativePath === key.slice(5));
			return !!entry && !entry.isDirectory;
		});
		return valid.length > 0 ? valid : ['compose'];
	});
	const activeProjectTab = $derived(openTabs.includes(selectedProjectFile) ? selectedProjectFile : (openTabs[0] ?? 'compose'));
	const projectTabs = $derived(
		openTabs.map((key) => ({
			key,
			label: key === 'compose' ? 'compose.yaml' : key === 'env' ? '.env' : projectFileBasename(key.slice(5)),
			title: key === 'compose' ? 'compose.yaml' : key === 'env' ? '.env' : key.slice(5),
			iconClass: key === 'compose' ? 'text-blue-500' : key === 'env' ? 'text-green-500' : 'text-muted-foreground',
			pending: false
		}))
	);

	function isNewProjectDirectoryKey(key: string): boolean {
		if (!key.startsWith('file:')) return false;
		return newProjectFileEntries.find((file) => file.relativePath === key.slice(5))?.isDirectory === true;
	}

	function openProjectFileTab(key: string) {
		if (!isNewProjectDirectoryKey(key) && !openProjectTabs.includes(key)) {
			openProjectTabs = [...openProjectTabs, key];
		}
		selectedProjectFile = key;
	}

	function closeProjectFileTab(key: string) {
		const index = openTabs.indexOf(key);
		const remaining = openTabs.filter((tab) => tab !== key);
		openProjectTabs = openProjectTabs.filter((tab) => tab !== key);
		if (selectedProjectFile === key) {
			selectedProjectFile = remaining[Math.min(Math.max(index - 1, 0), remaining.length - 1)] ?? 'compose';
		}
	}
	const validationState = $derived(
		getTemplateEditorValidationState(
			validation.composeValidationReady,
			validation.envValidationReady,
			validation.composeHasErrors,
			validation.envHasErrors
		)
	);
	let hasEditorErrors = $derived(hasTemplateEditorErrors(validationState));
	const codeEditorContext = $derived({
		envContent: $inputs.envContent.value,
		composeContents: [$inputs.composeContent.value].filter((value) => value.length > 0),
		globalVariables: globalVariableMap
	});

	let nameInputRef = $state<HTMLInputElement | null>(null);

	const composeYamlName = $derived(extractComposeYamlName($inputs.composeContent.value));
	// The compose file's top-level `name:` is authoritative; surface it as the
	// effective name without writing to form state reactively.
	const effectiveName = $derived(composeYamlName ?? $inputs.name.value);

	async function handleSubmit() {
		await handleCreateProject();
	}

	async function handleCreateProject() {
		// Sync the authoritative compose name into form state at submit time so
		// validation and the create payload use it (event-time write, not an effect).
		if (composeYamlName) form.setValue('name', composeYamlName);
		await submitComposeResourceForm({
			validate: () => validateTemplateEditorForm(validationState, form.validate),
			setLoading: (value) => (ui.saving = value),
			submit: ({ name, composeContent, envContent }) =>
				projectService.createProject(name, composeContent, envContent, buildNewProjectFilePayload()),
			failureMessage: (name) => m.common_create_failed({ resource: `${m.resource_project()} "${name}"` }),
			onSuccess: async (project, { name }) => {
				toast.success(
					m.common_create_success({ resource: `${m.resource_project()} "${name}"` }),
					activityToastOptions(extractActivityId(project))
				);
				goto(`/projects/${project.id}`, { invalidateAll: true });
			}
		});
	}

	const { composeHandlers, handleCreateTemplate } = createComposeTemplateDialogFlow({
		getInputs: () => $inputs,
		setInputValue: (key, value) => form.setValue(key, value),
		closeTemplateDialog: () => (ui.showTemplateDialog = false),
		validate: form.validate,
		setLoading: (value) => (ui.creatingTemplate = value),
		hasEditorErrors: () => hasEditorErrors
	});

	function ensureNewProjectFileUiState(relativePath: string) {
		if (newProjectFileHasErrors[relativePath] === undefined) {
			newProjectFileHasErrors = {
				...newProjectFileHasErrors,
				[relativePath]: false
			};
		}
		if (newProjectFileValidationReady[relativePath] === undefined) {
			newProjectFileValidationReady = {
				...newProjectFileValidationReady,
				[relativePath]: true
			};
		}
	}

	function createNewProjectFile(parentPath: string, name: string, content = '') {
		const relativePath = planProjectFileCreate(newProjectFilePaths, parentPath, name);
		if (!relativePath) return;
		newProjectFiles = [...newProjectFiles, { relativePath, isDirectory: false }];
		newProjectFileContents = { ...newProjectFileContents, [relativePath]: content };
		ensureNewProjectFileUiState(relativePath);
		openProjectFileTab(`file:${relativePath}`);
	}

	function createNewProjectFolder(parentPath: string, name: string) {
		const relativePath = planProjectFileCreate(newProjectFilePaths, parentPath, name);
		if (!relativePath) return;
		newProjectFiles = [...newProjectFiles, { relativePath, isDirectory: true }];
		selectedProjectFile = `file:${relativePath}`;
	}

	function applyNewProjectFilePathChange(oldPath: string, newPath: string) {
		newProjectFiles = newProjectFiles.map((file) => ({
			...file,
			relativePath: remapProjectFilePath(file.relativePath, oldPath, newPath)
		}));
		newProjectFileContents = remapProjectFileRecord(newProjectFileContents, oldPath, newPath);
		newProjectFileHasErrors = remapProjectFileRecord(newProjectFileHasErrors, oldPath, newPath);
		newProjectFileValidationReady = remapProjectFileRecord(newProjectFileValidationReady, oldPath, newPath);
		openProjectTabs = openProjectTabs.map((tab) => remapSelectedProjectFileKey(tab, oldPath, newPath) ?? tab);
		const remappedSelection = remapSelectedProjectFileKey(selectedProjectFile, oldPath, newPath);
		if (remappedSelection) {
			selectedProjectFile = remappedSelection;
		}
	}

	function renameNewProjectFile(relativePath: string, newName: string) {
		const plan = planProjectFileRename(newProjectFilePaths, relativePath, newName);
		if (!plan) return;
		applyNewProjectFilePathChange(relativePath, plan.newPath);
	}

	function moveNewProjectFile(relativePath: string, newParentPath: string) {
		const entry = newProjectFileEntries.find((file) => file.relativePath === relativePath);
		const newPath = planProjectFileMove(entry, newProjectFilePaths, relativePath, newParentPath);
		if (!newPath) return;
		applyNewProjectFilePathChange(relativePath, newPath);
	}

	function deleteNewProjectFile(relativePath: string) {
		newProjectFiles = newProjectFiles.filter((file) => !projectFilePathMatches(file.relativePath, relativePath));
		newProjectFileContents = removeProjectFileRecord(newProjectFileContents, relativePath);
		newProjectFileHasErrors = removeProjectFileRecord(newProjectFileHasErrors, relativePath);
		newProjectFileValidationReady = removeProjectFileRecord(newProjectFileValidationReady, relativePath);
		openProjectTabs = openProjectTabs.filter((tab) => !isProjectFileSelectionUnder(tab, relativePath));
		if (isProjectFileSelectionUnder(selectedProjectFile, relativePath)) {
			selectedProjectFile = openTabs[0] ?? 'compose';
		}
	}

	function buildNewProjectFilePayload(): ProjectFileDraft[] {
		return newProjectFiles.map((file) => ({
			relativePath: file.relativePath,
			isDirectory: !!file.isDirectory,
			content: file.isDirectory ? undefined : (newProjectFileContents[file.relativePath] ?? '')
		}));
	}
</script>

<div class="bg-background flex h-full min-h-0 flex-col">
	<div class="sticky top-0 mb-2 border-b">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					href="/projects"
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
				/>
				<div class="bg-border hidden h-4 w-px sm:block"></div>
				<div class="hidden items-center gap-3 sm:flex">
					<EditableName
						bind:value={$inputs.name.value}
						displayValue={effectiveName}
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.compose_project_name_placeholder?.() || 'Enter project name...'}
						canEdit={!ui.saving && !ui.isLoadingTemplateContent && !composeYamlName}
						disabledMessage={composeYamlName ? m.compose_project_name_defined_in_yaml() : undefined}
						class="hidden sm:block"
					/>
				</div>
			</div>

			<div class="flex items-center gap-2">
				<ButtonGroup.Root>
					<ArcaneTooltip.Root
						open={!effectiveName && !ui.saving && !ui.converting && !ui.isLoadingTemplateContent ? undefined : false}
					>
						<ArcaneTooltip.Trigger>
							<span>
								{#if !hasEditorErrors && canCreateProject}
									<ArcaneButton
										action="create"
										tone="ghost"
										disabled={!effectiveName ||
											!$inputs.composeContent.value ||
											hasEditorErrors ||
											ui.saving ||
											ui.converting ||
											ui.isLoadingTemplateContent}
										onclick={() => handleSubmit()}
										class={`${templateBtnClass} gap-2 rounded-r-none`}
										loading={ui.saving}
										customLabel={m.compose_create_project()}
										loadingLabel={m.common_action_creating()}
									/>
								{/if}
							</span>
						</ArcaneTooltip.Trigger>
						<ArcaneTooltip.Content class="arcane-tooltip-content max-w-[280px]">
							{#if effectiveName === ''}
								<p class="mb-1 text-sm font-medium">{m.compose_project_name_tooltip_title()}</p>
								<p class="text-muted-foreground text-xs">
									{m.compose_project_name_tooltip_description()}
								</p>
								<p class="bg-muted mt-1.5 inline-block rounded px-1.5 py-0.5 font-mono text-xs">
									{m.compose_project_name_tooltip_example()}
								</p>
							{/if}
						</ArcaneTooltip.Content>
					</ArcaneTooltip.Root>

					<DropdownMenu.Root>
						<DropdownMenu.Trigger>
							{#snippet child({ props })}
								<ArcaneButton
									{...props}
									action="base"
									tone="ghost"
									class={`${templateBtnClass} -ml-px rounded-l-none px-2`}
									icon={ChevronDown}
								/>
							{/snippet}
						</DropdownMenu.Trigger>
						<DropdownMenu.Content align="end" class={dropdownContentClass}>
							<DropdownMenu.Group>
								<DropdownMenu.Item
									class={dropdownItemClass}
									disabled={ui.saving || ui.converting || ui.isLoadingTemplateContent}
									onclick={() => (ui.showTemplateDialog = true)}
								>
									<TemplateIcon class="size-4" />
									{m.common_use_template()}
								</DropdownMenu.Item>
								<DropdownMenu.Item class={dropdownItemClass} onclick={() => (ui.showConverterDialog = true)}>
									<TerminalIcon class="size-4" />
									{m.compose_convert_from_docker_run()}
								</DropdownMenu.Item>
								<DropdownMenu.Item
									class={dropdownItemClass}
									onclick={async () =>
										goto(`/environments/${await environmentStore.getCurrentEnvironmentId()}/gitops?action=create`)}
								>
									<GitBranchIcon class="size-4" />
									{m.git_from_git_repo()}
								</DropdownMenu.Item>
								<IfPermitted perm="templates:create">
									<DropdownMenu.Separator />
									<DropdownMenu.Item
										class={dropdownItemClass}
										disabled={!$inputs.name.value ||
											!$inputs.composeContent.value ||
											hasEditorErrors ||
											ui.saving ||
											ui.converting ||
											ui.creatingTemplate ||
											ui.isLoadingTemplateContent}
										onclick={handleCreateTemplate}
									>
										{#if ui.creatingTemplate}
											<Spinner class="size-4" />
										{:else}
											<AddIcon class="size-4" />
										{/if}
										{m.templates_create_template()}
									</DropdownMenu.Item>
								</IfPermitted>
							</DropdownMenu.Group>
						</DropdownMenu.Content>
					</DropdownMenu.Root>
				</ButtonGroup.Root>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto h-full w-full max-w-full min-w-0">
			<div class="flex h-full min-h-0 flex-col gap-4">
				<div class="block flex-shrink-0 py-4 sm:hidden">
					<EditableName
						bind:value={$inputs.name.value}
						displayValue={effectiveName}
						bind:ref={nameInputRef}
						variant="block"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.compose_project_name_placeholder()}
						canEdit={!ui.saving && !ui.isLoadingTemplateContent && !composeYamlName}
						disabledMessage={composeYamlName ? m.compose_project_name_defined_in_yaml() : undefined}
					/>
				</div>

				<div class="flex shrink-0 items-center justify-end gap-2">
					<label
						for="new-project-layout-mode-toggle"
						class="text-muted-foreground cursor-pointer text-xs"
						title={m.project_view_description()}
					>
						{m.workspace()}
					</label>
					<Switch
						id="new-project-layout-mode-toggle"
						checked={layoutMode === 'tree'}
						aria-label={m.project_view_description()}
						onCheckedChange={(checked) => {
							layoutMode = checked ? 'tree' : 'classic';
							openProjectFileTab('compose');
						}}
					/>
				</div>

				{#if layoutMode === 'tree'}
					<div class="bg-card border-border flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
						<ResizableSplit
							class="min-h-0 flex-1"
							variant="flush"
							firstClass="bg-muted/20 border-border flex min-h-0 flex-col border-b lg:border-r lg:border-b-0"
							secondClass="flex min-h-0 flex-col"
							bind:size={treePaneWidth}
							minSize={minTreePaneWidth}
							maxSize={maxTreePaneWidth}
							minSecondSize={minEditorPaneWidth}
							defaultRatio={0.22}
							stackBelow={1024}
							ariaLabel={m.compose_editor_resize_files_panel()}
							persistKey="arcane.compose.split:new-project:tree"
						>
							{#snippet first()}
								<ProjectFileTreePanel
									composeFileName="compose.yaml"
									entries={newProjectFileEntries}
									selectedFile={selectedProjectFile}
									disabled={ui.saving || ui.isLoadingTemplateContent}
									onSelect={openProjectFileTab}
									onCreateFile={createNewProjectFile}
									onCreateFolder={createNewProjectFolder}
									onRename={renameNewProjectFile}
									onMove={moveNewProjectFile}
									onDelete={deleteNewProjectFile}
								/>
							{/snippet}

							{#snippet second()}
								<div class="flex h-full min-h-0 flex-1 flex-col">
									<EditorTabStrip
										tabs={projectTabs}
										activeKey={activeProjectTab}
										onSelect={openProjectFileTab}
										onClose={closeProjectFileTab}
									>
										{#snippet actions()}
											<ArcaneButton
												action="base"
												tone={treeOutlineOpen ? 'outline-primary' : 'ghost'}
												size="icon"
												class="size-6"
												showLabel={false}
												icon={FileTextIcon}
												customLabel={m.compose_editor_toggle_outline()}
												onclick={() => (treeOutlineOpen = !treeOutlineOpen)}
											/>
											<ArcaneButton
												action="base"
												tone={treeDiffOpen ? 'outline-primary' : 'ghost'}
												size="icon"
												class="size-6"
												showLabel={false}
												icon={ArrowsUpDownIcon}
												customLabel={m.compose_editor_toggle_diff()}
												onclick={() => (treeDiffOpen = !treeDiffOpen)}
											/>
											<ArcaneButton
												action="base"
												tone="ghost"
												size="icon"
												class="size-6"
												showLabel={false}
												icon={SearchIcon}
												customLabel={m.compose_editor_command_palette()}
												onclick={() => (treeCommandPaletteOpen = true)}
											/>
										{/snippet}
									</EditorTabStrip>
									<div class="flex min-h-0 flex-1 flex-col">
										{#key activeProjectTab}
											{#if activeProjectTab === 'compose'}
												<CodePanel
													variant="plain"
													bind:open={composeOpen}
													title={m.compose_compose_file_title()}
													language="yaml"
													validationMode="compose"
													bind:value={$inputs.composeContent.value}
													error={$inputs.composeContent.error ?? undefined}
													bind:hasErrors={validation.composeHasErrors}
													bind:validationReady={validation.composeValidationReady}
													fileId="projects:new:compose"
													editorContext={codeEditorContext}
													bind:outlineOpen={treeOutlineOpen}
													bind:diffOpen={treeDiffOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{:else if activeProjectTab === 'env'}
												<CodePanel
													variant="plain"
													bind:open={envOpen}
													title={m.compose_env_title()}
													language="env"
													validationMode="env"
													bind:value={$inputs.envContent.value}
													error={$inputs.envContent.error ?? undefined}
													bind:hasErrors={validation.envHasErrors}
													bind:validationReady={validation.envValidationReady}
													fileId="projects:new:env"
													editorContext={codeEditorContext}
													bind:outlineOpen={treeOutlineOpen}
													bind:diffOpen={treeDiffOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{:else if activeProjectTab.startsWith('file:')}
												{@const relativePath = activeProjectTab.slice(5)}
												<CodePanel
													variant="plain"
													open={true}
													title={relativePath}
													language={projectFileLanguage(relativePath)}
													validationMode="none"
													bind:value={newProjectFileContents[relativePath]}
													bind:hasErrors={newProjectFileHasErrors[relativePath]}
													bind:validationReady={newProjectFileValidationReady[relativePath]}
													fileId={`projects:new:file:${relativePath}`}
													originalValue=""
													enableDiff={true}
													editorContext={codeEditorContext}
													bind:outlineOpen={treeOutlineOpen}
													bind:diffOpen={treeDiffOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{/if}
										{/key}
									</div>
								</div>
							{/snippet}
						</ResizableSplit>
					</div>
				{:else}
					<ComposeEditorSplit onsubmit={preventDefault(handleSubmit)}>
						{#snippet compose()}
							<CodePanel
								bind:open={composeOpen}
								title={m.compose_compose_file_title()}
								language="yaml"
								validationMode="compose"
								bind:value={$inputs.composeContent.value}
								error={$inputs.composeContent.error ?? undefined}
								bind:hasErrors={validation.composeHasErrors}
								bind:validationReady={validation.composeValidationReady}
								fileId="projects:new:compose"
								editorContext={codeEditorContext}
							/>
						{/snippet}

						{#snippet env()}
							<CodePanel
								bind:open={envOpen}
								title={m.compose_env_title()}
								language="env"
								validationMode="env"
								bind:value={$inputs.envContent.value}
								error={$inputs.envContent.error ?? undefined}
								bind:hasErrors={validation.envHasErrors}
								bind:validationReady={validation.envValidationReady}
								fileId="projects:new:env"
								editorContext={codeEditorContext}
							/>
						{/snippet}
					</ComposeEditorSplit>
				{/if}
			</div>
		</div>
	</div>
</div>

<DockerRunConverterDialog
	bind:open={ui.showConverterDialog}
	bind:converting={ui.converting}
	onConverted={composeHandlers.handleDockerRunConverted}
/>

<TemplateSelectionDialog
	bind:open={ui.showTemplateDialog}
	templates={data.composeTemplates || []}
	onSelect={composeHandlers.handleTemplateSelect}
	onDownloadSuccess={invalidateAll}
/>

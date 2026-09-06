<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { ArrowLeftIcon } from '#lib/icons/index.js';
	import { goto, refreshAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { preventDefault, createForm } from '#lib/utils/settings.js';
	import TemplateSelectionDialog from '#lib/components/dialogs/template-selection-dialog.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { projectService } from '#lib/services/project-service.js';
	import ComposeCreateMenu from '#lib/components/compose-create-menu.svelte';
	import ComposeFileEditorPanel from '#lib/components/compose-file-editor-panel.svelte';
	import CodePanel from '#lib/components/code-panel.svelte';
	import EditableName from '../components/EditableName.svelte';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { containerService } from '#lib/services/container-service.js';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import { extractApiErrorMessage, tryCatch } from '#lib/utils/api.js';
	import { ComposeEditorSplit } from '#lib/components/compose/index.js';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import DockerRunConverterDialog from '#lib/components/compose/docker-run-converter-dialog.svelte';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { globalVariablesToMap } from '#lib/utils/template-load.js';
	import type { ProjectTag } from '#lib/types/swarm.js';
	import ProjectTagEditor from '#lib/components/project-tag-editor.svelte';
	import { createQuery } from '@tanstack/svelte-query';
	import { queryKeys } from '#lib/query/query-keys.js';
	import settingsStore from '#lib/stores/config-store.js';
	import {
		planProjectWorkspaceFileCreate,
		planProjectWorkspaceFileRename,
		validateProjectWorkspaceFileName
	} from '../components/project-workspace-utils';
	import {
		planWorkspaceFileMove,
		workspaceFileBasename,
		workspaceFileLanguage,
		workspaceReadOnlyMessage
	} from '#lib/utils/workspace-files.js';
	import { WorkspaceDraftState } from '#lib/components/workspace-editor/workspace-draft-state.svelte.js';
	import {
		composeTreeSplitProps,
		createComposeEditorSchema,
		createComposeTemplateDialogFlow,
		extractComposeYamlName,
		submitComposeResourceForm,
		templateNameSlug
	} from '#lib/utils/compose-flow.js';
	import {
		getTemplateEditorValidationState,
		hasTemplateEditorErrors,
		validateTemplateEditorForm
	} from '#lib/utils/template-editor.js';

	let { data } = $props();

	const currentEnvId = $derived(environmentStore.selected?.id || '0');
	const canCreateProject = $derived(hasPermission('projects:create', currentEnvId));
	const canDeleteContainers = $derived(hasPermission('containers:delete', currentEnvId));
	const sourceContainerIds = $derived(data.sourceContainerIds ?? []);
	const projectWorkspaceMaxFileSizeMb = $derived($settingsStore?.projectWorkspaceMaxFileSizeMb ?? 10);

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
		name: data.selectedTemplate
			? templateNameSlug(data.selectedTemplate.name)
			: data.sourceContainerName
				? templateNameSlug(data.sourceContainerName)
				: '',
		composeContent: data.defaultTemplate || '',
		envContent: data.envTemplate || ''
	};

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, formData);

	let composeOpen = $state(true);
	let envOpen = $state(true);
	let layoutMode = $state<'classic' | 'tree'>('classic');
	let treePaneWidth = $state(280);
	const workspaceDraft = new WorkspaceDraftState({
		fallbackTab: 'compose',
		initialOpenTabs: ['compose'],
		initialSelection: 'compose',
		isFixedTab: (key) => key === 'compose' || key === 'env',
		planCreate: planProjectWorkspaceFileCreate,
		planRename: planProjectWorkspaceFileRename,
		planMove: planWorkspaceFileMove
	});
	let newProjectTags = $state<ProjectTag[]>([]);
	const projectTagsQuery = createQuery(() => ({
		queryKey: queryKeys.projects.tags(currentEnvId),
		queryFn: () => projectService.getProjectTagsForEnvironment(currentEnvId)
	}));
	const availableProjectTags = $derived(projectTagsQuery.data ?? []);
	let validation = $state({
		composeHasErrors: false,
		envHasErrors: false,
		composeValidationReady: false,
		envValidationReady: false
	});

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));
	const newProjectWorkspaceEntries = $derived(workspaceDraft.entries);
	const newProjectWorkspaceLeadingRows = [
		{ key: 'compose', label: 'compose.yaml', iconClass: 'text-blue-500', locked: true },
		{ key: 'env', label: '.env', iconClass: 'text-green-500', locked: true }
	];
	let treeOutlineOpen = $state(false);
	let treeDiffOpen = $state(false);
	let treeCommandPaletteOpen = $state(false);
	const openTabs = $derived(workspaceDraft.validOpenTabs);
	const activeProjectTab = $derived(workspaceDraft.activeTab);
	const projectTabs = $derived(
		openTabs.map((key) => ({
			key,
			label: key === 'compose' ? 'compose.yaml' : key === 'env' ? '.env' : workspaceFileBasename(key.slice(5)),
			title: key === 'compose' ? 'compose.yaml' : key === 'env' ? '.env' : key.slice(5),
			iconClass: key === 'compose' ? 'text-blue-500' : key === 'env' ? 'text-green-500' : 'text-muted-foreground',
			pending: false
		}))
	);

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
	const createMenuBusy = $derived(ui.saving || ui.converting || ui.isLoadingTemplateContent);

	async function handleSubmit() {
		if (sourceContainerIds.length > 0 && canDeleteContainers) {
			openConfirmDialog({
				title: m.compose_create_project(),
				message: m.convert_create_message(),
				confirm: {
					label: m.compose_create_project(),
					button: 'create',
					action: (checkboxStates) => handleCreateProject(!!checkboxStates['removeOriginals'])
				},
				checkboxes: [{ id: 'removeOriginals', label: m.remove_original_containers() }]
			});
			return;
		}
		await handleCreateProject(false);
	}

	async function handleCreateProject(removeOriginals: boolean) {
		// Sync the authoritative compose name into form state at submit time so
		// validation and the create payload use it (event-time write, not an effect).
		if (composeYamlName) form.setValue('name', composeYamlName);
		await submitComposeResourceForm({
			validate: () => validateTemplateEditorForm(validationState, form.validate),
			setLoading: (value) => (ui.saving = value),
			submit: ({ name, composeContent, envContent }) =>
				projectService.createProject(name, composeContent, envContent, workspaceDraft.toDrafts(), newProjectTags),
			failureMessage: (name) => m.common_create_failed({ resource: `${m.resource_project()} "${name}"` }),
			onSuccess: async (project, { name }) => {
				toast.success(
					m.common_create_success({ resource: `${m.resource_project()} "${name}"` }),
					activityToastOptions(extractActivityId(project))
				);
				if (removeOriginals && canDeleteContainers) {
					for (const containerId of sourceContainerIds) {
						const { error } = await tryCatch(
							containerService.deleteContainer(containerId, {
								force: true,
								environmentId: data.sourceEnvironmentId
							})
						);
						if (error) toast.error(m.containers_remove_failed(), { description: extractApiErrorMessage(error) });
					}
				}
				// fallow-ignore-next-line code-duplication -- create-success handler; navigation target diverges per page
				goto(`/projects/${project.id}`, { refreshAll: true });
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

	function composePanelProps() {
		return {
			title: m.compose_compose_file_title(),
			language: 'yaml',
			validationMode: 'compose',
			error: $inputs.composeContent.error ?? undefined,
			fileId: 'projects:new:compose',
			editorContext: codeEditorContext
		} as const;
	}

	function envPanelProps() {
		return {
			title: m.compose_env_title(),
			language: 'env',
			validationMode: 'env',
			error: $inputs.envContent.error ?? undefined,
			fileId: 'projects:new:env',
			editorContext: codeEditorContext
		} as const;
	}
</script>

{#snippet newProjectWorkspaceEditor()}
	{#key activeProjectTab}
		{#if activeProjectTab === 'compose'}
			<CodePanel
				variant="plain"
				{...composePanelProps()}
				bind:open={composeOpen}
				bind:value={$inputs.composeContent.value}
				bind:hasErrors={validation.composeHasErrors}
				bind:validationReady={validation.composeValidationReady}
				bind:outlineOpen={treeOutlineOpen}
				bind:diffOpen={treeDiffOpen}
				bind:commandPaletteOpen={treeCommandPaletteOpen}
			/>
		{:else if activeProjectTab === 'env'}
			<CodePanel
				variant="plain"
				{...envPanelProps()}
				bind:open={envOpen}
				bind:value={$inputs.envContent.value}
				bind:hasErrors={validation.envHasErrors}
				bind:validationReady={validation.envValidationReady}
				bind:outlineOpen={treeOutlineOpen}
				bind:diffOpen={treeDiffOpen}
				bind:commandPaletteOpen={treeCommandPaletteOpen}
			/>
		{:else if activeProjectTab.startsWith('file:')}
			{@const relativePath = activeProjectTab.slice(5)}
			{#if workspaceDraft.binaryFiles[relativePath]}
				<div class="flex h-full min-h-0 items-center justify-center px-4 text-center text-sm text-muted-foreground">
					{workspaceReadOnlyMessage('binary', projectWorkspaceMaxFileSizeMb)}
				</div>
			{:else}
				<CodePanel
					variant="plain"
					open={true}
					title={relativePath}
					language={workspaceFileLanguage(relativePath)}
					validationMode="none"
					bind:value={workspaceDraft.contents[relativePath]}
					bind:hasErrors={workspaceDraft.hasErrors[relativePath]}
					bind:validationReady={workspaceDraft.validationReady[relativePath]}
					fileId={`projects:new:file:${relativePath}`}
					originalValue=""
					enableDiff={true}
					editorContext={codeEditorContext}
					bind:outlineOpen={treeOutlineOpen}
					bind:diffOpen={treeDiffOpen}
					bind:commandPaletteOpen={treeCommandPaletteOpen}
				/>
			{/if}
		{/if}
	{/key}
{/snippet}

{#snippet projectNameField(variant: 'inline' | 'block')}
	<EditableName
		bind:value={$inputs.name.value}
		displayValue={effectiveName}
		bind:ref={nameInputRef}
		{variant}
		error={$inputs.name.error ?? undefined}
		originalValue=""
		placeholder={m.compose_project_name_placeholder()}
		canEdit={!ui.saving && !ui.isLoadingTemplateContent && !composeYamlName}
		disabledMessage={composeYamlName ? m.compose_project_name_defined_in_yaml() : undefined}
		class={variant === 'inline' ? 'hidden sm:block' : undefined}
	/>
{/snippet}

<div class="flex h-full min-h-0 flex-col bg-background">
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
				<div class="hidden h-4 w-px bg-border sm:block"></div>
				<div class="hidden items-center gap-3 sm:flex">
					{@render projectNameField('inline')}
					<ProjectTagEditor bind:tags={newProjectTags} availableTags={availableProjectTags} canEdit={!ui.saving} />
				</div>
			</div>

			<div class="flex items-center gap-2">
				<ComposeCreateMenu
					tooltipOpen={!effectiveName && !createMenuBusy ? undefined : false}
					tooltipVisible={effectiveName === ''}
					tooltipTitle={m.compose_project_name_tooltip_title()}
					tooltipDescription={m.compose_project_name_tooltip_description()}
					tooltipExample={m.compose_project_name_tooltip_example()}
					showCreateButton={!hasEditorErrors && canCreateProject}
					createDisabled={!effectiveName || !$inputs.composeContent.value || hasEditorErrors || createMenuBusy}
					createLoading={ui.saving}
					createLabel={m.compose_create_project()}
					createLoadingLabel={m.common_action_creating()}
					onCreate={() => handleSubmit()}
					itemsDisabled={createMenuBusy}
					useTemplateLabel={m.common_use_template()}
					onUseTemplate={() => {
						// fallow-ignore-next-line code-duplication -- shared ComposeCreateMenu wiring with swarm stack create; labels/handlers are page-specific
						ui.showTemplateDialog = true;
					}}
					convertLabel={m.compose_convert_from_docker_run()}
					onConvert={() => (ui.showConverterDialog = true)}
					fromGitLabel={m.git_from_git_repo()}
					onFromGit={async () => goto(`/environments/${await environmentStore.getCurrentEnvironmentId()}/gitops?action=create`)}
					createTemplateLabel={m.templates_create_template()}
					createTemplateDisabled={!$inputs.name.value ||
						!$inputs.composeContent.value ||
						hasEditorErrors ||
						createMenuBusy ||
						ui.creatingTemplate}
					createTemplateLoading={ui.creatingTemplate}
					onCreateTemplate={handleCreateTemplate}
					createTemplatePermission="templates:create"
				/>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto h-full w-full max-w-full min-w-0">
			<div class="flex h-full min-h-0 flex-col gap-4">
				<div class="block flex-shrink-0 py-4 sm:hidden">
					{@render projectNameField('block')}
					<ProjectTagEditor bind:tags={newProjectTags} availableTags={availableProjectTags} canEdit={!ui.saving} class="mt-2" />
				</div>

				<div class="flex shrink-0 items-center justify-end gap-2">
					<label
						for="new-project-layout-mode-toggle"
						class="cursor-pointer text-xs text-muted-foreground"
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
							workspaceDraft.openTab('compose');
						}}
					/>
				</div>

				{#if layoutMode === 'tree'}
					<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-card">
						<ResizableSplit
							class="min-h-0 flex-1"
							{...composeTreeSplitProps}
							bind:size={treePaneWidth}
							ariaLabel={m.compose_editor_resize_files_panel()}
							persistKey="arcane.compose.split:tree"
							persistStorage="local"
						>
							{#snippet first()}
								<WorkspaceFileTreePanel
									leadingRows={newProjectWorkspaceLeadingRows}
									entries={newProjectWorkspaceEntries}
									selectedFile={workspaceDraft.selectedKey}
									disabled={ui.saving || ui.isLoadingTemplateContent}
									onSelect={workspaceDraft.openTab}
									onCreateFile={workspaceDraft.createFile}
									onCreateFolder={workspaceDraft.createFolder}
									onUpload={(parentPath, files) => workspaceDraft.uploadFile(parentPath, files, projectWorkspaceMaxFileSizeMb)}
									validateName={(name, parentPath) => validateProjectWorkspaceFileName(name, parentPath)}
									onRename={workspaceDraft.rename}
									onMove={workspaceDraft.move}
									onDelete={workspaceDraft.remove}
								/>
							{/snippet}

							{#snippet second()}
								<div class="flex h-full min-h-0 flex-1 flex-col">
									<EditorTabStrip
										tabs={projectTabs}
										activeKey={activeProjectTab}
										onSelect={workspaceDraft.openTab}
										onClose={workspaceDraft.closeTab}
									>
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
										{@render newProjectWorkspaceEditor()}
									</div>
								</div>
							{/snippet}
						</ResizableSplit>
					</div>
				{:else}
					<ComposeEditorSplit onsubmit={preventDefault(handleSubmit)}>
						{#snippet compose()}
							<CodePanel
								{...composePanelProps()}
								bind:open={composeOpen}
								bind:value={$inputs.composeContent.value}
								bind:hasErrors={validation.composeHasErrors}
								bind:validationReady={validation.composeValidationReady}
							/>
						{/snippet}

						{#snippet env()}
							<CodePanel
								{...envPanelProps()}
								bind:open={envOpen}
								bind:value={$inputs.envContent.value}
								bind:hasErrors={validation.envHasErrors}
								bind:validationReady={validation.envValidationReady}
							/>
						{/snippet}
					</ComposeEditorSplit>
				{/if}
				<!-- fallow-ignore-next-line code-duplication -- compose editor panel closing structure; ResizableSplit bindings/persistKey diverge per page -->
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
	onDownloadSuccess={refreshAll}
/>

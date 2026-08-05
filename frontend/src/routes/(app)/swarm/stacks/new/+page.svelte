<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { goto, refreshAll } from '$app/navigation';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import { preventDefault, createForm } from '#lib/utils/settings';
	import TemplateSelectionDialog from '#lib/components/dialogs/template-selection-dialog.svelte';
	import { m } from '#lib/paraglide/messages';
	import { swarmService } from '#lib/services/swarm-service.js';
	import ComposeCreateMenu from '#lib/components/compose-create-menu.svelte';
	import ComposeFileEditorPanel from '#lib/components/compose-file-editor-panel.svelte';
	import { ArrowLeftIcon, TrashIcon } from '#lib/icons';
	import CodePanel from '#lib/components/code-panel.svelte';
	import EditableName from '../../../projects/components/EditableName.svelte';
	import EditorTabStrip from '#lib/components/editor-tab-strip.svelte';
	import WorkspaceFileTreePanel from '#lib/components/workspace-file-tree-panel.svelte';
	import ResizableSplit from '#lib/components/resizable-split.svelte';
	import { Checkbox } from '#lib/components/ui/checkbox';
	import { Label } from '#lib/components/ui/label';
	import * as Select from '#lib/components/ui/select';
	import { environmentStore } from '#lib/stores/environment.store.svelte';
	import DockerRunConverterDialog from '#lib/components/compose/docker-run-converter-dialog.svelte';
	import { globalVariablesToMap } from '#lib/utils/template-load';
	import type { WorkspaceFileDraft } from '#lib/types/workspace';
	import type { SwarmSyncFile } from '#lib/types/swarm';
	import {
		isWorkspaceFileSelectionUnder,
		planWorkspaceFileCreate,
		planWorkspaceFileMove,
		planWorkspaceFileRename,
		readWorkspaceTextUpload,
		remapWorkspaceFilePath,
		remapWorkspaceFileRecord,
		remapSelectedWorkspaceFileKey,
		removeWorkspaceFileRecord,
		validateWorkspaceFileName,
		workspaceFileBasename,
		workspaceFileLanguage,
		workspaceFilePathMatches,
		type WorkspaceFileEntry
	} from '#lib/utils/workspace-files';
	import settingsStore from '#lib/stores/config-store';
	import {
		createComposeEditorSchema,
		createComposeTemplateDialogFlow,
		submitComposeResourceForm,
		templateNameSlug
	} from '#lib/utils/compose-flow';

	let { data } = $props();
	const workspaceMaxFileSizeMb = $derived($settingsStore?.projectWorkspaceMaxFileSizeMb ?? 10);

	let ui = $state({
		saving: false,
		converting: false,
		creatingTemplate: false,
		showTemplateDialog: false,
		showConverterDialog: false,
		isLoadingTemplateContent: false
	});
	const isEditMode = $derived(data.isEditMode === true);

	const formSchema = createComposeEditorSchema(m.common_name_required());

	function getInitialName() {
		if (data.sourceStackName) {
			return data.sourceStackName;
		}
		if (data.selectedTemplate) {
			return templateNameSlug(data.selectedTemplate.name);
		}
		return '';
	}

	function getInitialFormData() {
		return {
			name: getInitialName(),
			composeContent: data.defaultTemplate || '',
			envContent: data.envTemplate || ''
		};
	}

	const initialName = $derived(getInitialName());
	const backHref = $derived(isEditMode ? `/swarm/stacks/${encodeURIComponent(initialName)}` : '/swarm/stacks');
	const submitLabel = $derived(isEditMode ? m.common_save() : m.common_create_button({ resource: m.swarm_stack() }));
	const submitLoadingLabel = $derived(isEditMode ? m.common_saving() : m.common_action_creating());

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, getInitialFormData());

	let composeOpen = $state(true);
	let envOpen = $state(true);
	let nameInputRef = $state<HTMLInputElement | null>(null);
	let overrideContent = $state(untrack(() => data.overrideTemplate || ''));
	let overrideActive = $state(untrack(() => (data.overrideTemplate || '').trim().length > 0));
	let deployOptions = $state({
		prune: false,
		withRegistryAuth: false,
		resolveImage: 'always'
	});

	function decodeStackFileContent(content: string): string {
		try {
			const bytes = Uint8Array.from(atob(content), (character) => character.charCodeAt(0));
			return new TextDecoder().decode(bytes);
		} catch {
			return content;
		}
	}

	function encodeStackFileContent(content: string): string {
		const bytes = new TextEncoder().encode(content);
		let encoded = '';
		for (const byte of bytes) {
			encoded += String.fromCharCode(byte);
		}
		return btoa(encoded);
	}

	let stackFiles = $state<WorkspaceFileDraft[]>(
		untrack(() => (data.sourceFiles ?? []).map((file) => ({ relativePath: file.relativePath, isDirectory: false })))
	);
	let stackFileContents = $state<Record<string, string>>(
		untrack(() =>
			Object.fromEntries((data.sourceFiles ?? []).map((file) => [file.relativePath, decodeStackFileContent(file.content)]))
		)
	);
	const stackFileEntries = $derived.by<WorkspaceFileEntry[]>(() =>
		stackFiles.map((file) => ({
			path: file.relativePath,
			relativePath: file.relativePath,
			name: workspaceFileBasename(file.relativePath),
			isDirectory: !!file.isDirectory,
			size: file.isDirectory ? 0 : (stackFileContents[file.relativePath]?.length ?? 0),
			content: file.isDirectory ? undefined : (stackFileContents[file.relativePath] ?? ''),
			pending: false
		}))
	);
	const stackFilePaths = $derived.by(() => new Set(stackFileEntries.map((file) => file.relativePath)));
	const stackWorkspaceLeadingRows = $derived.by(() => [
		{ key: 'compose', label: 'compose.yaml', iconClass: 'text-blue-500', locked: true },
		...(overrideActive
			? [{ key: 'override', label: 'compose.override.yaml', iconClass: 'text-purple-500', locked: true }]
			: [{ key: 'add-override', label: m.compose_override_add(), action: true, onSelect: addOverride }]),
		{ key: 'env', label: '.env', iconClass: 'text-green-500', locked: true }
	]);

	let selectedStackFile = $state('compose');
	let openStackTabs = $state<string[]>(['compose']);
	let stackTreeWidth = $state<number | null>(null);
	let treeOutlineOpen = $state(false);
	let treeCommandPaletteOpen = $state(false);
	const stackOpenTabs = $derived.by(() => {
		const valid = openStackTabs.filter((key) => {
			if (key === 'compose' || key === 'env') return true;
			if (key === 'override') return overrideActive;
			if (!key.startsWith('file:')) return false;
			const entry = stackFileEntries.find((file) => file.relativePath === key.slice(5));
			return !!entry && !entry.isDirectory;
		});
		return valid.length > 0 ? valid : ['compose'];
	});
	const activeStackTab = $derived(
		stackOpenTabs.includes(selectedStackFile) ? selectedStackFile : (stackOpenTabs[0] ?? 'compose')
	);
	const stackTabs = $derived(
		stackOpenTabs.map((key) => ({
			key,
			label:
				key === 'compose'
					? 'compose.yaml'
					: key === 'override'
						? 'compose.override.yaml'
						: key === 'env'
							? '.env'
							: workspaceFileBasename(key.slice(5)),
			title:
				key === 'compose' ? 'compose.yaml' : key === 'override' ? 'compose.override.yaml' : key === 'env' ? '.env' : key.slice(5),
			iconClass:
				key === 'compose'
					? 'text-blue-500'
					: key === 'override'
						? 'text-purple-500'
						: key === 'env'
							? 'text-green-500'
							: 'text-muted-foreground',
			pending: false
		}))
	);

	function isStackDirectoryKey(key: string): boolean {
		if (!key.startsWith('file:')) return false;
		return stackFileEntries.find((file) => file.relativePath === key.slice(5))?.isDirectory === true;
	}

	function openStackTab(key: string) {
		if (!isStackDirectoryKey(key) && !openStackTabs.includes(key)) {
			openStackTabs = [...openStackTabs, key];
		}
		selectedStackFile = key;
	}

	function closeStackTab(key: string) {
		const index = stackOpenTabs.indexOf(key);
		const remaining = stackOpenTabs.filter((tab) => tab !== key);
		openStackTabs = openStackTabs.filter((tab) => tab !== key);
		if (selectedStackFile === key) {
			selectedStackFile = remaining[Math.min(Math.max(index - 1, 0), remaining.length - 1)] ?? 'compose';
		}
	}

	function addOverride() {
		overrideActive = true;
		openStackTab('override');
	}

	function removeOverride() {
		overrideContent = '';
		overrideActive = false;
		closeStackTab('override');
	}

	function createStackFile(parentPath: string, name: string, content = '') {
		const relativePath = planWorkspaceFileCreate(stackFilePaths, parentPath, name);
		if (!relativePath) return;
		stackFiles = [...stackFiles, { relativePath, isDirectory: false }];
		stackFileContents = { ...stackFileContents, [relativePath]: content };
		openStackTab(`file:${relativePath}`);
	}

	async function uploadStackFile(parentPath: string, files: File[]): Promise<string | void> {
		const file = files[0];
		if (!file) return m.workspace_upload_file_required();
		const result = await readWorkspaceTextUpload(file, workspaceMaxFileSizeMb);
		if (result.error) return result.error;
		createStackFile(parentPath, file.name, result.content ?? '');
	}

	function createStackFolder(parentPath: string, name: string) {
		const relativePath = planWorkspaceFileCreate(stackFilePaths, parentPath, name);
		if (!relativePath) return;
		stackFiles = [...stackFiles, { relativePath, isDirectory: true }];
		selectedStackFile = `file:${relativePath}`;
	}

	function applyStackFilePathChange(oldPath: string, newPath: string) {
		stackFiles = stackFiles.map((file) => ({
			...file,
			relativePath: remapWorkspaceFilePath(file.relativePath, oldPath, newPath)
		}));
		stackFileContents = remapWorkspaceFileRecord(stackFileContents, oldPath, newPath);
		openStackTabs = openStackTabs.map((tab) => remapSelectedWorkspaceFileKey(tab, oldPath, newPath) ?? tab);
		const remappedSelection = remapSelectedWorkspaceFileKey(selectedStackFile, oldPath, newPath);
		if (remappedSelection) {
			selectedStackFile = remappedSelection;
		}
	}

	function renameStackFile(relativePath: string, newName: string) {
		const plan = planWorkspaceFileRename(stackFilePaths, relativePath, newName);
		if (!plan) return;
		applyStackFilePathChange(relativePath, plan.newPath);
	}

	function moveStackFile(relativePath: string, newParentPath: string) {
		const entry = stackFileEntries.find((file) => file.relativePath === relativePath);
		const newPath = planWorkspaceFileMove(entry, stackFilePaths, relativePath, newParentPath);
		if (!newPath) return;
		applyStackFilePathChange(relativePath, newPath);
	}

	function deleteStackFile(relativePath: string) {
		stackFiles = stackFiles.filter((file) => !workspaceFilePathMatches(file.relativePath, relativePath));
		stackFileContents = removeWorkspaceFileRecord(stackFileContents, relativePath);
		openStackTabs = openStackTabs.filter((tab) => !isWorkspaceFileSelectionUnder(tab, relativePath));
		if (isWorkspaceFileSelectionUnder(selectedStackFile, relativePath)) {
			selectedStackFile = stackOpenTabs[0] ?? 'compose';
		}
	}

	function buildStackFilePayload(): SwarmSyncFile[] {
		return stackFiles
			.filter((file) => !file.isDirectory)
			.map((file) => ({
				relativePath: file.relativePath,
				content: encodeStackFileContent(stackFileContents[file.relativePath] ?? '')
			}));
	}

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));
	const stackEditorContext = $derived({
		envContent: $inputs.envContent.value,
		composeContents: [$inputs.composeContent.value, overrideContent].filter((content) => content.length > 0),
		globalVariables: globalVariableMap
	});

	async function handleSubmit() {
		if (isEditMode) {
			await handleSaveStackSource();
			return;
		}
		await handleDeployStack();
	}

	async function handleDeployStack() {
		await submitComposeResourceForm({
			validate: form.validate,
			setLoading: (value) => (ui.saving = value),
			submit: ({ name, composeContent, envContent }) =>
				swarmService.deployStack({
					name,
					composeContent,
					overrideContent,
					envContent,
					files: buildStackFilePayload(),
					prune: deployOptions.prune,
					withRegistryAuth: deployOptions.withRegistryAuth,
					resolveImage: deployOptions.resolveImage
				}),
			failureMessage: (name) => m.common_create_failed({ resource: `${m.swarm_stack()} "${name}"` }),
			onSuccess: async (_result, { name }) => {
				toast.success(m.common_create_success({ resource: `${m.swarm_stack()} "${name}"` }));
				goto('/swarm/stacks', { refreshAll: true });
			}
		});
	}

	async function handleSaveStackSource() {
		await submitComposeResourceForm({
			validate: form.validate,
			setLoading: (value) => (ui.saving = value),
			submit: ({ name, composeContent, envContent }) =>
				swarmService.updateStackSource(name, {
					composeContent,
					overrideContent,
					envContent,
					files: buildStackFilePayload()
				}),
			failureMessage: (name) => m.common_update_failed({ resource: `${m.swarm_stack()} "${name}"` }),
			onSuccess: async (_result, { name }) => {
				toast.success(m.common_update_success({ resource: `${m.swarm_stack()} "${name}"` }));
				goto(`/swarm/stacks/${encodeURIComponent(name)}`, { refreshAll: true });
			}
		});
	}

	const { composeHandlers, handleCreateTemplate } = createComposeTemplateDialogFlow({
		getInputs: () => $inputs,
		setInputValue: (key, value) => form.setValue(key, value),
		closeTemplateDialog: () => (ui.showTemplateDialog = false),
		validate: form.validate,
		setLoading: (value) => (ui.creatingTemplate = value)
	});

	const canSubmit = $derived(
		!!$inputs.name.value && !!$inputs.composeContent.value && !ui.saving && !ui.converting && !ui.isLoadingTemplateContent
	);
</script>

<div class="flex h-full min-h-0 flex-col bg-background">
	<div class="sticky top-0 mb-2 border-b">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					href={backHref}
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
				/>
				<div class="hidden h-4 w-px bg-border sm:block"></div>
				<div class="hidden items-center gap-3 sm:flex">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue={initialName}
						placeholder={m.compose_project_name_placeholder?.() || 'Enter name...'}
						canEdit={!isEditMode && !ui.saving && !ui.isLoadingTemplateContent}
						class="hidden sm:block"
					/>
				</div>
			</div>

			<div class="flex items-center gap-2">
				<ComposeCreateMenu
					tooltipOpen={!$inputs.name.value && !ui.saving && !ui.converting && !ui.isLoadingTemplateContent ? undefined : false}
					tooltipVisible={$inputs.name.value === ''}
					tooltipTitle={m.compose_project_name_tooltip_title()}
					tooltipDescription={m.compose_project_name_tooltip_description()}
					tooltipExample={m.compose_project_name_tooltip_example()}
					createDisabled={!canSubmit}
					createLoading={ui.saving}
					createLabel={submitLabel}
					createLoadingLabel={submitLoadingLabel}
					onCreate={() => handleSubmit()}
					itemsDisabled={ui.saving || ui.converting || ui.isLoadingTemplateContent}
					useTemplateLabel={m.common_use_template()}
					onUseTemplate={() => (ui.showTemplateDialog = true)}
					convertLabel={m.compose_convert_from_docker_run()}
					onConvert={() => (ui.showConverterDialog = true)}
					fromGitLabel={m.git_from_git_repo()}
					onFromGit={async () =>
						goto(`/environments/${await environmentStore.getCurrentEnvironmentId()}/gitops?action=create&targetType=swarm_stack`)}
					createTemplateLabel={m.templates_create_template()}
					createTemplateDisabled={!canSubmit || ui.creatingTemplate}
					createTemplateLoading={ui.creatingTemplate}
					onCreateTemplate={handleCreateTemplate}
				/>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto h-full w-full max-w-full min-w-0">
			<div class="flex h-full min-h-0 flex-col">
				<div class="block flex-shrink-0 px-2 py-4 sm:hidden sm:px-6">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="block"
						error={$inputs.name.error ?? undefined}
						originalValue={initialName}
						placeholder={m.compose_project_name_placeholder()}
						canEdit={!isEditMode && !ui.saving && !ui.isLoadingTemplateContent}
					/>
				</div>

				<form class="flex h-full min-h-0 flex-1 flex-col gap-3 px-2 pb-4 sm:px-6" onsubmit={preventDefault(handleSubmit)}>
					{#if !isEditMode}
						<div class="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-3 rounded-lg border border-border bg-card px-4 py-3">
							<div class="flex items-center gap-2">
								<Checkbox
									id="swarm-stack-prune"
									checked={deployOptions.prune}
									onCheckedChange={(checked) => (deployOptions.prune = checked === true)}
								/>
								<Label for="swarm-stack-prune" class="font-normal">{m.swarm_stack_deploy_prune()}</Label>
							</div>
							<div class="flex items-center gap-2">
								<Checkbox
									id="swarm-stack-registry-auth"
									checked={deployOptions.withRegistryAuth}
									onCheckedChange={(checked) => (deployOptions.withRegistryAuth = checked === true)}
								/>
								<Label for="swarm-stack-registry-auth" class="font-normal">{m.swarm_stack_registry_auth()}</Label>
							</div>
							<div class="flex min-w-56 items-center gap-2">
								<Label for="swarm-stack-resolve-image" class="shrink-0 font-normal">{m.swarm_stack_resolve_image()}</Label>
								<Select.Root type="single" bind:value={deployOptions.resolveImage}>
									<Select.Trigger id="swarm-stack-resolve-image" class="h-8 min-w-32">
										<span class="truncate">
											{deployOptions.resolveImage === 'changed'
												? m.swarm_stack_resolve_image_changed()
												: deployOptions.resolveImage === 'never'
													? m.common_never()
													: m.common_always()}
										</span>
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="always">{m.common_always()}</Select.Item>
										<Select.Item value="changed">{m.swarm_stack_resolve_image_changed()}</Select.Item>
										<Select.Item value="never">{m.common_never()}</Select.Item>
									</Select.Content>
								</Select.Root>
							</div>
						</div>
					{/if}
					<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-card">
						<ResizableSplit
							class="min-h-0 flex-1"
							variant="flush"
							firstClass="bg-muted/20 border-border flex min-h-0 flex-col border-b lg:border-r lg:border-b-0"
							secondClass="flex min-h-0 flex-col"
							bind:size={stackTreeWidth}
							minSize={200}
							maxSize={480}
							minSecondSize={360}
							defaultRatio={0.2}
							stackBelow={1024}
							ariaLabel={m.compose_editor_resize_files_panel()}
							persistKey="arcane.swarm.split:new"
						>
							{#snippet first()}
								<WorkspaceFileTreePanel
									leadingRows={stackWorkspaceLeadingRows}
									entries={stackFileEntries}
									selectedFile={selectedStackFile}
									disabled={ui.saving || ui.isLoadingTemplateContent}
									onSelect={openStackTab}
									onCreateFile={createStackFile}
									onCreateFolder={createStackFolder}
									onUpload={uploadStackFile}
									validateName={(name) => validateWorkspaceFileName(name)}
									onRename={renameStackFile}
									onMove={moveStackFile}
									onDelete={deleteStackFile}
								/>
							{/snippet}

							{#snippet second()}
								<div class="flex h-full min-h-0 flex-1 flex-col">
									<EditorTabStrip tabs={stackTabs} activeKey={activeStackTab} onSelect={openStackTab} onClose={closeStackTab}>
										{#snippet actions()}
											<ComposeFileEditorPanel
												outlineOpen={treeOutlineOpen}
												outlineLabel={m.compose_editor_toggle_outline()}
												onToggleOutline={() => (treeOutlineOpen = !treeOutlineOpen)}
												showDiff={false}
												commandPaletteLabel={m.compose_editor_command_palette()}
												onOpenCommandPalette={() => (treeCommandPaletteOpen = true)}
											/>
										{/snippet}
									</EditorTabStrip>
									<div class="flex min-h-0 flex-1 flex-col">
										{#key activeStackTab}
											{#if activeStackTab === 'compose'}
												<CodePanel
													variant="plain"
													bind:open={composeOpen}
													title="compose.yaml"
													language="yaml"
													bind:value={$inputs.composeContent.value}
													error={$inputs.composeContent.error ?? undefined}
													fileId={isEditMode ? `swarm:stacks:${initialName}:compose` : 'swarm:stacks:new:compose'}
													editorContext={stackEditorContext}
													bind:outlineOpen={treeOutlineOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{:else if activeStackTab === 'override'}
												<div class="flex min-h-0 flex-1 flex-col">
													<div class="flex shrink-0 items-center justify-between gap-2 border-b border-border px-3 py-2">
														<p class="text-xs text-muted-foreground">{m.compose_override_hint()}</p>
														<button
															type="button"
															class="flex shrink-0 items-center gap-1 text-xs font-medium text-muted-foreground hover:text-destructive"
															onclick={removeOverride}
														>
															<TrashIcon class="size-3.5 shrink-0" />
															<span>{m.compose_override_remove()}</span>
														</button>
													</div>
													<CodePanel
														variant="plain"
														open={true}
														title="compose.override.yaml"
														language="yaml"
														bind:value={overrideContent}
														fileId={isEditMode ? `swarm:stacks:${initialName}:override` : 'swarm:stacks:new:override'}
														editorContext={stackEditorContext}
														bind:outlineOpen={treeOutlineOpen}
														bind:commandPaletteOpen={treeCommandPaletteOpen}
													/>
												</div>
											{:else if activeStackTab === 'env'}
												<CodePanel
													variant="plain"
													bind:open={envOpen}
													title=".env"
													language="env"
													bind:value={$inputs.envContent.value}
													error={$inputs.envContent.error ?? undefined}
													fileId={isEditMode ? `swarm:stacks:${initialName}:env` : 'swarm:stacks:new:env'}
													editorContext={stackEditorContext}
													bind:outlineOpen={treeOutlineOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{:else if activeStackTab.startsWith('file:')}
												{@const relativePath = activeStackTab.slice(5)}
												<CodePanel
													variant="plain"
													open={true}
													title={relativePath}
													language={workspaceFileLanguage(relativePath)}
													validationMode="none"
													bind:value={stackFileContents[relativePath]}
													fileId={`swarm:stacks:${isEditMode ? initialName : 'new'}:file:${relativePath}`}
													editorContext={stackEditorContext}
													bind:outlineOpen={treeOutlineOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{/if}
										{/key}
									</div>
								</div>
							{/snippet}
						</ResizableSplit>
					</div>
				</form>
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

<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { goto, refreshAll } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import { preventDefault, createForm } from '$lib/utils/settings';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service.js';
	import ComposeCreateMenu from '$lib/components/compose-create-menu.svelte';
	import ComposeFileEditorPanel from '$lib/components/compose-file-editor-panel.svelte';
	import { ArrowLeftIcon } from '$lib/icons';
	import CodePanel from '../../../projects/components/CodePanel.svelte';
	import EditableName from '../../../projects/components/EditableName.svelte';
	import EditorTabStrip from '../../../projects/components/EditorTabStrip.svelte';
	import ProjectFileTreePanel from '../../../projects/components/ProjectFileTreePanel.svelte';
	import ResizableSplit from '$lib/components/resizable-split.svelte';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import DockerRunConverterDialog from '$lib/components/compose/docker-run-converter-dialog.svelte';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import {
		createComposeEditorSchema,
		createComposeTemplateDialogFlow,
		submitComposeResourceForm,
		templateNameSlug
	} from '$lib/utils/compose-flow';

	let { data } = $props();

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

	let selectedStackFile = $state('compose');
	let openStackTabs = $state<string[]>(['compose']);
	let stackTreeWidth = $state<number | null>(null);
	let treeOutlineOpen = $state(false);
	let treeCommandPaletteOpen = $state(false);
	const stackOpenTabs = $derived(openStackTabs.length > 0 ? openStackTabs : ['compose']);
	const activeStackTab = $derived(
		stackOpenTabs.includes(selectedStackFile) ? selectedStackFile : (stackOpenTabs[0] ?? 'compose')
	);
	const stackTabs = $derived(
		stackOpenTabs.map((key) => ({
			key,
			label: key === 'compose' ? 'compose.yaml' : '.env',
			title: key === 'compose' ? 'compose.yaml' : '.env',
			iconClass: key === 'compose' ? 'text-blue-500' : 'text-green-500',
			pending: false
		}))
	);

	function openStackTab(key: string) {
		if (!openStackTabs.includes(key)) {
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

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));

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
			submit: ({ name, composeContent, envContent }) => swarmService.deployStack({ name, composeContent, envContent }),
			failureMessage: (name) => m.common_create_failed({ resource: `${m.swarm_stack()} "${name}"` }),
			onSuccess: async (_result, { name }) => {
				toast.success(m.common_create_success({ resource: `${m.swarm_stack()} "${name}"` }));
				goto('/swarm/stacks', { invalidateAll: true });
			}
		});
	}

	async function handleSaveStackSource() {
		await submitComposeResourceForm({
			validate: form.validate,
			setLoading: (value) => (ui.saving = value),
			submit: ({ name, composeContent, envContent }) => swarmService.deployStack({ name, composeContent, envContent }),
			failureMessage: (name) => m.common_update_failed({ resource: `${m.swarm_stack()} "${name}"` }),
			onSuccess: async (_result, { name }) => {
				toast.success(m.common_update_success({ resource: `${m.swarm_stack()} "${name}"` }));
				goto(`/swarm/stacks/${encodeURIComponent(name)}`, { invalidateAll: true });
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

<div class="bg-background flex h-full min-h-0 flex-col">
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
				<div class="bg-border hidden h-4 w-px sm:block"></div>
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

				<form class="flex h-full min-h-0 flex-1 flex-col px-2 pb-4 sm:px-6" onsubmit={preventDefault(handleSubmit)}>
					<div class="bg-card border-border flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
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
								<ProjectFileTreePanel
									composeFileName="compose.yaml"
									entries={[]}
									selectedFile={selectedStackFile}
									disabled={ui.saving || ui.isLoadingTemplateContent}
									onSelect={openStackTab}
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
													editorContext={{
														envContent: $inputs.envContent.value,
														composeContents: [$inputs.composeContent.value],
														globalVariables: globalVariableMap
													}}
													bind:outlineOpen={treeOutlineOpen}
													bind:commandPaletteOpen={treeCommandPaletteOpen}
												/>
											{:else if activeStackTab === 'env'}
												<CodePanel
													variant="plain"
													bind:open={envOpen}
													title=".env"
													language="env"
													bind:value={$inputs.envContent.value}
													error={$inputs.envContent.error ?? undefined}
													fileId={isEditMode ? `swarm:stacks:${initialName}:env` : 'swarm:stacks:new:env'}
													editorContext={{
														envContent: $inputs.envContent.value,
														composeContents: [$inputs.composeContent.value],
														globalVariables: globalVariableMap
													}}
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

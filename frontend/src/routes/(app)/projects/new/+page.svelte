<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { ArrowLeftIcon, TerminalIcon, TemplateIcon, AddIcon, GitBranchIcon } from '$lib/icons';
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
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { ComposeEditorSplit } from '$lib/components/compose';
	import DockerRunConverterDialog from '$lib/components/compose/docker-run-converter-dialog.svelte';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import {
		createComposeEditorSchema,
		createComposeTemplateDialogFlow,
		dropdownContentClass,
		dropdownItemClass,
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
	let validation = $state({
		composeHasErrors: false,
		envHasErrors: false,
		composeValidationReady: false,
		envValidationReady: false
	});

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));
	const validationState = $derived(
		getTemplateEditorValidationState(
			validation.composeValidationReady,
			validation.envValidationReady,
			validation.composeHasErrors,
			validation.envHasErrors
		)
	);
	let hasEditorErrors = $derived(hasTemplateEditorErrors(validationState));

	let nameInputRef = $state<HTMLInputElement | null>(null);

	async function handleSubmit() {
		await handleCreateProject();
	}

	async function handleCreateProject() {
		await submitComposeResourceForm({
			validate: () => validateTemplateEditorForm(validationState, form.validate),
			setLoading: (value) => (ui.saving = value),
			submit: ({ name, composeContent, envContent }) => projectService.createProject(name, composeContent, envContent),
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
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.compose_project_name_placeholder?.() || 'Enter project name...'}
						canEdit={!ui.saving && !ui.isLoadingTemplateContent}
						class="hidden sm:block"
					/>
				</div>
			</div>

			<div class="flex items-center gap-2">
				<ButtonGroup.Root>
					<ArcaneTooltip.Root
						open={!$inputs.name.value && !ui.saving && !ui.converting && !ui.isLoadingTemplateContent ? undefined : false}
					>
						<ArcaneTooltip.Trigger>
							<span>
								{#if !hasEditorErrors && canCreateProject}
									<ArcaneButton
										action="create"
										tone="ghost"
										disabled={!$inputs.name.value ||
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
							{#if $inputs.name.value === ''}
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
						bind:ref={nameInputRef}
						variant="block"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.compose_project_name_placeholder()}
						canEdit={!ui.saving && !ui.isLoadingTemplateContent}
					/>
				</div>

				<ComposeEditorSplit onsubmit={preventDefault(handleSubmit)}>
					{#snippet compose()}
						<CodePanel
							bind:open={composeOpen}
							title={m.compose_compose_file_title()}
							language="yaml"
							bind:value={$inputs.composeContent.value}
							error={$inputs.composeContent.error ?? undefined}
							bind:hasErrors={validation.composeHasErrors}
							bind:validationReady={validation.composeValidationReady}
							fileId="projects:new:compose"
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					{/snippet}

					{#snippet env()}
						<CodePanel
							bind:open={envOpen}
							title={m.compose_env_title()}
							language="env"
							bind:value={$inputs.envContent.value}
							error={$inputs.envContent.error ?? undefined}
							bind:hasErrors={validation.envHasErrors}
							bind:validationReady={validation.envValidationReady}
							fileId="projects:new:env"
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					{/snippet}
				</ComposeEditorSplit>
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

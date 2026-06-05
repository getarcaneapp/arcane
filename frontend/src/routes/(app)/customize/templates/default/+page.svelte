<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card';
	import { toast } from 'svelte-sonner';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import { createForm } from '$lib/utils/settings';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { goto } from '$app/navigation';
	import TemplateSelectionDialog from '$lib/components/dialogs/template-selection-dialog.svelte';
	import { ComposeEditorSplit } from '$lib/components/compose';
	import { untrack } from 'svelte';
	import type { Template } from '$lib/types/swarm';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import { ArrowLeftIcon, CodeIcon, VariableIcon } from '$lib/icons';
	import {
		createTemplateContentSchema,
		getTemplateEditorValidationState,
		hasTemplateEditorErrors,
		resetTemplateEditorFields,
		runTemplateEditorSave
	} from '$lib/utils/template-editor';

	let { data } = $props();

	let ui = $state({
		saving: false,
		showTemplateDialog: false,
		isLoadingTemplate: false
	});
	let originalComposeContent = $state(untrack(() => data.composeTemplate));
	let originalEnvContent = $state(untrack(() => data.envTemplate));

	let validation = $state({
		composeHasErrors: false,
		envHasErrors: false,
		composeValidationReady: false,
		envValidationReady: false
	});

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));

	const formSchema = createTemplateContentSchema();

	let formData = $derived({
		composeContent: originalComposeContent,
		envContent: originalEnvContent
	});

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, formData));

	const hasChanges = $derived(
		$inputs.composeContent.value !== originalComposeContent || $inputs.envContent.value !== originalEnvContent
	);
	const validationState = $derived(
		getTemplateEditorValidationState(
			validation.composeValidationReady,
			validation.envValidationReady,
			validation.composeHasErrors,
			validation.envHasErrors
		)
	);

	const canSave = $derived(hasChanges && !hasTemplateEditorErrors(validationState));

	async function handleSave() {
		await runTemplateEditorSave({
			validationState,
			validate: form.validate,
			save: ({ composeContent, envContent }) => templateService.saveDefaultTemplates(composeContent, envContent),
			failureMessage: m.templates_save_failed(),
			setLoading: (value) => (ui.saving = value),
			onSuccess: async () => {
				toast.success(m.templates_save_success());
				originalComposeContent = $inputs.composeContent.value;
				originalEnvContent = $inputs.envContent.value;
			}
		});
	}

	async function handleReset() {
		resetTemplateEditorFields([
			{
				set: (value) => ($inputs.composeContent.value = value),
				value: originalComposeContent
			},
			{
				set: (value) => ($inputs.envContent.value = value),
				value: originalEnvContent
			}
		]);
	}

	async function handleTemplateSelect(template: Template) {
		ui.showTemplateDialog = false;
		ui.isLoadingTemplate = true;

		try {
			const templateContent = await templateService.getTemplateContent(template.id);
			$inputs.composeContent.value = templateContent.content ?? template.content ?? '';
			$inputs.envContent.value = templateContent.envContent ?? template.envContent ?? '';
			toast.success(m.compose_template_loaded({ name: template.name }));
		} catch (error) {
			console.error('Error loading template:', error);
			toast.error(error instanceof Error ? error.message : m.templates_download_failed());
		} finally {
			ui.isLoadingTemplate = false;
		}
	}
</script>

<div class="container mx-auto flex h-full min-h-0 max-w-full flex-col gap-6 overflow-hidden p-2 pb-10 sm:p-6 sm:pb-10">
	<div class="space-y-3 sm:space-y-4">
		<ArcaneButton action="base" tone="ghost" onclick={() => goto('/customize/templates')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.templates_title() })}</span>
		</ArcaneButton>

		<div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
			<div>
				<h1 class="text-xl font-semibold wrap-break-word sm:text-2xl">{m.templates_defaults_title()}</h1>
				<p class="text-muted-foreground mt-1.5 text-sm wrap-break-word sm:text-base">
					{m.templates_defaults_description()}
				</p>
			</div>
			<div class="flex flex-col gap-2 sm:flex-row">
				<ArcaneButton
					action="base"
					tone="outline"
					onclick={() => (ui.showTemplateDialog = true)}
					disabled={ui.saving || ui.isLoadingTemplate}
				>
					{m.common_use_template()}
				</ArcaneButton>
				<ArcaneButton action="cancel" onclick={handleReset} disabled={!hasChanges || ui.saving || ui.isLoadingTemplate}>
					{m.common_reset()}
				</ArcaneButton>
				<ArcaneButton
					action="save"
					onclick={handleSave}
					disabled={!canSave || ui.isLoadingTemplate}
					loading={ui.saving}
					loadingLabel={m.common_action_saving()}
				/>
			</div>
		</div>
	</div>

	<ComposeEditorSplit
		class="flex min-h-0 flex-1 flex-col gap-6 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch"
		composeClass="contents"
		envClass="contents"
	>
		{#snippet compose()}
			<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-3">
				<Card.Header icon={CodeIcon} class="shrink-0">
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.templates_compose_template_label()}</h2>
						</Card.Title>
						<Card.Description>{m.templates_service_definitions()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
					<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
						<CodeEditor
							bind:value={$inputs.composeContent.value}
							language="yaml"
							readOnly={ui.saving || ui.isLoadingTemplate}
							fontSize="13px"
							bind:hasErrors={validation.composeHasErrors}
							bind:validationReady={validation.composeValidationReady}
							fileId="templates:defaults:compose"
							originalValue={originalComposeContent}
							enableDiff={true}
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					</div>
				</Card.Content>
				{#if $inputs.composeContent.error}
					<Card.Footer class="pt-0">
						<p class="text-destructive text-xs font-medium">
							{$inputs.composeContent.error}
						</p>
					</Card.Footer>
				{/if}
			</Card.Root>
		{/snippet}

		{#snippet env()}
			<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-2">
				<Card.Header icon={VariableIcon} class="shrink-0">
					<div class="flex flex-col space-y-1.5">
						<Card.Title>
							<h2>{m.templates_env_template_label()}</h2>
						</Card.Title>
						<Card.Description>{m.templates_default_config_values()}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
					<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
						<CodeEditor
							bind:value={$inputs.envContent.value}
							language="env"
							readOnly={ui.saving || ui.isLoadingTemplate}
							fontSize="13px"
							bind:hasErrors={validation.envHasErrors}
							bind:validationReady={validation.envValidationReady}
							fileId="templates:defaults:env"
							originalValue={originalEnvContent}
							enableDiff={true}
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					</div>
				</Card.Content>
				{#if $inputs.envContent.error}
					<Card.Footer class="pt-0">
						<p class="text-destructive text-xs font-medium">
							{$inputs.envContent.error}
						</p>
					</Card.Footer>
				{/if}
			</Card.Root>
		{/snippet}
	</ComposeEditorSplit>
</div>

<TemplateSelectionDialog bind:open={ui.showTemplateDialog} templates={data.templates || []} onSelect={handleTemplateSelect} />

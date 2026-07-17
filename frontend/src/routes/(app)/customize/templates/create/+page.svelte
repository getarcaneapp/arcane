<script lang="ts">
	import { goto } from '$app/navigation';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { createForm } from '$lib/utils/settings';
	import { tryCatch } from '$lib/utils/api';
	import { toast } from 'svelte-sonner';
	import TemplateEditorWorkspace from '../components/template-editor-workspace.svelte';
	import { globalVariablesToMap } from '$lib/utils/template-load';
	import {
		createNamedTemplateSchema,
		getTemplateEditorValidationState,
		hasTemplateEditorErrors,
		validateTemplateEditorForm
	} from '$lib/utils/template-editor';

	let { data } = $props();

	let saving = $state(false);
	let validation = $state({
		composeHasErrors: false,
		envHasErrors: false,
		composeValidationReady: false,
		envValidationReady: false
	});

	const globalVariableMap = $derived(globalVariablesToMap(data.globalVariables));

	const formSchema = createNamedTemplateSchema();

	const initialValues = {
		name: '',
		description: '',
		composeContent: '',
		envContent: ''
	};

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, initialValues);

	const hasEditorErrors = $derived(
		hasTemplateEditorErrors(
			getTemplateEditorValidationState(
				validation.composeValidationReady,
				validation.envValidationReady,
				validation.composeHasErrors,
				validation.envHasErrors
			)
		)
	);
	const canCreate = $derived(!!$inputs.name.value && !!$inputs.composeContent.value && !hasEditorErrors && !saving);

	async function handleCreate() {
		const validated = validateTemplateEditorForm(
			getTemplateEditorValidationState(
				validation.composeValidationReady,
				validation.envValidationReady,
				validation.composeHasErrors,
				validation.envHasErrors
			),
			form.validate
		);
		if (!validated) return;

		handleApiResultWithCallbacks({
			result: await tryCatch(
				templateService.createTemplate({
					name: validated.name,
					description: validated.description,
					content: validated.composeContent,
					envContent: validated.envContent
				})
			),
			message: m.templates_create_template_failed(),
			setLoadingState: (value) => (saving = value),
			onSuccess: async (created) => {
				toast.success(m.templates_create_template_success({ name: validated.name }));
				await goto(`/customize/templates/${created.id}`);
			}
		});
	}
</script>

<TemplateEditorWorkspace
	{inputs}
	bind:validation
	fileIdPrefix="templates:create"
	{globalVariableMap}
	{saving}
	onSubmit={handleCreate}
>
	{#snippet toolbarActions()}
		<ArcaneButton action="cancel" onclick={() => goto('/customize/templates')} disabled={saving} />
		<ArcaneButton
			action="create"
			customLabel={m.templates_create_template()}
			onclick={handleCreate}
			disabled={!canCreate}
			loading={saving}
			loadingLabel={m.common_action_creating()}
		/>
	{/snippet}
</TemplateEditorWorkspace>

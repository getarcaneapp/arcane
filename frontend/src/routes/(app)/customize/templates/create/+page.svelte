<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as Card from '$lib/components/ui/card';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { goto } from '$app/navigation';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { createForm } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';
	import { ArrowLeftIcon, CodeIcon, VariableIcon } from '$lib/icons';

	let { data } = $props();

	let saving = $state(false);
	let composeHasErrors = $state(false);
	let envHasErrors = $state(false);
	let composeValidationReady = $state(false);
	let envValidationReady = $state(false);

	const globalVariableMap = $derived.by(() =>
		Object.fromEntries((data.globalVariables ?? []).map((item) => [item.key, item.value]))
	);

	const formSchema = z.object({
		name: z.string().min(1, m.templates_template_name_required()),
		description: z.string().optional().default(''),
		composeContent: z.string().min(1, m.templates_content_required()),
		envContent: z.string().optional().default('')
	});

	const initialValues = {
		name: '',
		description: '',
		composeContent: '',
		envContent: ''
	};

	let { inputs, ...form } = $derived(createForm<typeof formSchema>(formSchema, initialValues));

	const canCreate = $derived(composeValidationReady && envValidationReady && !composeHasErrors && !envHasErrors);

	async function handleCreate() {
		if (!composeValidationReady || !envValidationReady || composeHasErrors || envHasErrors) {
			toast.error(m.templates_validation_error());
			return;
		}
		const validated = form.validate();
		if (!validated) {
			toast.error(m.templates_validation_error());
			return;
		}

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

<div class="container mx-auto flex h-full min-h-0 max-w-full flex-col gap-6 overflow-hidden p-2 pb-10 sm:p-6 sm:pb-10">
	<div class="space-y-3 sm:space-y-4">
		<ArcaneButton action="base" tone="ghost" onclick={() => goto('/customize/templates')} class="w-fit gap-2">
			<ArrowLeftIcon class="size-4" />
			<span>{m.common_back_to({ resource: m.templates_title() })}</span>
		</ArcaneButton>

		<div class="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
			<div class="min-w-0 flex-1 space-y-3">
				<FormInput
					input={$inputs.name}
					label={m.templates_template_name_label()}
					placeholder={m.templates_template_name_placeholder()}
					disabled={saving}
				/>
				<FormInput
					input={$inputs.description}
					label={m.templates_template_description_label()}
					placeholder={m.templates_template_description_placeholder()}
					disabled={saving}
				/>
			</div>
			<div class="flex flex-col gap-2 sm:flex-row sm:items-start">
				<ArcaneButton action="cancel" onclick={() => goto('/customize/templates')} disabled={saving}>
					{m.common_cancel()}
				</ArcaneButton>
				{#if composeValidationReady && envValidationReady && !composeHasErrors && !envHasErrors}
					<ArcaneButton
						action="create"
						customLabel={m.templates_create_template()}
						onclick={handleCreate}
						disabled={!canCreate}
						loading={saving}
						loadingLabel={m.common_action_saving()}
					/>
				{/if}
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 flex-col gap-6 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch">
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
						readOnly={saving}
						fontSize="13px"
						bind:hasErrors={composeHasErrors}
						bind:validationReady={composeValidationReady}
						fileId="templates:create:compose"
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
					<p class="text-destructive text-xs font-medium">{$inputs.composeContent.error}</p>
				</Card.Footer>
			{/if}
		</Card.Root>

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
						readOnly={saving}
						fontSize="13px"
						bind:hasErrors={envHasErrors}
						bind:validationReady={envValidationReady}
						fileId="templates:create:env"
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
					<p class="text-destructive text-xs font-medium">{$inputs.envContent.error}</p>
				</Card.Footer>
			{/if}
		</Card.Root>
	</div>
</div>

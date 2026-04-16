<script lang="ts">
	import { goto } from '$app/navigation';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import { ArrowLeftIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { templateService } from '$lib/services/template-service';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { createForm, preventDefault } from '$lib/utils/form.utils';
	import { tryCatch } from '$lib/utils/try-catch';
	import { toast } from 'svelte-sonner';
	import { z } from 'zod/v4';
	import CodePanel from '../../../projects/components/CodePanel.svelte';
	import EditableName from '../../../projects/components/EditableName.svelte';

	let { data } = $props();

	let saving = $state(false);
	let composeHasErrors = $state(false);
	let envHasErrors = $state(false);
	let composeValidationReady = $state(false);
	let envValidationReady = $state(false);
	let nameInputRef = $state<HTMLInputElement | null>(null);

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

	const { inputs, ...form } = createForm<typeof formSchema>(formSchema, initialValues);

	const hasEditorErrors = $derived(!composeValidationReady || !envValidationReady || composeHasErrors || envHasErrors);
	const canCreate = $derived(!!$inputs.name.value && !!$inputs.composeContent.value && !hasEditorErrors && !saving);

	async function handleCreate() {
		if (hasEditorErrors) {
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

<div class="bg-background flex h-full min-h-0 flex-col">
	<div class="bg-background/95 supports-[backdrop-filter]:bg-background/80 sticky top-0 z-10 mb-2 border-b backdrop-blur">
		<div class="mx-auto flex h-16 max-w-full items-center justify-between gap-4 px-6">
			<div class="flex min-w-0 items-center gap-4">
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					class="gap-2 bg-transparent"
					icon={ArrowLeftIcon}
					customLabel={m.common_back()}
					onclick={() => goto('/customize/templates')}
				/>
				<div class="bg-border hidden h-4 w-px sm:block"></div>
				<div class="hidden min-w-0 items-center gap-3 sm:flex">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.templates_template_name_placeholder()}
						canEdit={!saving}
						class="hidden sm:block"
					/>
				</div>
			</div>

			<div class="flex items-center gap-2">
				<ArcaneButton action="cancel" onclick={() => goto('/customize/templates')} disabled={saving} />
				<ArcaneButton
					action="create"
					customLabel={m.templates_create_template()}
					onclick={handleCreate}
					disabled={!canCreate}
					loading={saving}
					loadingLabel={m.common_action_creating()}
				/>
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto flex h-full w-full max-w-full min-w-0 flex-col px-2 pb-6 sm:px-6 sm:pb-6">
			<form class="flex min-h-0 flex-1 flex-col gap-4" onsubmit={preventDefault(handleCreate)}>
				<div class="block flex-shrink-0 py-4 sm:hidden">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="block"
						error={$inputs.name.error ?? undefined}
						originalValue=""
						placeholder={m.templates_template_name_placeholder()}
						canEdit={!saving}
					/>
				</div>

				<div class="flex-shrink-0 px-1 pt-1">
					<div class="max-w-2xl">
						<FormInput
							input={$inputs.description}
							label={m.templates_template_description_label()}
							placeholder={m.templates_template_description_placeholder()}
							disabled={saving}
						/>
					</div>
				</div>

				<div class="flex min-h-0 flex-1 flex-col gap-4 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch">
					<div class="flex min-h-0 flex-1 flex-col lg:col-span-3">
						<CodePanel
							title="compose.yaml"
							language="yaml"
							bind:value={$inputs.composeContent.value}
							error={$inputs.composeContent.error ?? undefined}
							readOnly={saving}
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

					<div class="flex min-h-0 flex-1 flex-col lg:col-span-2">
						<CodePanel
							title=".env"
							language="env"
							bind:value={$inputs.envContent.value}
							error={$inputs.envContent.error ?? undefined}
							readOnly={saving}
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
				</div>
			</form>
		</div>
	</div>
</div>

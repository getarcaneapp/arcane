<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import FormInput from '$lib/components/form/form-input.svelte';
	import CodePanel from '../../../projects/components/CodePanel.svelte';
	import EditableName from '../../../projects/components/EditableName.svelte';
	import { ComposeEditorSplit } from '$lib/components/compose';
	import { goto } from '$app/navigation';
	import { m } from '$lib/paraglide/messages';
	import { preventDefault, type FormInputs } from '$lib/utils/settings';
	import { ArrowLeftIcon } from '$lib/icons';
	import type { Writable } from 'svelte/store';
	import type { Snippet } from 'svelte';

	interface ValidationState {
		composeHasErrors: boolean;
		envHasErrors: boolean;
		composeValidationReady: boolean;
		envValidationReady: boolean;
	}

	type NamedTemplateValues = {
		name: string;
		description: string;
		composeContent: string;
		envContent: string;
	};

	let {
		inputs,
		validation = $bindable(),
		originalName = '',
		originalCompose,
		originalEnv,
		fileIdPrefix,
		globalVariableMap,
		saving = false,
		onSubmit,
		toolbarActions
	}: {
		inputs: Writable<FormInputs<NamedTemplateValues>>;
		validation: ValidationState;
		originalName?: string;
		originalCompose?: string;
		originalEnv?: string;
		fileIdPrefix: string;
		globalVariableMap: Record<string, string>;
		saving?: boolean;
		onSubmit: () => void;
		toolbarActions: Snippet;
	} = $props();

	let nameInputRef = $state<HTMLInputElement | null>(null);
	const enableDiff = $derived(originalCompose !== undefined);
</script>

<div class="flex h-full min-h-0 flex-col bg-background">
	<div class="sticky top-0 mb-2 border-b">
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
				<div class="hidden h-4 w-px bg-border sm:block"></div>
				<div class="hidden min-w-0 items-center gap-3 sm:flex">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="inline"
						error={$inputs.name.error ?? undefined}
						originalValue={originalName}
						placeholder={m.templates_template_name_placeholder()}
						canEdit={!saving}
						class="hidden sm:block"
					/>
				</div>
			</div>

			<div class="flex items-center gap-2">
				{@render toolbarActions()}
			</div>
		</div>
	</div>

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div class="mx-auto flex h-full w-full max-w-full min-w-0 flex-col px-2 pb-6 sm:px-6 sm:pb-6">
			<form class="flex min-h-0 flex-1 flex-col gap-4" onsubmit={preventDefault(onSubmit)}>
				<div class="block flex-shrink-0 py-4 sm:hidden">
					<EditableName
						bind:value={$inputs.name.value}
						bind:ref={nameInputRef}
						variant="block"
						error={$inputs.name.error ?? undefined}
						originalValue={originalName}
						placeholder={m.templates_template_name_placeholder()}
						canEdit={!saving}
					/>
				</div>

				<div class="flex-shrink-0 px-1 pt-1">
					<div class="max-w-2xl">
						<FormInput
							input={$inputs.description}
							label={m.common_description()}
							placeholder={m.templates_template_description_placeholder()}
							disabled={saving}
						/>
					</div>
				</div>

				<ComposeEditorSplit>
					{#snippet compose()}
						<CodePanel
							title="compose.yaml"
							language="yaml"
							bind:value={$inputs.composeContent.value}
							error={$inputs.composeContent.error ?? undefined}
							readOnly={saving}
							bind:hasErrors={validation.composeHasErrors}
							bind:validationReady={validation.composeValidationReady}
							fileId="{fileIdPrefix}:compose"
							originalValue={originalCompose}
							{enableDiff}
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					{/snippet}

					{#snippet env()}
						<CodePanel
							title=".env"
							language="env"
							bind:value={$inputs.envContent.value}
							error={$inputs.envContent.error ?? undefined}
							readOnly={saving}
							bind:hasErrors={validation.envHasErrors}
							bind:validationReady={validation.envValidationReady}
							fileId="{fileIdPrefix}:env"
							originalValue={originalEnv}
							{enableDiff}
							editorContext={{
								envContent: $inputs.envContent.value,
								composeContents: [$inputs.composeContent.value],
								globalVariables: globalVariableMap
							}}
						/>
					{/snippet}
				</ComposeEditorSplit>
			</form>
		</div>
	</div>
</div>

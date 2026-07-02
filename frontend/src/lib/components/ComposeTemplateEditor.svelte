<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import { ComposeEditorSplit } from '$lib/components/compose';
	import { CodeIcon, VariableIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';

	interface ValidationState {
		composeHasErrors: boolean;
		envHasErrors: boolean;
		composeValidationReady: boolean;
		envValidationReady: boolean;
	}

	interface Props {
		composeValue: string;
		envValue: string;
		originalCompose: string;
		originalEnv: string;
		validation: ValidationState;
		globalVariableMap: Record<string, string>;
		fileIdPrefix: string;
		readOnly?: boolean;
		composeError?: string | null;
		envError?: string | null;
		composeLabel?: string;
		composeDescription?: string;
		envLabel?: string;
		envDescription?: string;
	}

	let {
		composeValue = $bindable(),
		envValue = $bindable(),
		originalCompose,
		originalEnv,
		validation = $bindable(),
		globalVariableMap,
		fileIdPrefix,
		readOnly = false,
		composeError,
		envError,
		composeLabel = m.templates_compose_template_label(),
		composeDescription = m.templates_service_definitions(),
		envLabel = m.templates_env_template_label(),
		envDescription = m.templates_default_config_values()
	}: Props = $props();
</script>

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
						<h2>{composeLabel}</h2>
					</Card.Title>
					<Card.Description>{composeDescription}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
				<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
					<CodeEditor
						bind:value={composeValue}
						language="yaml"
						{readOnly}
						fontSize="13px"
						bind:hasErrors={validation.composeHasErrors}
						bind:validationReady={validation.composeValidationReady}
						fileId="{fileIdPrefix}:compose"
						originalValue={originalCompose}
						enableDiff={true}
						editorContext={{
							envContent: envValue,
							composeContents: [composeValue],
							globalVariables: globalVariableMap
						}}
					/>
				</div>
			</Card.Content>
			{#if composeError}
				<Card.Footer class="pt-0">
					<p class="text-destructive text-xs font-medium">{composeError}</p>
				</Card.Footer>
			{/if}
		</Card.Root>
	{/snippet}

	{#snippet env()}
		<Card.Root class="flex min-h-0 min-w-0 flex-1 flex-col lg:col-span-2">
			<Card.Header icon={VariableIcon} class="shrink-0">
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{envLabel}</h2>
					</Card.Title>
					<Card.Description>{envDescription}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="flex min-h-0 min-w-0 flex-1 flex-col p-0">
				<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
					<CodeEditor
						bind:value={envValue}
						language="env"
						{readOnly}
						fontSize="13px"
						bind:hasErrors={validation.envHasErrors}
						bind:validationReady={validation.envValidationReady}
						fileId="{fileIdPrefix}:env"
						originalValue={originalEnv}
						enableDiff={true}
						editorContext={{
							envContent: envValue,
							composeContents: [composeValue],
							globalVariables: globalVariableMap
						}}
					/>
				</div>
			</Card.Content>
			{#if envError}
				<Card.Footer class="pt-0">
					<p class="text-destructive text-xs font-medium">{envError}</p>
				</Card.Footer>
			{/if}
		</Card.Root>
	{/snippet}
</ComposeEditorSplit>

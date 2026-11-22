<script lang="ts">
	import { ComposeEditorSplit } from '#lib/components/compose/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { CodeIcon, VariableIcon, type IconType } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

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

{#snippet editorCard(opts: {
	className: string;
	icon: IconType;
	label: string;
	description: string;
	language: 'yaml' | 'env';
	fileId: string;
	originalValue: string;
	error?: string | null;
	getValue: () => string;
	setValue: (v: string) => void;
	getHasErrors: () => boolean;
	setHasErrors: (v: boolean) => void;
	getValidationReady: () => boolean;
	setValidationReady: (v: boolean) => void;
})}
	<Card.Root class="flex min-h-90 min-w-0 flex-1 flex-col lg:min-h-0 {opts.className}">
		<Card.Header icon={opts.icon} class="shrink-0">
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{opts.label}</h2>
				</Card.Title>
				<Card.Description>{opts.description}</Card.Description>
			</div>
		</Card.Header>
		<div class="flex min-h-0 min-w-0 flex-1 flex-col">
			<div class="min-h-0 min-w-0 flex-1 rounded-b-xl">
				{#await import('#lib/components/code-editor/editor.svelte')}
					<p class="p-4 text-sm text-muted-foreground">{m.common_loading()}</p>
				{:then { default: CodeEditor }}
					<CodeEditor
						bind:value={opts.getValue, opts.setValue}
						language={opts.language}
						{readOnly}
						fontSize="13px"
						bind:hasErrors={opts.getHasErrors, opts.setHasErrors}
						bind:validationReady={opts.getValidationReady, opts.setValidationReady}
						fileId={opts.fileId}
						originalValue={opts.originalValue}
						enableDiff={true}
						editorContext={{
							envContent: envValue,
							composeContents: [composeValue],
							globalVariables: globalVariableMap
						}}
					/>
				{:catch}
					<p class="p-4 text-sm text-destructive">{m.code_editor_load_failed()}</p>
				{/await}
			</div>
		</div>
		{#if opts.error}
			<Card.Footer>
				<p class="text-xs font-medium text-destructive">{opts.error}</p>
			</Card.Footer>
		{/if}
	</Card.Root>
{/snippet}

<ComposeEditorSplit
	class="flex min-h-0 flex-1 flex-col gap-6 lg:grid lg:grid-cols-5 lg:grid-rows-1 lg:items-stretch"
	composeClass="contents"
	envClass="contents"
>
	{#snippet compose()}
		{@render editorCard({
			className: 'lg:col-span-3',
			icon: CodeIcon,
			label: composeLabel,
			description: composeDescription,
			language: 'yaml',
			fileId: `${fileIdPrefix}:compose`,
			originalValue: originalCompose,
			error: composeError,
			getValue: () => composeValue,
			setValue: (v) => (composeValue = v),
			getHasErrors: () => validation.composeHasErrors,
			setHasErrors: (v) => (validation.composeHasErrors = v),
			getValidationReady: () => validation.composeValidationReady,
			setValidationReady: (v) => (validation.composeValidationReady = v)
		})}
	{/snippet}

	{#snippet env()}
		{@render editorCard({
			className: 'lg:col-span-2',
			icon: VariableIcon,
			label: envLabel,
			description: envDescription,
			language: 'env',
			fileId: `${fileIdPrefix}:env`,
			originalValue: originalEnv,
			error: envError,
			getValue: () => envValue,
			setValue: (v) => (envValue = v),
			getHasErrors: () => validation.envHasErrors,
			setHasErrors: (v) => (validation.envHasErrors = v),
			getValidationReady: () => validation.envValidationReady,
			setValidationReady: (v) => (validation.envValidationReady = v)
		})}
	{/snippet}
</ComposeEditorSplit>

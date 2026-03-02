<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import { CodeIcon, FileTextIcon, SearchIcon, ArrowsUpDownIcon } from '$lib/icons';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import type { DiagnosticSummary, EditorContext } from '$lib/components/code-editor/analysis/types';

	type CodeLanguage = 'yaml' | 'env';

	let {
		title,
		open = $bindable(),
		language,
		value = $bindable(),
		error,
		autoHeight = false,
		readOnly = false,
		hasErrors = $bindable(false),
		validationReady = $bindable(false),
		diagnosticSummary = $bindable({
			errors: 0,
			warnings: 0,
			infos: 0,
			hints: 0,
			schemaStatus: 'unavailable',
			schemaMessage: undefined,
			unresolvedVariables: [],
			secretWarnings: 0,
			duplicateKeyWarnings: 0,
			duplicateEnvWarnings: 0,
			cursorLine: 1,
			cursorCol: 1,
			validationReady: false
		} as DiagnosticSummary),
		fileId,
		originalValue,
		enableDiff = false,
		editorContext,
		outlineOpen = $bindable(false),
		diffOpen = $bindable(false),
		commandPaletteOpen = $bindable(false)
	}: {
		title: string;
		open: boolean;
		language: CodeLanguage;
		value: string;
		error?: string;
		autoHeight?: boolean;
		readOnly?: boolean;
		hasErrors?: boolean;
		validationReady?: boolean;
		diagnosticSummary?: DiagnosticSummary;
		fileId?: string;
		originalValue?: string;
		enableDiff?: boolean;
		editorContext?: EditorContext;
		outlineOpen?: boolean;
		diffOpen?: boolean;
		commandPaletteOpen?: boolean;
	} = $props();

	const isMobile = new IsMobile();
	const effectiveAutoHeight = $derived(autoHeight || isMobile.current);
</script>

<Card.Root class="flex {effectiveAutoHeight ? '' : 'flex-1'} min-h-0 flex-col overflow-hidden">
	<Card.Header icon={CodeIcon} class="flex-shrink-0 items-center">
		<Card.Title>
			<h2>{title}</h2>
		</Card.Title>
		<Card.Action class="flex items-center gap-1 pt-1">
			<ArcaneButton
				action="base"
				tone={outlineOpen ? 'outline-primary' : 'ghost'}
				size="icon"
				showLabel={false}
				icon={FileTextIcon}
				customLabel="Toggle outline"
				onclick={() => (outlineOpen = !outlineOpen)}
			/>
			{#if enableDiff && originalValue !== undefined}
				<ArcaneButton
					action="base"
					tone={diffOpen ? 'outline-primary' : 'ghost'}
					size="icon"
					showLabel={false}
					icon={ArrowsUpDownIcon}
					customLabel="Toggle diff"
					onclick={() => (diffOpen = !diffOpen)}
				/>
			{/if}
			<ArcaneButton
				action="base"
				tone="ghost"
				size="icon"
				showLabel={false}
				icon={SearchIcon}
				customLabel="Command palette"
				onclick={() => (commandPaletteOpen = true)}
			/>
		</Card.Action>
	</Card.Header>
	<Card.Content class="relative z-0 flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col overflow-visible p-0">
		<div class="{effectiveAutoHeight ? '' : 'relative flex-1'} min-h-0 w-full min-w-0">
			{#if effectiveAutoHeight}
				<CodeEditor
					bind:value
					{language}
					fontSize="13px"
					autoHeight={true}
					{readOnly}
					bind:hasErrors
					bind:validationReady
					bind:diagnosticSummary
					{fileId}
					{originalValue}
					{enableDiff}
					{editorContext}
					bind:outlineOpen
					bind:diffOpen
					bind:commandPaletteOpen
				/>
			{:else}
				<div class="absolute inset-0">
					<CodeEditor
						bind:value
						{language}
						fontSize="13px"
						{readOnly}
						bind:hasErrors
						bind:validationReady
						bind:diagnosticSummary
						{fileId}
						{originalValue}
						{enableDiff}
						{editorContext}
						bind:outlineOpen
						bind:diffOpen
						bind:commandPaletteOpen
					/>
				</div>
			{/if}
		</div>
		{#if error}
			<p class="text-destructive px-4 py-2 text-xs">{error}</p>
		{/if}
	</Card.Content>
</Card.Root>

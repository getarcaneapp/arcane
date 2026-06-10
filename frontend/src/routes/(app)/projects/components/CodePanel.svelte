<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { ArcaneButton } from '$lib/components/arcane-button';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import { CodeIcon, FileTextIcon, SearchIcon, ArrowsUpDownIcon } from '$lib/icons';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';
	import { m } from '$lib/paraglide/messages';
	import type {
		CodeLanguage,
		CodeValidationMode,
		DiagnosticSummary,
		EditorContext
	} from '$lib/components/code-editor/analysis/types';

	let {
		title,
		open = $bindable(true),
		language,
		validationMode,
		value = $bindable(''),
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
		commandPaletteOpen = $bindable(false),
		variant = 'card'
	}: {
		title: string;
		open?: boolean;
		language: CodeLanguage;
		validationMode?: CodeValidationMode;
		value?: string;
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
		variant?: 'card' | 'plain';
	} = $props();

	const isMobile = new IsMobile();
	const effectiveAutoHeight = $derived(autoHeight || isMobile.current);
</script>

{#snippet editorBody()}
	<div class="{effectiveAutoHeight ? '' : 'relative flex-1'} min-h-0 w-full min-w-0">
		<div class={effectiveAutoHeight ? '' : 'absolute inset-0'}>
			<CodeEditor
				bind:value
				{language}
				{validationMode}
				fontSize="13px"
				autoHeight={effectiveAutoHeight}
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
	</div>
	{#if error}
		<p class="text-destructive px-4 py-2 text-xs">{error}</p>
	{/if}
{/snippet}

{#if variant === 'plain'}
	<div class="relative z-0 flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col" data-open={open}>
		{@render editorBody()}
	</div>
{:else}
	<Card.Root class="flex {effectiveAutoHeight ? '' : 'flex-1'} min-h-0 flex-col overflow-hidden" data-open={open}>
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
					customLabel={m.compose_editor_toggle_outline()}
					onclick={() => (outlineOpen = !outlineOpen)}
				/>
				{#if enableDiff && originalValue !== undefined}
					<ArcaneButton
						action="base"
						tone={diffOpen ? 'outline-primary' : 'ghost'}
						size="icon"
						showLabel={false}
						icon={ArrowsUpDownIcon}
						customLabel={m.compose_editor_toggle_diff()}
						onclick={() => (diffOpen = !diffOpen)}
					/>
				{/if}
				<ArcaneButton
					action="base"
					tone="ghost"
					size="icon"
					showLabel={false}
					icon={SearchIcon}
					customLabel={m.compose_editor_command_palette()}
					onclick={() => (commandPaletteOpen = true)}
				/>
			</Card.Action>
		</Card.Header>
		<Card.Content class="relative z-0 flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col overflow-visible p-0">
			{@render editorBody()}
		</Card.Content>
	</Card.Root>
{/if}

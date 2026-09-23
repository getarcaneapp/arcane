<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import CodeEditor from '#lib/components/code-editor/editor.svelte';
	import { CodeIcon, FileTextIcon, SearchIcon, ArrowsUpDownIcon } from '#lib/icons/index.js';
	import { IsMobile } from '#lib/hooks/is-mobile.svelte.js';
	import { m } from '#lib/paraglide/messages.js';
	import type {
		CodeLanguage,
		CodeValidationMode,
		DiagnosticSummary,
		EditorContext
	} from '#lib/components/code-editor/analysis/types.js';

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
		variant = 'card',
		gitEditUrl
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
		gitEditUrl?: string | null;
	} = $props();

	// Physical viewport only: auto height is about screen space, not the layout preference.
	const isMobile = new IsMobile({ honorLayoutMode: false });
	const effectiveAutoHeight = $derived(autoHeight || isMobile.current);
	const editUrl = $derived(readOnly && !(enableDiff && diffOpen) && gitEditUrl ? gitEditUrl : null);

	// CodeMirror's gutters, search panel and tooltips share this wrapper.
	function isEditorText(target: EventTarget | null): boolean {
		return target instanceof Element && !!target.closest('.cm-content');
	}

	function handleEditorClick(event: MouseEvent) {
		if (!editUrl || !isEditorText(event.target)) return;
		if (!window.getSelection()?.isCollapsed) return;
		window.open(editUrl, '_blank', 'noopener,noreferrer');
	}

	function handleEditorKeydown(event: KeyboardEvent) {
		if (!editUrl || event.key !== 'Enter') return;
		if (event.target !== event.currentTarget && !isEditorText(event.target)) return;
		event.preventDefault();
		window.open(editUrl, '_blank', 'noopener,noreferrer');
	}
</script>

{#snippet editorBody()}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div
		class="{effectiveAutoHeight ? '' : 'relative flex-1'} min-h-0 w-full min-w-0 {editUrl ? 'cursor-pointer' : ''}"
		role={editUrl ? 'link' : undefined}
		tabindex={editUrl ? 0 : undefined}
		title={editUrl ? m.git_edit_file_in_repository() : undefined}
		onclick={handleEditorClick}
		onkeydown={handleEditorKeydown}
	>
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
		<p class="px-4 py-2 text-xs text-destructive">{error}</p>
	{/if}
{/snippet}

{#if variant === 'plain'}
	<div class="relative z-(--arcane-z-content) flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col" data-open={open}>
		{@render editorBody()}
	</div>
{:else}
	<Card.Root class="flex {effectiveAutoHeight ? '' : 'flex-1'} min-h-0 flex-col overflow-hidden" data-open={open}>
		<Card.Header icon={CodeIcon} class="flex-shrink-0">
			<Card.Title>
				<h2>{title}</h2>
			</Card.Title>
			<Card.Action>
				<div class="flex items-center gap-1 pt-1">
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
				</div>
			</Card.Action>
		</Card.Header>
		<div class="relative z-(--arcane-z-content) flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col overflow-visible">
			{@render editorBody()}
		</div>
	</Card.Root>
{/if}

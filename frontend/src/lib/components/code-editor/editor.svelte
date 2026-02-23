<script lang="ts">
	import CodeMirror from 'svelte-codemirror-editor';
	import * as Command from '$lib/components/ui/command';
	import { autocompletion, type Completion, type CompletionContext } from '@codemirror/autocomplete';
	import { yaml } from '@codemirror/lang-yaml';
	import { StreamLanguage, foldAll, unfoldAll, foldKeymap } from '@codemirror/language';
	import { properties } from '@codemirror/legacy-modes/mode/properties';
	import {
		linter,
		lintGutter,
		forEachDiagnostic,
		nextDiagnostic,
		previousDiagnostic,
		type Diagnostic,
		type LintSource
	} from '@codemirror/lint';
	import { keymap, hoverTooltip, EditorView } from '@codemirror/view';
	import { type Extension } from '@codemirror/state';
	import { parseDocument } from 'yaml';
	import { browser } from '$app/environment';
	import { m } from '$lib/paraglide/messages';
	import configStore from '$lib/stores/config-store';
	import { arcaneDarkInit } from './theme';
	import { createDefaultSummary, ENV_SNIPPETS, YAML_SNIPPETS } from './editor-constants';
	import { createMergeEnterIndentKeymap, createMergeHostAction, type MergeActionParams } from './merge-editor';
	import {
		analyzeComposeContent,
		findYamlPositionContext,
		resolveVariableSourceAtPosition,
		type YamlPositionContext
	} from './analysis/compose-analysis';
	import { analyzeEnvContent } from './analysis/env-analysis';
	import {
		getComposeSchemaContext,
		getCompletionOptionsForPath,
		getEnumValueCompletions,
		getSchemaDocForPath,
		type ComposeSchemaContext
	} from './analysis/compose-schema';
	import type { CodeLanguage, DiagnosticSummary, EditorContext, OutlineItem } from './analysis/types';

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		placeholder = '',
		readOnly = false,
		fontSize = '12px',
		autoHeight = false,
		hasErrors = $bindable(false),
		fileId,
		originalValue,
		enableDiff = false,
		editorContext = {} as EditorContext,
		validationReady = $bindable(false),
		diagnosticSummary = $bindable(createDefaultSummary()),
		outlineOpen = $bindable(false),
		diffOpen = $bindable(false),
		commandPaletteOpen = $bindable(false)
	}: {
		value?: string;
		language?: CodeLanguage;
		placeholder?: string;
		readOnly?: boolean;
		fontSize?: string;
		autoHeight?: boolean;
		hasErrors?: boolean;
		fileId?: string;
		originalValue?: string;
		enableDiff?: boolean;
		editorContext?: EditorContext;
		validationReady?: boolean;
		diagnosticSummary?: DiagnosticSummary;
		outlineOpen?: boolean;
		diffOpen?: boolean;
		commandPaletteOpen?: boolean;
	} = $props();

	let activeOutlineItems = $state<OutlineItem[]>([]);
	let activeView = $state<EditorView | null>(null);
	let normalView = $state<EditorView | null>(null);
	let schemaState = $state<ComposeSchemaContext | null>(null);
	let shortcutsEnabled = $derived($configStore?.keyboardShortcutsEnabled !== false);

	const storageKey = $derived(fileId ? `arcane.editor.state:${fileId}` : null);
	const isDiffActive = $derived(Boolean(enableDiff && diffOpen && originalValue !== undefined));
	const effectiveEditorContext = $derived({
		envContent: editorContext?.envContent ?? '',
		composeContents: editorContext?.composeContents ?? [],
		globalVariables: editorContext?.globalVariables ?? {}
	});

	const mergeActionParams = $derived({
		diffActive: isDiffActive,
		language,
		value,
		baseline: originalValue ?? ''
	} satisfies MergeActionParams);

	function updateSummary(patch: Partial<DiagnosticSummary>) {
		let changed = false;
		for (const key in patch) {
			if (diagnosticSummary[key as keyof DiagnosticSummary] !== patch[key as keyof DiagnosticSummary]) {
				changed = true;
				break;
			}
		}
		if (!changed) return;

		diagnosticSummary = {
			...createDefaultSummary(),
			...diagnosticSummary,
			...patch
		};
	}

	function updateSummaryFromDiagnostics(diagnostics: Diagnostic[], patch: Partial<DiagnosticSummary> = {}) {
		let errors = 0;
		let warnings = 0;
		let infos = 0;
		let hints = 0;

		for (const diagnostic of diagnostics) {
			switch (diagnostic.severity) {
				case 'error':
					errors += 1;
					break;
				case 'warning':
					warnings += 1;
					break;
				case 'info':
					infos += 1;
					break;
				case 'hint':
					hints += 1;
					break;
			}
		}

		hasErrors = errors > 0;
		validationReady = true;
		updateSummary({
			errors,
			warnings,
			infos,
			hints,
			validationReady: true,
			...patch
		});
	}

	function markReadOnlyReady() {
		if (!readOnly) return;
		hasErrors = false;
		validationReady = true;
		updateSummary({
			errors: 0,
			warnings: 0,
			infos: 0,
			hints: 0,
			validationReady: true
		});
	}

	function updateCursorSummary(view: EditorView) {
		const position = view.state.selection.main.head;
		const line = view.state.doc.lineAt(position);
		updateSummary({
			cursorLine: line.number,
			cursorCol: position - line.from + 1
		});
	}

	function persistEditorState(view: EditorView) {
		if (!browser || !storageKey) return;
		try {
			const payload = {
				head: view.state.selection.main.head,
				scrollTop: view.scrollDOM.scrollTop
			};
			sessionStorage.setItem(storageKey, JSON.stringify(payload));
		} catch {
			// ignore persistence errors
		}
	}

	function restoreEditorState(view: EditorView) {
		if (!browser || !storageKey) return;
		try {
			const raw = sessionStorage.getItem(storageKey);
			if (!raw) return;
			const payload = JSON.parse(raw) as { head?: number; scrollTop?: number };
			if (typeof payload.head === 'number') {
				const bounded = Math.max(0, Math.min(payload.head, view.state.doc.length));
				view.dispatch({ selection: { anchor: bounded } });
			}
			if (typeof payload.scrollTop === 'number') {
				requestAnimationFrame(() => {
					view.scrollDOM.scrollTop = payload.scrollTop ?? 0;
				});
			}
		} catch {
			// ignore bad state payload
		}
	}

	function focusEditor() {
		activeView?.focus();
	}

	function jumpToOutlineItem(item: OutlineItem) {
		if (!activeView) return;
		activeView.dispatch({
			selection: { anchor: item.from },
			scrollIntoView: true
		});
		activeView.focus();
	}

	function formatEnvContent(content: string): string {
		const lines = content.split(/\r?\n/);
		const formatted: string[] = [];
		for (const line of lines) {
			const trimmed = line.trim();
			if (!trimmed || trimmed.startsWith('#')) {
				formatted.push(trimmed);
				continue;
			}
			const valueLine = trimmed.startsWith('export ') ? trimmed.slice(7).trim() : trimmed;
			const separator = valueLine.indexOf('=');
			if (separator < 0) {
				formatted.push(trimmed);
				continue;
			}
			const key = valueLine.slice(0, separator).trim().toUpperCase().replace(/\s+/g, '_');
			const valuePart = valueLine.slice(separator + 1).trim();
			formatted.push(`${key}=${valuePart}`);
		}
		return formatted.join('\n').replace(/\n{3,}/g, '\n\n');
	}

	function formatDocument() {
		if (!activeView || readOnly) return;
		const current = activeView.state.doc.toString();
		let formatted = current;

		if (language === 'yaml') {
			const parsed = parseDocument(current, { strict: false, uniqueKeys: false });
			if (parsed.errors.length === 0) {
				formatted = parsed.toString({ indent: 2, lineWidth: 0 });
			}
		} else {
			formatted = formatEnvContent(current);
		}

		if (formatted === current) return;

		activeView.dispatch({
			changes: { from: 0, to: activeView.state.doc.length, insert: formatted }
		});
	}

	function goToLine() {
		if (!activeView) return;
		const raw = window.prompt('Go to line', String(diagnosticSummary.cursorLine));
		if (!raw) return;
		const lineNumber = Number.parseInt(raw, 10);
		if (Number.isNaN(lineNumber)) return;
		const line = activeView.state.doc.line(Math.max(1, Math.min(lineNumber, activeView.state.doc.lines)));
		activeView.dispatch({
			selection: { anchor: line.from },
			scrollIntoView: true
		});
		activeView.focus();
	}

	function executeCommand(id: string) {
		commandPaletteOpen = false;
		switch (id) {
			case 'format':
				formatDocument();
				break;
			case 'next-diagnostic':
				if (activeView) nextDiagnostic(activeView);
				break;
			case 'prev-diagnostic':
				if (activeView) previousDiagnostic(activeView);
				break;
			case 'toggle-outline':
				outlineOpen = !outlineOpen;
				break;
			case 'toggle-diff':
				if (enableDiff && originalValue !== undefined) {
					diffOpen = !diffOpen;
				}
				break;
			case 'fold-all':
				if (activeView) foldAll(activeView);
				break;
			case 'unfold-all':
				if (activeView) unfoldAll(activeView);
				break;
			case 'jump-line':
				goToLine();
				break;
		}
	}

	const commandItems = $derived.by(() => {
		const items = [
			{ id: 'format', label: 'Format document', shortcut: 'Shift+Alt+F' },
			{ id: 'next-diagnostic', label: 'Next diagnostic', shortcut: 'F8' },
			{ id: 'prev-diagnostic', label: 'Previous diagnostic', shortcut: 'Shift+F8' },
			{ id: 'toggle-outline', label: outlineOpen ? 'Hide outline' : 'Show outline' },
			{ id: 'fold-all', label: 'Fold all' },
			{ id: 'unfold-all', label: 'Unfold all' },
			{ id: 'jump-line', label: 'Jump to line' }
		];

		if (enableDiff && originalValue !== undefined) {
			items.splice(4, 0, { id: 'toggle-diff', label: diffOpen ? 'Hide diff' : 'Show diff' });
		}

		return items;
	});

	const schemaCompletions = async (context: CompletionContext, yamlContext: YamlPositionContext): Promise<Completion[]> => {
		const schema = await getComposeSchemaContext();
		schemaState = schema;
		updateSummary({
			schemaStatus: schema.status,
			schemaMessage: schema.message
		});

		if (!schema.schema) return [];
		const before = context.matchBefore(/[\w.-]*/);
		const prefix = before?.text ?? '';

		if (yamlContext.atKey) {
			return [...YAML_SNIPPETS, ...getCompletionOptionsForPath(schema.schema, yamlContext.parentPath, prefix)];
		}

		return getEnumValueCompletions(schema.schema, yamlContext.path);
	};

	const composeCompletionSource = async (context: CompletionContext) => {
		if (language !== 'yaml' || readOnly) return null;
		const source = context.state.doc.toString();
		const yamlContext = findYamlPositionContext(source, context.pos);
		if (!yamlContext) return null;

		const before = context.matchBefore(/[\w.-]*/);
		if (!context.explicit && (!before || before.from === before.to)) return null;

		const options = await schemaCompletions(context, yamlContext);
		if (options.length === 0) return null;

		return {
			from: before ? before.from : context.pos,
			options,
			validFor: /[\w.-]*/
		};
	};

	const envCompletionSource = async (context: CompletionContext) => {
		if (language !== 'env' || readOnly) return null;
		const before = context.matchBefore(/[A-Za-z0-9_.-]*/);
		if (!context.explicit && (!before || before.from === before.to)) return null;

		const variableOptions = Array.from(
			new Set<string>([
				...Object.keys(effectiveEditorContext.globalVariables),
				...(effectiveEditorContext.envContent ?? '')
					.split(/\r?\n/)
					.map((line) => line.trim())
					.filter(Boolean)
					.map((line) => line.split('=')[0]?.trim() ?? '')
					.filter(Boolean)
			])
		)
			.sort((a, b) => a.localeCompare(b))
			.map(
				(key) =>
					({
						label: key,
						type: 'variable',
						apply: `${key}=`
					}) satisfies Completion
			);

		return {
			from: before ? before.from : context.pos,
			options: [...ENV_SNIPPETS, ...variableOptions],
			validFor: /[A-Za-z0-9_.-]*/
		};
	};

	const yamlHover = hoverTooltip(async (view, position) => {
		if (language !== 'yaml') return null;
		const source = view.state.doc.toString();
		const variableRef = resolveVariableSourceAtPosition(source, position, effectiveEditorContext);
		if (variableRef) {
			return {
				pos: position,
				create() {
					const dom = document.createElement('div');
					dom.className = 'arcane-hover';
					dom.innerHTML = `<strong>${variableRef.name}</strong><div>Source: ${variableRef.source}</div>`;
					return { dom };
				}
			};
		}

		const yamlContext = findYamlPositionContext(source, position);
		if (!yamlContext || !yamlContext.currentKey) return null;

		const schema = schemaState ?? (await getComposeSchemaContext());
		schemaState = schema;
		if (!schema.schema) return null;

		const doc = getSchemaDocForPath(schema.schema, yamlContext.path);
		if (!doc) return null;

		return {
			pos: yamlContext.keyFrom ?? position,
			end: yamlContext.keyTo ?? position,
			create() {
				const dom = document.createElement('div');
				dom.className = 'arcane-hover';
				const examples =
					doc.examples && doc.examples.length > 0 ? `<div><strong>Examples:</strong> ${doc.examples.join(', ')}</div>` : '';
				dom.innerHTML = `
					<div><strong>${doc.title ?? yamlContext.currentKey}</strong></div>
					${doc.description ? `<div>${doc.description}</div>` : ''}
					${doc.defaultValue ? `<div><strong>Default:</strong> ${doc.defaultValue}</div>` : ''}
					${examples}
				`;
				return { dom };
			}
		};
	});

	const editorLifecycleExtension = EditorView.updateListener.of((update) => {
		if (!update.view) return;
		if (activeView !== update.view) {
			activeView = update.view;
		}
		if (update.docChanged || update.selectionSet) {
			updateCursorSummary(update.view);
			persistEditorState(update.view);
		}
	});

	const shortcutKeymapExtension = keymap.of([
		...foldKeymap,
		{
			key: 'F8',
			run(view) {
				return nextDiagnostic(view);
			}
		},
		{
			key: 'Shift-F8',
			run(view) {
				return previousDiagnostic(view);
			}
		},
		{
			key: 'Shift-Alt-f',
			run() {
				formatDocument();
				return true;
			}
		},
		{
			key: 'Mod-Shift-p',
			run() {
				if (!shortcutsEnabled) return false;
				commandPaletteOpen = true;
				return true;
			}
		}
	]);

	const yamlLinter: LintSource = async (view): Promise<Diagnostic[]> => {
		if (readOnly) {
			validationReady = true;
			hasErrors = false;
			return [];
		}

		const schema = await getComposeSchemaContext();
		schemaState = schema;
		const analysis = await analyzeComposeContent(view, schema, effectiveEditorContext);
		activeOutlineItems = analysis.outlineItems;

		updateSummaryFromDiagnostics(analysis.diagnostics, {
			schemaStatus: schema.status,
			schemaMessage: schema.message,
			...analysis.summaryPatch
		});

		return analysis.diagnostics;
	};

	const envLinter: LintSource = async (view): Promise<Diagnostic[]> => {
		if (readOnly) {
			validationReady = true;
			hasErrors = false;
			return [];
		}

		const analysis = analyzeEnvContent(view.state.doc.toString(), effectiveEditorContext);
		activeOutlineItems = analysis.outlineItems;

		updateSummaryFromDiagnostics(analysis.diagnostics, {
			schemaStatus: schemaState?.status ?? 'unavailable',
			schemaMessage: schemaState?.message,
			...analysis.summaryPatch
		});

		return analysis.diagnostics;
	};

	function getLanguageExtension(lang: CodeLanguage, options: { lightweight?: boolean } = {}): Extension[] {
		const lightweight = options.lightweight === true;
		const extensions: Extension[] = [editorLifecycleExtension, shortcutKeymapExtension];

		switch (lang) {
			case 'yaml':
				extensions.push(yaml());
				if (!readOnly && !lightweight) {
					extensions.push(
						lintGutter(),
						linter(yamlLinter, { delay: 140 }),
						yamlHover,
						autocompletion({
							activateOnTyping: true,
							override: [composeCompletionSource]
						})
					);
				}
				break;
			case 'env':
				extensions.push(StreamLanguage.define(properties));
				if (!readOnly && !lightweight) {
					extensions.push(
						lintGutter(),
						linter(envLinter, { delay: 140 }),
						autocompletion({
							activateOnTyping: true,
							override: [envCompletionSource]
						})
					);
				}
				break;
		}

		return extensions;
	}

	const theme = $derived.by(() => {
		$configStore;
		return arcaneDarkInit();
	});

	const mergeEnterIndentKeymap = createMergeEnterIndentKeymap();
	const mergeHostAction = createMergeHostAction({
		getTheme: () => theme,
		mergeEnterIndentKeymap,
		getLanguageExtension,
		onValueChange: (nextValue) => {
			value = nextValue;
		},
		onPrimaryViewReady: (view) => {
			activeView = view;
			restoreEditorState(view);
			updateCursorSummary(view);
			markReadOnlyReady();
		}
	});

	const extensions = $derived([...getLanguageExtension(language), theme]);

	const styles = $derived({
		'&': {
			fontSize,
			height: autoHeight ? 'auto' : '100%'
		},
		'.cm-scroller': {
			overflow: autoHeight ? 'visible' : 'auto',
			maxHeight: autoHeight ? 'none' : '100%'
		},
		'&.cm-editor[contenteditable=false]': {
			cursor: 'not-allowed'
		},
		'.cm-content[contenteditable=false]': {
			cursor: 'not-allowed'
		}
	});

	function wireNormalView(view: EditorView) {
		normalView = view;
		if (!isDiffActive) {
			activeView = view;
			restoreEditorState(view);
			updateCursorSummary(view);
			markReadOnlyReady();
		}
	}

	function countCurrentDiagnostics(): number {
		if (!activeView) return 0;
		let count = 0;
		forEachDiagnostic(activeView.state, () => {
			count += 1;
		});
		return count;
	}
</script>

<div class="arcane-code-editor {autoHeight ? 'auto-height' : 'full-height'}">
	<div class="editor-main">
		{#if isDiffActive}
			<div class="merge-shell">
				<div class="merge-pane-header">
					<div class="merge-pane-label merge-pane-label-new">New (editable)</div>
					<div class="merge-pane-label merge-pane-label-old">Original (read-only)</div>
				</div>
				<div class="merge-legend">
					<span class="merge-badge merge-badge-add">+ Added</span>
					<span class="merge-badge merge-badge-del">- Removed</span>
				</div>
				<div class="merge-host" use:mergeHostAction={mergeActionParams}></div>
			</div>
		{:else}
			<CodeMirror bind:value {extensions} {styles} {placeholder} readonly={readOnly} nodebounce={true} onready={wireNormalView} />
		{/if}

		{#if outlineOpen && activeOutlineItems.length > 0}
			<div class="outline-panel">
				<div class="outline-title">Outline</div>
				<div class="outline-list">
					{#each activeOutlineItems as item (item.id)}
						<button type="button" class="outline-item level-{item.level}" onclick={() => jumpToOutlineItem(item)}>
							{item.label}
						</button>
					{/each}
				</div>
			</div>
		{/if}
	</div>

	<div class="editor-status">
		<span>{diagnosticSummary.errors} errors</span>
		<span>{diagnosticSummary.warnings} warnings</span>
		<span>Schema: {diagnosticSummary.schemaStatus}</span>
		<span>Unresolved vars: {diagnosticSummary.unresolvedVariables.length}</span>
		<span>Secrets: {diagnosticSummary.secretWarnings}</span>
		<span>Ln {diagnosticSummary.cursorLine}, Col {diagnosticSummary.cursorCol}</span>
		<span>Diagnostics: {countCurrentDiagnostics()}</span>
		{#if !validationReady}
			<span class="status-muted">Validating...</span>
		{/if}
	</div>

	<Command.Dialog bind:open={commandPaletteOpen} title="Editor Commands" description="Run editor commands">
		{#snippet children()}
			<Command.Input placeholder="Search editor commands..." />
			<Command.List>
				<Command.Empty>{m.common_no_results_found()}</Command.Empty>
				<Command.Group>
					{#each commandItems as item (item.id)}
						<Command.Item value={item.label} onSelect={() => executeCommand(item.id)}>
							<span class="flex-1">{item.label}</span>
							{#if item.shortcut}
								<Command.Shortcut>{item.shortcut}</Command.Shortcut>
							{/if}
						</Command.Item>
					{/each}
				</Command.Group>
			</Command.List>
		{/snippet}
	</Command.Dialog>
</div>

<style>
	:global(.arcane-code-editor.full-height) {
		height: 100%;
		display: flex;
		flex-direction: column;
		min-height: 0;
	}
	:global(.arcane-code-editor.auto-height) {
		height: auto;
		display: flex;
		flex-direction: column;
	}
	.editor-main {
		position: relative;
		flex: 1;
		min-height: 0;
	}
	:global(.arcane-code-editor.full-height .codemirror-wrapper) {
		height: 100%;
	}
	:global(.arcane-code-editor.full-height .cm-editor) {
		height: 100%;
	}
	:global(.arcane-code-editor.auto-height .codemirror-wrapper) {
		height: auto;
	}
	:global(.arcane-code-editor.auto-height .cm-editor) {
		height: auto;
		min-height: 120px;
	}
	:global(.arcane-code-editor.auto-height .cm-editor .cm-scroller) {
		overflow-y: visible;
	}
	:global(.arcane-code-editor .cm-editor .cm-scroller) {
		overflow-x: auto;
	}
	:global(.arcane-code-editor .cm-editor .cm-gutters) {
		background-color: #18181b;
		border-right: none;
	}
	:global(.arcane-code-editor .cm-editor .cm-activeLineGutter) {
		background-color: #2c313a;
		color: #e5e7eb;
	}
	:global(.arcane-code-editor .cm-mergeView) {
		height: 100%;
	}
	.merge-shell {
		height: 100%;
		min-height: 0;
		display: flex;
		flex-direction: column;
		background: color-mix(in oklab, var(--background) 96%, black 4%);
	}
	.merge-pane-header {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 0.5rem;
		padding: 0.35rem 0.5rem;
		border-bottom: 1px solid var(--border);
	}
	.merge-pane-label {
		font-size: 0.72rem;
		font-weight: 700;
		padding: 0.25rem 0.5rem;
		border-radius: 0.4rem;
		text-align: center;
	}
	.merge-pane-label-new {
		color: #3fb950;
		background: color-mix(in oklab, #3fb950 18%, transparent);
		border: 1px solid color-mix(in oklab, #3fb950 45%, transparent);
	}
	.merge-pane-label-old {
		color: #f85149;
		background: color-mix(in oklab, #f85149 18%, transparent);
		border: 1px solid color-mix(in oklab, #f85149 45%, transparent);
	}
	.merge-legend {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		padding: 0.3rem 0.5rem;
		border-bottom: 1px solid var(--border);
	}
	.merge-badge {
		font-size: 0.68rem;
		font-weight: 600;
		line-height: 1;
		padding: 0.22rem 0.45rem;
		border-radius: 999px;
	}
	.merge-badge-add {
		color: #3fb950;
		background: color-mix(in oklab, #3fb950 16%, transparent);
		border: 1px solid color-mix(in oklab, #3fb950 40%, transparent);
	}
	.merge-badge-del {
		color: #f85149;
		background: color-mix(in oklab, #f85149 16%, transparent);
		border: 1px solid color-mix(in oklab, #f85149 40%, transparent);
	}
	.merge-host {
		flex: 1;
		min-height: 0;
	}
	:global(.arcane-code-editor .merge-host .cm-mergeView) {
		height: 100%;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	:global(.arcane-code-editor .merge-host .cm-mergeViewEditors) {
		height: 100%;
		min-height: 0;
	}
	:global(.arcane-code-editor .merge-host .cm-mergeViewEditor) {
		height: 100%;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}
	:global(.arcane-code-editor .merge-host .cm-mergeViewEditor:first-child) {
		border-right: 1px solid var(--border);
	}
	:global(.arcane-code-editor .merge-host .cm-mergeViewEditor .cm-editor) {
		height: 100% !important;
		min-height: 0;
	}
	:global(.arcane-code-editor .merge-host .cm-mergeViewEditor .cm-scroller) {
		height: 100% !important;
		overflow: auto !important;
	}
	:global(.arcane-code-editor .merge-host .cm-merge-a .cm-changedLine),
	:global(.arcane-code-editor .merge-host .cm-merge-a .cm-inlineChangedLine) {
		background-color: rgba(46, 160, 67, 0.14) !important;
	}
	:global(.arcane-code-editor .merge-host .cm-merge-b .cm-changedLine),
	:global(.arcane-code-editor .merge-host .cm-merge-b .cm-inlineChangedLine),
	:global(.arcane-code-editor .merge-host .cm-deletedChunk) {
		background-color: rgba(248, 81, 73, 0.14) !important;
	}
	:global(.arcane-code-editor .merge-host .cm-merge-a .cm-changedText) {
		background: linear-gradient(rgba(46, 160, 67, 0.7), rgba(46, 160, 67, 0.7)) bottom/100% 2px no-repeat !important;
	}
	:global(.arcane-code-editor .merge-host .cm-merge-b .cm-changedText),
	:global(.arcane-code-editor .merge-host .cm-merge-b .cm-deletedText),
	:global(.arcane-code-editor .merge-host .cm-deletedChunk .cm-deletedText) {
		background: linear-gradient(rgba(248, 81, 73, 0.75), rgba(248, 81, 73, 0.75)) bottom/100% 2px no-repeat !important;
	}
	:global(.arcane-code-editor .merge-host .cm-merge-a .cm-changedLineGutter) {
		background-color: #2ea043 !important;
		color: #fff !important;
	}
	:global(.arcane-code-editor .merge-host .cm-merge-b .cm-changedLineGutter),
	:global(.arcane-code-editor .merge-host .cm-merge-b .cm-deletedLineGutter) {
		background-color: #f85149 !important;
		color: #fff !important;
	}
	.outline-panel {
		position: absolute;
		top: 0.5rem;
		right: 0.5rem;
		z-index: 20;
		width: 16rem;
		max-height: calc(100% - 1rem);
		overflow: hidden;
		border: 1px solid var(--border);
		border-radius: 0.5rem;
		background: color-mix(in oklab, var(--background) 95%, black 5%);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25);
	}
	.outline-title {
		padding: 0.5rem 0.75rem;
		font-size: 0.75rem;
		font-weight: 700;
		border-bottom: 1px solid var(--border);
	}
	.outline-list {
		max-height: 20rem;
		overflow: auto;
		padding: 0.25rem;
	}
	.outline-item {
		width: 100%;
		text-align: left;
		padding: 0.25rem 0.5rem;
		font-size: 0.75rem;
		border-radius: 0.375rem;
	}
	.outline-item:hover {
		background: color-mix(in oklab, var(--primary) 18%, transparent);
	}
	.outline-item.level-1 {
		padding-left: 1rem;
	}
	.editor-status {
		display: flex;
		gap: 0.75rem;
		align-items: center;
		padding: 0.25rem 0.5rem;
		font-size: 0.7rem;
		border-top: 1px solid var(--border);
		background: color-mix(in oklab, var(--background) 92%, black 8%);
		overflow-x: auto;
		white-space: nowrap;
	}
	.status-muted {
		opacity: 0.7;
	}
	:global(.arcane-hover) {
		max-width: 26rem;
		padding: 0.5rem 0.625rem;
		font-size: 0.75rem;
		line-height: 1.4;
		border-radius: 0.5rem;
		border: 1px solid var(--border);
		background: color-mix(in oklab, var(--background) 96%, black 4%);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
	}
</style>

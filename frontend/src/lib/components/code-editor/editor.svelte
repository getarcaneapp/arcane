<script lang="ts">
	import * as Command from '#lib/components/ui/command';
	import { browser } from '$app/env';
	import { m } from '#lib/paraglide/messages';
	import userStore from '#lib/stores/user-store';
	import { mode } from 'mode-watcher';
	import { untrack } from 'svelte';
	import type {
		EditorChangeEvent,
		File as PierreFile,
		FileDiff as PierreFileDiff,
		FileContents,
		TokenEventBase
	} from '@pierre/diffs';
	import type { Editor as PierreEditor, IStateStorage } from '@pierre/diffs/edit';
	import { createDefaultSummary, ENV_SNIPPETS, resolveSnippet, YAML_SNIPPETS } from './editor-constants';
	import { ARCANE_EDITOR_THEMES, registerArcaneEditorThemes } from './arcane-theme';
	import { analyzeEnvContent } from './analysis/env-analysis';
	import type { ComposeSchemaContext } from './analysis/compose-schema';
	import type {
		CodeLanguage,
		CodeValidationMode,
		CompletionItem,
		Diagnostic,
		DiagnosticAction,
		DiagnosticSummary,
		EditorContext,
		OutlineItem,
		SchemaDoc,
		SnippetItem
	} from './analysis/types';

	type ComposeAnalysisModule = typeof import('./analysis/compose-analysis');
	type ComposeSchemaModule = typeof import('./analysis/compose-schema');
	type FileInstance = PierreFile<undefined>;
	type DiffInstance = PierreFileDiff<undefined>;
	type EditorInstance = PierreEditor<undefined>;
	type Marker = Parameters<EditorInstance['setMarkers']>[0][number];
	type PopupItem = CompletionItem | (SnippetItem & { apply?: undefined; info?: undefined });
	type HoverData = { kind: 'variable'; name: string; source: string } | { kind: 'schema'; title: string; doc: SchemaDoc };

	let composeAnalysisModulePromise: Promise<ComposeAnalysisModule> | null = null;
	let composeSchemaModulePromise: Promise<ComposeSchemaModule> | null = null;

	function loadComposeAnalysisModule(): Promise<ComposeAnalysisModule> {
		composeAnalysisModulePromise ??= import('./analysis/compose-analysis');
		return composeAnalysisModulePromise;
	}

	function loadComposeSchemaModule(): Promise<ComposeSchemaModule> {
		composeSchemaModulePromise ??= import('./analysis/compose-schema');
		return composeSchemaModulePromise;
	}

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		validationMode,
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
		validationMode?: CodeValidationMode;
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
	void hasErrors;

	let activeOutlineItems = $state.raw<OutlineItem[]>([]);
	let currentDiagnostics = $state.raw<Diagnostic[]>([]);
	let shortcutsEnabled = $derived($userStore?.preferences?.keyboardShortcutsEnabled !== false);

	let completionOpen = $state(false);
	let completionItems = $state.raw<PopupItem[]>([]);
	let completionIndex = $state(0);
	let completionPos = $state({ left: 0, top: 0 });

	let hoverOpen = $state(false);
	let hoverData = $state.raw<HoverData | null>(null);
	let hoverPos = $state({ left: 0, top: 0 });

	// Non-reactive editor plumbing.
	let fileInstance: FileInstance | undefined;
	let diffInstance: DiffInstance | undefined;
	let editor: EditorInstance | undefined;
	let editorAttached = false;
	let fileHostEl: HTMLElement | undefined;
	let lastKnownValue = '';
	let completionFrom = 0;
	let completionSequence = 0;
	let analysisSequence = 0;
	let analysisTimer: ReturnType<typeof setTimeout> | undefined;
	let hoverSequence = 0;
	let schemaState: ComposeSchemaContext | null = null;

	const isDiffActive = $derived(Boolean(enableDiff && diffOpen && originalValue !== undefined));
	const effectiveValidationMode = $derived(validationMode ?? defaultValidationModeForLanguage(language));
	const effectiveEditorContext = $derived({
		envContent: editorContext?.envContent ?? '',
		composeContents: editorContext?.composeContents ?? [],
		globalVariables: editorContext?.globalVariables ?? {}
	});

	function defaultValidationModeForLanguage(lang: CodeLanguage): CodeValidationMode {
		switch (lang) {
			case 'yaml':
				return 'compose';
			case 'env':
				return 'env';
			case 'json':
			case 'toml':
			case 'dockerfile':
			case 'shell':
			case 'javascript':
			case 'typescript':
			case 'markdown':
			case 'plaintext':
				return 'none';
		}
	}

	const SHIKI_LANGUAGES: Record<CodeLanguage, string> = {
		yaml: 'yaml',
		env: 'dotenv',
		json: 'json',
		toml: 'toml',
		dockerfile: 'docker',
		shell: 'shellscript',
		javascript: 'javascript',
		typescript: 'typescript',
		markdown: 'markdown',
		plaintext: 'text'
	};

	const FILE_NAMES: Record<CodeLanguage, string> = {
		yaml: 'compose.yaml',
		env: '.env',
		json: 'file.json',
		toml: 'file.toml',
		dockerfile: 'Dockerfile',
		shell: 'file.sh',
		javascript: 'file.js',
		typescript: 'file.ts',
		markdown: 'file.md',
		plaintext: 'file.txt'
	};

	function buildFileContents(contents: string, lang: CodeLanguage, cacheKey?: string): FileContents {
		return {
			name: FILE_NAMES[lang],
			contents,
			lang: SHIKI_LANGUAGES[lang],
			cacheKey
		};
	}

	const sessionStateStorage: IStateStorage = {
		get(cacheKey) {
			if (!browser) return undefined;
			try {
				const raw = sessionStorage.getItem(`arcane.editor.state:${cacheKey}`);
				return raw ? JSON.parse(raw) : undefined;
			} catch {
				return undefined;
			}
		},
		set(cacheKey, state) {
			if (!browser) return;
			try {
				sessionStorage.setItem(`arcane.editor.state:${cacheKey}`, JSON.stringify(state));
			} catch {
				// ignore persistence errors
			}
		}
	};

	function buildLineIndex(text: string): number[] {
		const starts = [0];
		for (let index = 0; index < text.length; index += 1) {
			if (text.charCodeAt(index) === 10) starts.push(index + 1);
		}
		return starts;
	}

	function offsetToPosition(starts: number[], offset: number): { line: number; character: number } {
		let low = 0;
		let high = starts.length - 1;
		while (low < high) {
			const mid = (low + high + 1) >> 1;
			if ((starts[mid] ?? 0) <= offset) low = mid;
			else high = mid - 1;
		}
		return { line: low, character: offset - (starts[low] ?? 0) };
	}

	function positionToOffset(starts: number[], position: { line: number; character: number }): number {
		const line = Math.max(0, Math.min(position.line, starts.length - 1));
		return (starts[line] ?? 0) + position.character;
	}

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

	function markValidationNotRequiredReady() {
		if (!readOnly && effectiveValidationMode !== 'none') return;
		hasErrors = false;
		validationReady = true;
		activeOutlineItems = [];
		updateSummary({
			errors: 0,
			warnings: 0,
			infos: 0,
			hints: 0,
			validationReady: true
		});
	}

	function getPrimarySelection(): {
		start: { line: number; character: number };
		end: { line: number; character: number };
	} | null {
		if (!editor || !editorAttached) return null;
		const selections = editor.getState().selections;
		if (!selections || selections.length === 0) return null;
		return selections[selections.length - 1] ?? null;
	}

	function scheduleAnalysis(source: string) {
		if (analysisTimer !== undefined) clearTimeout(analysisTimer);
		analysisTimer = setTimeout(() => {
			analysisTimer = undefined;
			void runAnalysis(source);
		}, 140);
	}

	async function runAnalysis(source: string) {
		if (readOnly || (effectiveValidationMode !== 'compose' && effectiveValidationMode !== 'env')) return;
		const sequence = ++analysisSequence;

		let diagnostics: Diagnostic[] = [];
		let outlineItems: OutlineItem[] = [];
		let patch: Partial<DiagnosticSummary> = {};

		if (effectiveValidationMode === 'compose') {
			if (language !== 'yaml') return;
			const [schemaModule, analysisModule] = await Promise.all([loadComposeSchemaModule(), loadComposeAnalysisModule()]);
			const schema = await schemaModule.getComposeSchemaContext();
			if (sequence !== analysisSequence) return;
			schemaState = schema;
			const analysis = await analysisModule.analyzeComposeContent(source, schema, effectiveEditorContext);
			if (sequence !== analysisSequence) return;
			diagnostics = analysis.diagnostics;
			outlineItems = analysis.outlineItems;
			patch = { schemaStatus: schema.status, schemaMessage: schema.message, ...analysis.summaryPatch };
		} else {
			const analysis = analyzeEnvContent(source, effectiveEditorContext);
			diagnostics = analysis.diagnostics;
			outlineItems = analysis.outlineItems;
			patch = {
				schemaStatus: schemaState?.status ?? 'unavailable',
				schemaMessage: schemaState?.message,
				...analysis.summaryPatch
			};
		}

		currentDiagnostics = diagnostics;
		activeOutlineItems = outlineItems;
		updateSummaryFromDiagnostics(diagnostics, patch);
		applyMarkers(source, diagnostics);
	}

	function applyDiagnosticAction(action: DiagnosticAction) {
		if (!editor || !editorAttached) return;
		const starts = buildLineIndex(editor.getText());
		editor.applyEdits(
			action.edits.map((edit) => ({
				range: { start: offsetToPosition(starts, edit.from), end: offsetToPosition(starts, edit.to) },
				newText: edit.insert
			}))
		);
	}

	function buildMarkerMessage(diagnostic: Diagnostic): string | HTMLElement {
		if (!diagnostic.actions || diagnostic.actions.length === 0) return diagnostic.message;
		const container = document.createElement('div');
		const message = document.createElement('div');
		message.textContent = diagnostic.message;
		container.appendChild(message);
		for (const action of diagnostic.actions) {
			const button = document.createElement('button');
			button.type = 'button';
			button.textContent = action.name;
			button.style.cssText =
				'margin-top:4px;padding:2px 8px;font-size:11px;border:1px solid currentColor;border-radius:4px;cursor:pointer;background:transparent;color:inherit;';
			button.addEventListener('click', () => applyDiagnosticAction(action));
			container.appendChild(button);
		}
		return container;
	}

	function applyMarkers(source: string, diagnostics: Diagnostic[]) {
		if (!editor || !editorAttached) return;
		const starts = buildLineIndex(source);
		const markers: Marker[] = diagnostics.map((diagnostic) => ({
			severity: diagnostic.severity,
			message: buildMarkerMessage(diagnostic),
			start: offsetToPosition(starts, diagnostic.from),
			end: offsetToPosition(starts, diagnostic.to),
			source: 'arcane'
		}));
		try {
			editor.setMarkers(markers);
		} catch {
			// setMarkers throws when the editor is not attached yet
		}
	}

	function closeCompletion() {
		completionSequence += 1;
		if (completionOpen) {
			completionOpen = false;
			completionItems = [];
			completionIndex = 0;
		}
	}

	function getCaretRect(): DOMRect | null {
		const container = fileHostEl?.querySelector('diffs-container');
		const carets = container?.shadowRoot?.querySelectorAll('[data-caret]');
		if (!carets || carets.length === 0) return null;
		return carets[carets.length - 1]?.getBoundingClientRect() ?? null;
	}

	async function openCompletion(explicit: boolean) {
		if (!editor || !editorAttached || readOnly || isDiffActive) return closeCompletion();
		if (effectiveValidationMode !== 'compose' && effectiveValidationMode !== 'env') return closeCompletion();

		const selection = getPrimarySelection();
		if (!selection || selection.start.line !== selection.end.line || selection.start.character !== selection.end.character) {
			return closeCompletion();
		}

		const sequence = ++completionSequence;
		const source = editor.getText();
		const starts = buildLineIndex(source);
		const caret = positionToOffset(starts, selection.end);
		const lineText = source.slice(starts[selection.end.line] ?? 0, caret);
		const matchRegex = effectiveValidationMode === 'env' ? /[A-Za-z0-9_.-]*$/ : /[\w.-]*$/;
		const prefix = matchRegex.exec(lineText)?.[0] ?? '';
		if (!explicit && prefix.length === 0) return closeCompletion();

		let options: PopupItem[] = [];

		if (effectiveValidationMode === 'compose') {
			const analysisModule = await loadComposeAnalysisModule();
			const yamlContext = analysisModule.findYamlPositionContext(source, caret);
			if (sequence !== completionSequence) return;
			if (!yamlContext) return closeCompletion();

			const schemaModule = await loadComposeSchemaModule();
			const schema = await schemaModule.getComposeSchemaContext();
			if (sequence !== completionSequence) return;
			schemaState = schema;
			updateSummary({ schemaStatus: schema.status, schemaMessage: schema.message });
			if (!schema.schema) return closeCompletion();

			if (yamlContext.atKey) {
				options = [...YAML_SNIPPETS, ...schemaModule.getCompletionOptionsForPath(schema.schema, yamlContext.parentPath, prefix)];
			} else {
				options = schemaModule.getEnumValueCompletions(schema.schema, yamlContext.path);
			}
		} else {
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
				.map((key) => ({ label: key, type: 'variable', apply: `${key}=` }) satisfies CompletionItem);

			options = [...ENV_SNIPPETS, ...variableOptions];
		}

		const normalizedPrefix = prefix.toLowerCase();
		options = options.filter((item) => item.label.toLowerCase().includes(normalizedPrefix));
		if (options.length === 0) return closeCompletion();

		const rect = getCaretRect();
		if (!rect) return closeCompletion();

		completionFrom = caret - prefix.length;
		completionItems = options;
		completionIndex = 0;
		completionPos = {
			left: rect.left,
			top: rect.bottom + 200 > window.innerHeight ? rect.top - 4 : rect.bottom + 4
		};
		completionOpen = true;
	}

	function acceptCompletion(item: PopupItem) {
		if (!editor || !editorAttached) return closeCompletion();
		const selection = getPrimarySelection();
		if (!selection) return closeCompletion();

		const source = editor.getText();
		const starts = buildLineIndex(source);
		const caret = positionToOffset(starts, selection.end);

		let text: string;
		let selectFrom: number | undefined;
		let selectTo: number | undefined;
		if ('insert' in item && item.insert !== undefined) {
			const resolved = resolveSnippet(item.insert);
			text = resolved.text;
			selectFrom = resolved.selectFrom;
			selectTo = resolved.selectTo;
		} else {
			text = item.apply ?? item.label;
		}

		editor.applyEdits([
			{
				range: { start: offsetToPosition(starts, completionFrom), end: offsetToPosition(starts, caret) },
				newText: text
			}
		]);

		if (selectFrom !== undefined && selectTo !== undefined) {
			const nextStarts = buildLineIndex(editor.getText());
			editor.setSelections([
				{
					start: offsetToPosition(nextStarts, completionFrom + selectFrom),
					end: offsetToPosition(nextStarts, completionFrom + selectTo),
					direction: 'forward'
				}
			]);
		}

		closeCompletion();
	}

	function closeHover() {
		hoverSequence += 1;
		if (hoverOpen) {
			hoverOpen = false;
			hoverData = null;
		}
	}

	async function handleTokenEnter(props: TokenEventBase) {
		if (readOnly || language !== 'yaml' || effectiveValidationMode !== 'compose') return;
		const sequence = ++hoverSequence;

		// Delay so quick pointer passes over keys don't flash the popup;
		// leaving the token bumps hoverSequence and cancels the pending show.
		await new Promise((resolve) => setTimeout(resolve, 450));
		if (sequence !== hoverSequence) return;
		const source = editor && editorAttached ? editor.getText() : lastKnownValue;
		const starts = buildLineIndex(source);
		if (props.lineNumber < 1 || props.lineNumber > starts.length) return;
		const offset = (starts[props.lineNumber - 1] ?? 0) + props.lineCharStart;

		const analysisModule = await loadComposeAnalysisModule();
		if (sequence !== hoverSequence) return;

		const variableRef = analysisModule.resolveVariableSourceAtPosition(source, offset, effectiveEditorContext);
		let data: HoverData | null = null;
		if (variableRef) {
			data = { kind: 'variable', name: variableRef.name, source: variableRef.source };
		} else {
			const yamlContext = analysisModule.findYamlPositionContext(source, offset);
			if (yamlContext?.currentKey) {
				const schemaModule = await loadComposeSchemaModule();
				const schema = schemaState ?? (await schemaModule.getComposeSchemaContext());
				if (sequence !== hoverSequence) return;
				schemaState = schema;
				if (schema.schema) {
					const doc = schemaModule.getSchemaDocForPath(schema.schema, yamlContext.path);
					if (doc) data = { kind: 'schema', title: doc.title ?? yamlContext.currentKey, doc };
				}
			}
		}

		if (sequence !== hoverSequence || !data) return;
		const rect = props.tokenElement.getBoundingClientRect();
		hoverPos = {
			left: rect.left,
			top: rect.bottom + 160 > window.innerHeight ? rect.top - 4 : rect.bottom + 4
		};
		hoverData = data;
		hoverOpen = true;
	}

	function jumpToOutlineItem(item: OutlineItem) {
		const source = editor && editorAttached ? editor.getText() : lastKnownValue;
		const starts = buildLineIndex(source);
		const position = offsetToPosition(starts, item.from);
		if (editor && editorAttached) {
			editor.focus({ lineNumber: position.line + 1, character: position.character });
		} else {
			scrollToLineReadOnly(position.line + 1);
		}
	}

	function scrollToLineReadOnly(lineNumber: number) {
		const container = fileHostEl?.querySelector('diffs-container');
		container?.shadowRoot?.querySelector(`[data-line="${lineNumber}"]`)?.scrollIntoView({ block: 'center' });
	}

	function gotoDiagnostic(direction: 1 | -1) {
		if (!editor || !editorAttached || currentDiagnostics.length === 0) return;
		const source = editor.getText();
		const starts = buildLineIndex(source);
		const selection = getPrimarySelection();
		const caret = selection ? positionToOffset(starts, selection.end) : 0;
		const sorted = [...currentDiagnostics].sort((a, b) => a.from - b.from);

		let target: Diagnostic | undefined;
		if (direction === 1) {
			target = sorted.find((diagnostic) => diagnostic.from > caret) ?? sorted[0];
		} else {
			target = [...sorted].reverse().find((diagnostic) => diagnostic.from < caret) ?? sorted[sorted.length - 1];
		}
		if (!target) return;

		const position = offsetToPosition(starts, target.from);
		editor.focus({ lineNumber: position.line + 1, character: position.character });
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

	async function formatDocument() {
		if (!editor || !editorAttached || readOnly) return;
		const current = editor.getText();
		let formatted = current;

		if (language === 'yaml') {
			const { parseDocument } = await import('yaml');
			const parsed = parseDocument(current, { strict: false, uniqueKeys: false });
			if (parsed.errors.length === 0) {
				formatted = parsed.toString({ indent: 2, lineWidth: 0 });
			}
		} else if (language === 'env') {
			formatted = formatEnvContent(current);
		}

		if (formatted === current) return;

		const starts = buildLineIndex(current);
		const lastLine = starts.length - 1;
		editor.applyEdits([
			{
				range: {
					start: { line: 0, character: 0 },
					end: { line: lastLine, character: current.length - (starts[lastLine] ?? 0) }
				},
				newText: formatted
			}
		]);
	}

	function goToLine() {
		const raw = window.prompt('Go to line', String(diagnosticSummary.cursorLine));
		if (!raw) return;
		const lineNumber = Number.parseInt(raw, 10);
		if (Number.isNaN(lineNumber)) return;

		if (editor && editorAttached) {
			const lineCount = buildLineIndex(editor.getText()).length;
			editor.focus({ lineNumber: Math.max(1, Math.min(lineNumber, lineCount)) });
		} else {
			scrollToLineReadOnly(Math.max(1, lineNumber));
		}
	}

	function executeCommand(id: string) {
		commandPaletteOpen = false;
		switch (id) {
			case 'format':
				void formatDocument();
				break;
			case 'next-diagnostic':
				gotoDiagnostic(1);
				break;
			case 'prev-diagnostic':
				gotoDiagnostic(-1);
				break;
			case 'toggle-outline':
				outlineOpen = !outlineOpen;
				break;
			case 'toggle-diff':
				if (enableDiff && originalValue !== undefined) {
					diffOpen = !diffOpen;
				}
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
			{ id: 'jump-line', label: 'Jump to line' }
		];

		if (enableDiff && originalValue !== undefined) {
			items.splice(4, 0, { id: 'toggle-diff', label: diffOpen ? 'Hide diff' : 'Show diff' });
		}

		return items;
	});

	function handleYamlEnter(event: KeyboardEvent): boolean {
		if (language !== 'yaml' || !editor || !editorAttached || readOnly) return false;
		const selections = editor.getState().selections;
		if (!selections || selections.length !== 1) return false;
		const selection = selections[0];
		if (!selection) return false;
		if (selection.start.line !== selection.end.line || selection.start.character !== selection.end.character) return false;

		const source = editor.getText();
		const starts = buildLineIndex(source);
		const caret = positionToOffset(starts, selection.end);
		const line = selection.end.line;
		const lineEnd = line + 1 < starts.length ? (starts[line + 1] ?? 0) - 1 : source.length;
		if (caret !== lineEnd) return false;

		const lineText = source.slice(starts[line] ?? 0, lineEnd);
		const trimmed = lineText.trimEnd();
		const startsYamlBlock =
			/:\s*(?:#.*)?$/.test(trimmed) || /:\s*[|>][-+0-9]*\s*(?:#.*)?$/.test(trimmed) || /^-\s*(?:#.*)?$/.test(trimmed.trimStart());
		if (!startsYamlBlock) return false;

		const indentation = (lineText.match(/^\s*/)?.[0] ?? '') + '  ';
		event.preventDefault();
		event.stopPropagation();
		editor.applyEdits([{ range: { start: selection.end, end: selection.end }, newText: `\n${indentation}` }]);
		editor.setSelections([
			{
				start: { line: line + 1, character: indentation.length },
				end: { line: line + 1, character: indentation.length },
				direction: 'none'
			}
		]);
		// The programmatic edit replaces the row DOM; without an explicit focus
		// the contentEditable drops focus and further typing goes nowhere.
		editor.focus({ preventScroll: true });
		return true;
	}

	function handleKeydownCapture(event: KeyboardEvent) {
		if (event.isComposing) return;

		if (completionOpen) {
			switch (event.key) {
				case 'ArrowDown':
					completionIndex = (completionIndex + 1) % completionItems.length;
					event.preventDefault();
					event.stopPropagation();
					return;
				case 'ArrowUp':
					completionIndex = (completionIndex - 1 + completionItems.length) % completionItems.length;
					event.preventDefault();
					event.stopPropagation();
					return;
				case 'Enter':
				case 'Tab': {
					const item = completionItems[completionIndex];
					if (item) {
						event.preventDefault();
						event.stopPropagation();
						acceptCompletion(item);
					}
					return;
				}
				case 'Escape':
					event.preventDefault();
					event.stopPropagation();
					closeCompletion();
					return;
			}
		}

		if (hoverOpen) closeHover();

		const mod = event.metaKey || event.ctrlKey;
		if (event.key === 'F8' && !mod && !event.altKey) {
			event.preventDefault();
			event.stopPropagation();
			gotoDiagnostic(event.shiftKey ? -1 : 1);
			return;
		}
		if (event.key.toLowerCase() === 'f' && event.shiftKey && event.altKey && !mod) {
			event.preventDefault();
			event.stopPropagation();
			void formatDocument();
			return;
		}
		if (event.key.toLowerCase() === 'p' && mod && event.shiftKey) {
			if (shortcutsEnabled) {
				event.preventDefault();
				event.stopPropagation();
				commandPaletteOpen = true;
			}
			return;
		}
		if (event.key === ' ' && event.ctrlKey && !event.metaKey && !event.altKey) {
			event.preventDefault();
			event.stopPropagation();
			void openCompletion(true);
			return;
		}
		if (event.key === 'Enter' && !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
			handleYamlEnter(event);
		}
	}

	function captureKeys(node: HTMLElement) {
		node.addEventListener('keydown', handleKeydownCapture, true);
		return () => node.removeEventListener('keydown', handleKeydownCapture, true);
	}

	function handleEditorChange(file: FileContents, event: EditorChangeEvent<undefined>) {
		lastKnownValue = file.contents;
		value = file.contents;
		scheduleAnalysis(file.contents);

		// Refresh an open popup on any change; otherwise only short word-ish
		// insertions (typing) trigger it, so programmatic edits stay quiet.
		const inserted = event.changes[event.changes.length - 1]?.text ?? '';
		if (completionOpen || (inserted.length > 0 && inserted.length <= 2 && /[\w.$-]/.test(inserted.slice(-1)))) {
			void openCompletion(false);
		}
	}

	function handleSelectionChange() {
		if (!editor || !editorAttached) return;
		const selection = getPrimarySelection();
		if (!selection) return;
		updateSummary({
			cursorLine: selection.end.line + 1,
			cursorCol: selection.end.character + 1
		});
	}

	function fileEditorAttachment(host: HTMLElement) {
		const lang = language;
		const ro = readOnly;
		const key = fileId;
		const context = { cancelled: false };
		const cleanups: Array<() => void> = [];

		fileHostEl = host;
		void (async () => {
			const { File } = await import('@pierre/diffs');
			if (context.cancelled) return;
			registerArcaneEditorThemes();

			const initialValue = value;
			lastKnownValue = initialValue;

			const instance = new File({
				theme: ARCANE_EDITOR_THEMES,
				themeType: mode.current === 'dark' ? 'dark' : 'light',
				disableFileHeader: true,
				overflow: 'scroll',
				onTokenEnter: (props) => void handleTokenEnter(props),
				onTokenLeave: () => closeHover()
			});
			fileInstance = instance;
			cleanups.push(() => {
				instance.cleanUp();
				if (fileInstance === instance) fileInstance = undefined;
			});
			instance.render({ file: buildFileContents(initialValue, lang, key), containerWrapper: host });

			if (ro) {
				markReadOnlyReady();
				markValidationNotRequiredReady();
				if (key) {
					try {
						const saved = sessionStorage.getItem(`arcane.editor.state:${key}:scroll`);
						if (saved) requestAnimationFrame(() => (host.scrollTop = Number(saved) || 0));
					} catch {
						// ignore bad state payload
					}
					const onScroll = () => {
						try {
							sessionStorage.setItem(`arcane.editor.state:${key}:scroll`, String(host.scrollTop));
						} catch {
							// ignore persistence errors
						}
					};
					host.addEventListener('scroll', onScroll, { passive: true });
					cleanups.push(() => host.removeEventListener('scroll', onScroll));
				}
				return;
			}

			const { Editor } = await import('@pierre/diffs/edit');
			if (context.cancelled) return;

			const editorInstance: EditorInstance = new Editor({
				persistState: Boolean(key),
				persistStateStorage: sessionStateStorage,
				onAttach: () => {
					editorAttached = true;
					handleSelectionChange();
					markValidationNotRequiredReady();
					scheduleAnalysis(editorInstance.getText());
				},
				onChange: (file, _annotations, event) => handleEditorChange(file, event),
				onBlur: () => closeCompletion()
			});
			editor = editorInstance;
			const dispose = editorInstance.edit(instance);
			cleanups.push(() => {
				editorAttached = false;
				dispose();
				editorInstance.cleanUp();
				if (editor === editorInstance) editor = undefined;
			});
		})();

		return () => {
			context.cancelled = true;
			closeCompletion();
			closeHover();
			if (analysisTimer !== undefined) {
				clearTimeout(analysisTimer);
				analysisTimer = undefined;
			}
			for (const cleanup of cleanups.reverse()) cleanup();
			if (fileHostEl === host) fileHostEl = undefined;
		};
	}

	function diffAttachment(host: HTMLElement) {
		const lang = language;
		const current = value;
		const baseline = originalValue ?? '';
		const context = { cancelled: false };

		void (async () => {
			const { FileDiff } = await import('@pierre/diffs');
			if (context.cancelled) return;
			registerArcaneEditorThemes();

			const instance = new FileDiff({
				theme: ARCANE_EDITOR_THEMES,
				themeType: mode.current === 'dark' ? 'dark' : 'light',
				disableFileHeader: true,
				overflow: 'scroll',
				diffStyle: 'split'
			});
			diffInstance = instance;
			instance.render({
				oldFile: buildFileContents(baseline, lang),
				newFile: buildFileContents(current, lang),
				containerWrapper: host
			});
			markReadOnlyReady();
			markValidationNotRequiredReady();
		})();

		return () => {
			context.cancelled = true;
			diffInstance?.cleanUp();
			diffInstance = undefined;
		};
	}

	// Keep the rendered theme in sync with the app theme.
	$effect(() => {
		const themeType = mode.current === 'dark' ? 'dark' : 'light';
		untrack(() => {
			fileInstance?.setThemeType(themeType);
			diffInstance?.setThemeType(themeType);
		});
	});

	// Push external value changes into the mounted editor or viewer.
	$effect(() => {
		const next = value;
		untrack(() => {
			if (next === lastKnownValue) return;
			lastKnownValue = next;
			if (editor && editorAttached) {
				const current = editor.getText();
				if (current === next) return;
				const starts = buildLineIndex(current);
				const lastLine = starts.length - 1;
				editor.applyEdits([
					{
						range: {
							start: { line: 0, character: 0 },
							end: { line: lastLine, character: current.length - (starts[lastLine] ?? 0) }
						},
						newText: next
					}
				]);
			} else if (fileInstance && fileHostEl) {
				fileInstance.render({ file: buildFileContents(next, language, fileId), containerWrapper: fileHostEl });
			}
		});
	});
</script>

<svelte:document onselectionchange={handleSelectionChange} />

<div
	class="arcane-code-editor {autoHeight ? 'auto-height' : 'full-height'}"
	style:--diffs-font-size={fontSize}
	{@attach captureKeys}
>
	<div class="editor-main">
		{#if isDiffActive}
			<div class="diff-hint">
				<span class="diff-badge diff-badge-add">+ Added</span>
				<span class="diff-badge diff-badge-del">- Removed</span>
				<span class="diff-hint-label">Read-only preview</span>
			</div>
			<div class="editor-scroll diff-host" {@attach diffAttachment}></div>
		{:else}
			<div class="editor-scroll file-host" {@attach fileEditorAttachment}></div>
			{#if placeholder && !value}
				<div class="editor-placeholder">{placeholder}</div>
			{/if}
		{/if}

		{#if completionOpen}
			<div
				class="completion-popup"
				style="left: {completionPos.left}px; top: {completionPos.top}px"
				role="listbox"
				tabindex={-1}
				onpointerdown={(event) => event.preventDefault()}
			>
				{#each completionItems as item, index (`${item.type ?? ''}:${item.label}`)}
					<button
						type="button"
						class={['completion-item', index === completionIndex && 'selected']}
						onclick={() => acceptCompletion(item)}
						onpointerenter={() => (completionIndex = index)}
					>
						<span class="completion-label">{item.label}</span>
						{#if item.detail}
							<span class="completion-detail">{item.detail}</span>
						{/if}
					</button>
				{/each}
			</div>
		{/if}

		{#if hoverOpen && hoverData}
			<div class="arcane-hover" style="left: {hoverPos.left}px; top: {hoverPos.top}px">
				{#if hoverData.kind === 'variable'}
					<strong>{hoverData.name}</strong>
					<div>Source: {hoverData.source}</div>
				{:else}
					<div><strong>{hoverData.title}</strong></div>
					{#if hoverData.doc.description}
						<div>{hoverData.doc.description}</div>
					{/if}
					{#if hoverData.doc.defaultValue}
						<div><strong>Default:</strong> {hoverData.doc.defaultValue}</div>
					{/if}
					{#if hoverData.doc.examples && hoverData.doc.examples.length > 0}
						<div><strong>Examples:</strong> {hoverData.doc.examples.join(', ')}</div>
					{/if}
				{/if}
			</div>
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
		<span>{diagnosticSummary.errors} {m.editor_errors()}</span>
		<span>{diagnosticSummary.warnings} {m.editor_warnings()}</span>
		<span>{m.editor_schema()}: {diagnosticSummary.schemaStatus}</span>
		<span>{m.editor_line()} {diagnosticSummary.cursorLine}, {m.editor_column()} {diagnosticSummary.cursorCol}</span>
		<span>{m.diagnostics()}: {currentDiagnostics.length}</span>
		{#if !validationReady}
			<span class="status-muted">{m.editor_validating()}</span>
		{/if}
	</div>

	<Command.Dialog bind:open={commandPaletteOpen} title={m.editor_commands()} description={m.editor_commands_desc()}>
		{#snippet children()}
			<Command.Input placeholder={m.editor_search_commands()} />
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
	.arcane-code-editor {
		--diffs-font-family: 'Mona Sans Mono', ui-monospace, SFMono-Regular, Menlo, monospace;
		--diffs-bg-caret-override: var(--primary);
		--diffs-bg-selection-override: color-mix(in oklab, var(--primary) 28%, transparent);
	}
	.editor-main {
		position: relative;
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
	}
	.editor-scroll {
		flex: 1;
		min-height: 0;
		/* Match the Pierre theme editor background so the panel stays filled
		   below short documents (the rendered pre only spans its content). */
		background: #ffffff;
	}
	:global(.dark) .editor-scroll {
		background: #0a0a0a;
	}
	:global(.arcane-code-editor.full-height .editor-scroll) {
		height: 100%;
		overflow: auto;
	}
	:global(.arcane-code-editor.auto-height .editor-scroll) {
		height: auto;
		min-height: 120px;
	}
	.editor-placeholder {
		position: absolute;
		top: 0.35rem;
		left: 3.5rem;
		font-size: 0.75rem;
		font-family: var(--diffs-font-family);
		opacity: 0.5;
		pointer-events: none;
	}
	.diff-hint {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		padding: 0.3rem 0.5rem;
		border-bottom: 1px solid var(--border);
	}
	.diff-hint-label {
		font-size: 0.68rem;
		opacity: 0.7;
	}
	.diff-badge {
		font-size: 0.68rem;
		font-weight: 600;
		line-height: 1;
		padding: 0.22rem 0.45rem;
		border-radius: 999px;
	}
	.diff-badge-add {
		color: #3fb950;
		background: color-mix(in oklab, #3fb950 16%, transparent);
		border: 1px solid color-mix(in oklab, #3fb950 40%, transparent);
	}
	.diff-badge-del {
		color: #f85149;
		background: color-mix(in oklab, #f85149 16%, transparent);
		border: 1px solid color-mix(in oklab, #f85149 40%, transparent);
	}
	.completion-popup {
		position: fixed;
		z-index: var(--arcane-z-popover, 50);
		min-width: 14rem;
		max-width: 24rem;
		max-height: 16rem;
		overflow: auto;
		padding: 0.25rem;
		border: 1px solid var(--border);
		border-radius: 0.5rem;
		background: color-mix(in oklab, var(--background) 96%, black 4%);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.25);
	}
	.completion-item {
		display: flex;
		gap: 0.5rem;
		align-items: baseline;
		width: 100%;
		text-align: left;
		padding: 0.25rem 0.5rem;
		font-size: 0.75rem;
		border-radius: 0.375rem;
	}
	.completion-item.selected {
		background: color-mix(in oklab, var(--primary) 18%, transparent);
	}
	.completion-label {
		font-family: var(--diffs-font-family);
	}
	.completion-detail {
		font-size: 0.68rem;
		opacity: 0.65;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.outline-panel {
		position: absolute;
		top: 0.5rem;
		right: 0.5rem;
		z-index: var(--arcane-z-sticky);
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
	.arcane-hover {
		position: fixed;
		z-index: var(--arcane-z-popover, 50);
		max-width: 26rem;
		padding: 0.5rem 0.625rem;
		font-size: 0.75rem;
		line-height: 1.4;
		pointer-events: none;
		border-radius: 0.5rem;
		border: 1px solid var(--border);
		background: color-mix(in oklab, var(--background) 96%, black 4%);
		box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
	}
</style>

<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { Compartment, EditorState, type Extension } from '@codemirror/state';
	import { EditorView, keymap, type ViewUpdate } from '@codemirror/view';
	import { defaultKeymap, indentWithTab, historyKeymap } from '@codemirror/commands';
	import { history } from '@codemirror/history';
	import { StreamLanguage } from '@codemirror/language';
	import { linter, lintGutter, type Diagnostic, type LintSource } from '@codemirror/lint';
	import { yaml } from '@codemirror/lang-yaml';
	import { properties } from '@codemirror/legacy-modes/mode/properties';
	import jsyaml from 'js-yaml';
	import { arcaneCodeMirrorTheme } from './theme';

	type CodeLanguage = 'yaml' | 'env';

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		readOnly = false,
		fontSize = '13px',
		autoHeight = false
	}: {
		value: string;
		language: CodeLanguage;
		readOnly?: boolean;
		fontSize?: string;
		autoHeight?: boolean;
	} = $props();

	let container = $state<HTMLDivElement>();
	let view = $state.raw<EditorView | null>(null);

	const languageCompartment = new Compartment();
	const readOnlyCompartment = new Compartment();
	const themeCompartment = new Compartment();

	const yamlLinter: LintSource = (view): Diagnostic[] => {
		const diagnostics: Diagnostic[] = [];
		try {
			jsyaml.load(view.state.doc.toString());
		} catch (e: unknown) {
			const err = e as { mark?: { position: number }; message?: string };
			const start = err.mark?.position || 0;
			const end = err.mark?.position !== undefined ? Math.max(start + 1, err.mark.position + 1) : start + 1;
			diagnostics.push({
				from: start,
				to: end,
				severity: 'error',
				message: err.message || 'YAML error'
			});
		}
		return diagnostics;
	};

	function getLanguageExtensions(lang: CodeLanguage): Extension[] {
		if (lang === 'yaml') {
			const exts: Extension[] = [yaml()];
			if (!readOnly) {
				exts.push(lintGutter(), linter(yamlLinter));
			}
			return exts;
		}
		// Best-effort .env highlighting (key/value)
		return [StreamLanguage.define(properties)];
	}

	function getThemeExtensions(size: string) {
		return EditorView.theme({
			'&': {
				fontSize: size
			},
			'.cm-content': {
				padding: '12px'
			},
			'&.cm-editor[contenteditable=false]': {
				cursor: 'not-allowed'
			},
			'.cm-content[contenteditable=false]': {
				cursor: 'not-allowed'
			},
			'.cm-scroller': {
				overflow: 'auto'
			}
		});
	}

	function updateAutoHeight() {
		if (!view || !container || !autoHeight) return;
		const scroller = view.dom.querySelector('.cm-scroller') as HTMLElement | null;
		if (!scroller) return;
		container.style.height = `${scroller.scrollHeight}px`;
	}

	onMount(() => {
		if (!container) return;

		// Match prior behavior: compute theme from CSS variables at runtime.
		const theme = arcaneCodeMirrorTheme();

		const initialState = EditorState.create({
			doc: value,
			extensions: [
				history(),
				keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
				EditorView.lineWrapping,
				languageCompartment.of(getLanguageExtensions(language)),
				readOnlyCompartment.of([EditorView.editable.of(!readOnly), EditorState.readOnly.of(readOnly)]),
				themeCompartment.of(getThemeExtensions(fontSize)),
				theme,
				EditorView.updateListener.of((update: ViewUpdate) => {
					if (update.docChanged) {
						value = update.state.doc.toString();
					}
					if (autoHeight) {
						requestAnimationFrame(updateAutoHeight);
					}
				})
			]
		});

		view = new EditorView({
			state: initialState,
			parent: container
		});

		if (autoHeight) {
			requestAnimationFrame(updateAutoHeight);
		}
	});

	onDestroy(() => {
		view?.destroy();
		view = null;
	});

	$effect(() => {
		if (!view) return;
		view.dispatch({
			effects: languageCompartment.reconfigure(getLanguageExtensions(language))
		});
	});

	$effect(() => {
		if (!view) return;
		view.dispatch({
			effects: readOnlyCompartment.reconfigure([EditorView.editable.of(!readOnly), EditorState.readOnly.of(readOnly)])
		});
	});

	$effect(() => {
		if (!view) return;
		view.dispatch({
			effects: themeCompartment.reconfigure(getThemeExtensions(fontSize))
		});
	});

	$effect(() => {
		if (!view) return;
		const current = view.state.doc.toString();
		if (value === current) return;
		view.dispatch({
			changes: { from: 0, to: current.length, insert: value }
		});
	});

	$effect(() => {
		if (!container) return;
		if (!autoHeight) {
			container.style.height = '';
			return;
		}
		requestAnimationFrame(updateAutoHeight);
	});
</script>

<div
	bind:this={container}
	class="relative min-h-0 w-full overflow-hidden {autoHeight ? '' : 'h-full'}"
></div>

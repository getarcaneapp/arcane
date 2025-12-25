<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { monaco, initShiki } from './monaco';
	import { mode } from 'mode-watcher';
	import jsyaml from 'js-yaml';

	type CodeLanguage = 'yaml' | 'env';

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		readOnly = false,
		fontSize = '12px',
		fileUri = undefined,
		autoHeight = false
	}: {
		value: string;
		language: CodeLanguage;
		readOnly?: boolean;
		fontSize?: string;
		fileUri?: string;
		autoHeight?: boolean;
	} = $props();

	let editorElement = $state<HTMLDivElement>();
	let editor = $state.raw<monaco.editor.IStandaloneCodeEditor | null>(null);
	let model = $state.raw<monaco.editor.ITextModel | null>(null);
	let ownsModel = $state(false);
	let resizeObserver: ResizeObserver | null = null;
	let changeDisposable: monaco.IDisposable | null = null;

	const langId = $derived(language === 'env' ? 'ini' : language);
	const theme = $derived(mode.current === 'dark' ? 'catppuccin-mocha' : 'catppuccin-latte');

	const markers = $derived.by(() => {
		if (!model || language !== 'yaml' || readOnly) return [];

		try {
			jsyaml.load(value);
			return [];
		} catch (e: unknown) {
			const err = e as { mark?: { line: number; column: number }; reason?: string; message?: string };
			const mark = err.mark;

			if (mark) {
				const lineCount = model.getLineCount();
				const lineNumber = Math.min(Math.max(1, mark.line + 1), lineCount);
				const maxColumn = model.getLineMaxColumn(lineNumber);

				return [
					{
						severity: monaco.MarkerSeverity.Error,
						message: err.reason || err.message || 'YAML error',
						startLineNumber: lineNumber,
						startColumn: Math.min(mark.column + 1, maxColumn),
						endLineNumber: lineNumber,
						endColumn: maxColumn
					}
				];
			}
			return [
				{
					severity: monaco.MarkerSeverity.Error,
					message: err.message || 'YAML error',
					startLineNumber: 1,
					startColumn: 1,
					endLineNumber: 1,
					endColumn: 1
				}
			];
		}
	});

	function updateHeight() {
		if (!editor || !editorElement || !autoHeight) return;
		const contentHeight = editor.getContentHeight();
		editorElement.style.height = `${contentHeight}px`;
		editor.layout();
	}

	onMount(async () => {
		if (!editorElement) return;

		await initShiki(monaco);

		// Wait for container to be properly sized
		await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));

		// Create or get model with proper URI for LSP features
		const uri = fileUri ? monaco.Uri.parse(fileUri) : monaco.Uri.parse(`inmemory://model-${Date.now()}.${langId}`);
		const existingModel = monaco.editor.getModel(uri);
		ownsModel = !existingModel;
		model = existingModel || monaco.editor.createModel(value, langId, uri);

		editor = monaco.editor.create(editorElement, {
			model: model,
			automaticLayout: false,
			theme,
			readOnly,
			fontSize: parseInt(fontSize.replace('px', '')),
			minimap: { enabled: false },
			scrollBeyondLastLine: false,
			fontFamily:
				'"Geist Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
			padding: { top: 10, bottom: 10 },
			scrollbar: autoHeight
				? {
						vertical: 'hidden',
						handleMouseWheel: false
					}
				: undefined
		});

		changeDisposable = model.onDidChangeContent(() => {
			value = model?.getValue() || '';
		});

		resizeObserver = new ResizeObserver(() => {
			requestAnimationFrame(() => {
				editor?.layout();
			});
		});
		resizeObserver.observe(editorElement);
	});

	onDestroy(() => {
		changeDisposable?.dispose();
		resizeObserver?.disconnect();
		editor?.dispose();
		if (ownsModel) model?.dispose();
	});

	// Sync value to model
	$effect(() => {
		if (model && value !== model.getValue()) {
			model.setValue(value);
		}
	});

	// Sync language
	$effect(() => {
		if (model) {
			monaco.editor.setModelLanguage(model, langId);
		}
	});

	// Sync markers
	$effect(() => {
		if (model) {
			monaco.editor.setModelMarkers(model, 'yaml-linter', markers);
		}
	});

	// Sync options and layout
	$effect(() => {
		if (editor) {
			editor.updateOptions({
				readOnly,
				theme,
				fontSize: parseInt(fontSize.replace('px', '')),
				scrollbar: autoHeight
					? {
							vertical: 'hidden',
							handleMouseWheel: false
						}
					: {
							vertical: 'auto',
							handleMouseWheel: true
						}
			});
			editor.layout();
		}
	});

	// Auto-height handling
	$effect(() => {
		if (editor && autoHeight) {
			const disposable = editor.onDidContentSizeChange(updateHeight);
			updateHeight();
			return () => disposable.dispose();
		} else if (editorElement) {
			editorElement.style.height = '';
		}
	});

	// Global theme sync
	$effect(() => {
		monaco.editor.setTheme(theme);
	});
</script>

<div class="relative {autoHeight ? '' : 'h-full'} min-h-0 w-full overflow-visible" bind:this={editorElement}></div>

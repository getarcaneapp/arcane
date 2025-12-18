<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { monaco, initShiki } from './monaco';
	import { mode } from 'mode-watcher';
	import jsyaml from 'js-yaml';

	function getCurrentTheme(): string {
		const isDark = mode.current === 'dark';
		return isDark ? 'catppuccin-mocha' : 'catppuccin-latte';
	}

	type CodeLanguage = 'yaml' | 'env';

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		readOnly = false,
		fontSize = '12px',
		fileUri = undefined
	}: {
		value: string;
		language: CodeLanguage;
		readOnly?: boolean;
		fontSize?: string;
		fileUri?: string;
	} = $props();

	let editorElement: HTMLDivElement;
	let editor: monaco.editor.IStandaloneCodeEditor;
	let resizeObserver: ResizeObserver | null = null;
	let model: monaco.editor.ITextModel | null = null;
	let ownsModel = false;

	function validateYaml() {
		if (!model || language !== 'yaml' || readOnly) {
			if (model) monaco.editor.setModelMarkers(model, 'yaml-linter', []);
			return;
		}

		const content = model.getValue();
		try {
			jsyaml.load(content);
			monaco.editor.setModelMarkers(model, 'yaml-linter', []);
		} catch (e: unknown) {
			const markers: monaco.editor.IMarkerData[] = [];
			const err = e as { mark?: { line: number; column: number }; reason?: string; message?: string };
			const mark = err.mark;

			if (mark) {
				markers.push({
					severity: monaco.MarkerSeverity.Error,
					message: err.reason || err.message || 'YAML error',
					startLineNumber: mark.line + 1,
					startColumn: mark.column + 1,
					endLineNumber: mark.line + 1,
					endColumn: model.getLineMaxColumn(mark.line + 1)
				});
			} else {
				markers.push({
					severity: monaco.MarkerSeverity.Error,
					message: err.message || 'YAML error',
					startLineNumber: 1,
					startColumn: 1,
					endLineNumber: 1,
					endColumn: 1
				});
			}
			monaco.editor.setModelMarkers(model, 'yaml-linter', markers);
		}
	}

	onMount(async () => {
		const langId = language === 'env' ? 'ini' : language;

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
			automaticLayout: true,
			theme: getCurrentTheme(),
			readOnly: readOnly,
			fontSize: parseInt(fontSize.replace('px', '')),
			minimap: { enabled: false },
			scrollBeyondLastLine: false,
			fontFamily:
				'"Geist Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
			padding: { top: 10, bottom: 10 }
		});

		model.onDidChangeContent(() => {
			value = model?.getValue() || '';
			validateYaml();
		});

		resizeObserver = new ResizeObserver(() => {
			editor?.layout();
		});
		resizeObserver.observe(editorElement);

		// Force layout update
		editor.layout();
		validateYaml();
	});

	$effect(() => {
		if (model && value !== model.getValue()) {
			model.setValue(value);
		}
	});

	$effect(() => {
		if (model) {
			const langId = language === 'env' ? 'ini' : language;
			monaco.editor.setModelLanguage(model, langId);
			validateYaml();
		}
	});

	$effect(() => {
		if (editor) {
			editor.updateOptions({ readOnly });
			validateYaml();
		}
	});

	// Watch for theme changes and update globally
	$effect(() => {
		monaco.editor.setTheme(getCurrentTheme());
	});

	onDestroy(() => {
		resizeObserver?.disconnect();
		resizeObserver = null;
		editor?.dispose();
		if (ownsModel) model?.dispose();
	});
</script>

<div class="relative h-full min-h-0 w-full overflow-visible" bind:this={editorElement}></div>

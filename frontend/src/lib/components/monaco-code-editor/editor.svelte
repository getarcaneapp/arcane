<script lang="ts">
	import { onDestroy, onMount } from 'svelte';
	import { monaco } from './monaco';
	import { mode } from 'mode-watcher';

	function getCurrentTheme(): string {
		return mode.current === 'dark' ? 'dracula' : 'github-light';
	}

	type CodeLanguage = 'yaml' | 'env';

	let {
		value = $bindable(''),
		language = 'yaml' as CodeLanguage,
		placeholder = '',
		readOnly = false,
		fontSize = '12px',
		fileUri = undefined
	}: {
		value: string;
		language: CodeLanguage;
		placeholder?: string;
		readOnly?: boolean;
		fontSize?: string;
		fileUri?: string;
	} = $props();

	let editorElement: HTMLDivElement;
	let editor: monaco.editor.IStandaloneCodeEditor;
	let resizeObserver: ResizeObserver | null = null;
	let model: monaco.editor.ITextModel | null = null;

	onMount(async () => {
		const langId = language === 'env' ? 'ini' : language;

		// Small delay to ensure container is sized
		await new Promise((resolve) => setTimeout(resolve, 50));

		// Create or get model with proper URI for LSP features
		const uri = fileUri ? monaco.Uri.parse(fileUri) : monaco.Uri.parse(`inmemory://model-${Date.now()}.${langId}`);
		model = monaco.editor.getModel(uri) || monaco.editor.createModel(value, langId, uri);

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
		});

		resizeObserver = new ResizeObserver(() => {
			editor?.layout();
		});
		resizeObserver.observe(editorElement);

		// Force layout update
		editor.layout();
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
		}
	});

	$effect(() => {
		if (editor) {
			editor.updateOptions({ readOnly });
		}
	});

	// Watch for theme changes and update globally
	$effect(() => {
		const themeName = mode.current === 'dark' ? 'dracula' : 'github-light';
		monaco.editor.setTheme(themeName);
	});

	onDestroy(() => {
		resizeObserver?.disconnect();
		resizeObserver = null;
		editor?.dispose();
		// Note: Don't dispose the model as it may be reused
	});
</script>

<div class="relative h-full min-h-0 w-full overflow-visible" bind:this={editorElement}></div>

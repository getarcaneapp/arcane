<script lang="ts">
	import { onMount } from 'svelte';
	import * as Card from '$lib/components/ui/card';
	import CodeEditor from '$lib/components/monaco-code-editor/editor.svelte';
	import MobileCodeEditor from '$lib/components/codemirror-code-editor/editor.svelte';
	import { CodeIcon } from '$lib/icons';
	import { IsMobile } from '$lib/hooks/is-mobile.svelte.js';

	type CodeLanguage = 'yaml' | 'env';

	let {
		title,
		open = $bindable(),
		language,
		value = $bindable(),
		error,
		autoHeight = false
	}: {
		title: string;
		open: boolean;
		language: CodeLanguage;
		value: string;
		error?: string;
		autoHeight?: boolean;
	} = $props();

	const isMobile = new IsMobile();
	let isTouchDevice = $state(false);
	const effectiveAutoHeight = $derived(autoHeight || isMobile.current);
	const useMobileEditor = $derived(isMobile.current || isTouchDevice);

	onMount(() => {
		isTouchDevice =
			(typeof navigator !== 'undefined' && navigator.maxTouchPoints > 0) ||
			(typeof window !== 'undefined' && 'ontouchstart' in window);
	});
</script>

<Card.Root class="flex {effectiveAutoHeight ? '' : 'flex-1'} min-h-0 flex-col overflow-hidden">
	<Card.Header icon={CodeIcon} class="flex-shrink-0 items-center">
		<Card.Title>
			<h2>{title}</h2>
		</Card.Title>
	</Card.Header>
	<Card.Content class="relative z-0 flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col overflow-visible p-0">
		<div class="{effectiveAutoHeight ? '' : 'relative flex-1'} min-h-0 w-full min-w-0">
			{#if useMobileEditor}
				<MobileCodeEditor bind:value {language} fontSize="13px" autoHeight={effectiveAutoHeight} />
			{:else if effectiveAutoHeight}
				<CodeEditor bind:value {language} fontSize="13px" autoHeight={true} />
			{:else}
				<div class="absolute inset-0">
					<CodeEditor bind:value {language} fontSize="13px" />
				</div>
			{/if}
		</div>
		{#if error}
			<p class="text-destructive px-4 py-2 text-xs">{error}</p>
		{/if}
	</Card.Content>
</Card.Root>

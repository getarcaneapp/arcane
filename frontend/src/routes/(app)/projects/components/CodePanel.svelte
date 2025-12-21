<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import CodeEditor from '$lib/components/monaco-code-editor/editor.svelte';
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
	const effectiveAutoHeight = $derived(autoHeight || isMobile.current);
</script>

<Card.Root class="flex {effectiveAutoHeight ? '' : 'flex-1'} min-h-0 flex-col overflow-hidden">
	<Card.Header icon={CodeIcon} class="flex-shrink-0 items-center">
		<Card.Title>
			<h2>{title}</h2>
		</Card.Title>
	</Card.Header>
	<Card.Content class="relative z-0 flex min-h-0 {effectiveAutoHeight ? '' : 'flex-1'} flex-col overflow-visible p-0">
		<div class="{effectiveAutoHeight ? '' : 'relative flex-1'} min-h-0 w-full min-w-0">
			{#if effectiveAutoHeight}
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

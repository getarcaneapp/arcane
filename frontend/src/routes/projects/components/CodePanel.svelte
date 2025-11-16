<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import CodeEditor from '$lib/components/code-editor/editor.svelte';
	import FileTextIcon from '@lucide/svelte/icons/file-text';

	type CodeLanguage = 'yaml' | 'env';

	let {
		title,
		open = $bindable(),
		language,
		value = $bindable(),
		placeholder,
		error
	}: {
		title: string;
		open: boolean;
		language: CodeLanguage;
		value: string;
		placeholder?: string;
		error?: string;
	} = $props();
</script>

<Card.Root class="flex h-full flex-col">
	<Card.Header icon={FileTextIcon} class="flex-shrink-0 items-center">
		<Card.Title>
			<h2>
				{title}
			</h2>
		</Card.Title>
	</Card.Header>
	<Card.Content class="flex-grow p-0">
		<div class="h-full [&_.cm-content]:text-xs sm:[&_.cm-content]:text-sm">
			<CodeEditor bind:value {language} {placeholder} />
			{#if error}
				<p class="text-destructive mt-2 text-xs">{error}</p>
			{/if}
		</div>
	</Card.Content>
</Card.Root>

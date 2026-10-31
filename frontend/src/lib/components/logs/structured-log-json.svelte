<script lang="ts">
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { useHighlightedCode } from '#lib/hooks/use-highlighted-code.svelte.js';

	interface Props {
		json: string;
	}

	let { json }: Props = $props();

	const lines = useHighlightedCode(
		() => json,
		() => 'json'
	);
</script>

<CodeBlock.Root size="sm">
	<CodeBlock.Content wrap>
		{#each lines.current as line, index (index)}
			<CodeBlock.Line code={line} />
		{/each}
	</CodeBlock.Content>
</CodeBlock.Root>

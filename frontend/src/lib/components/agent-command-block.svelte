<script lang="ts">
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { useHighlightedCode } from '#lib/hooks/use-highlighted-code.svelte.js';

	interface Props {
		dockerRunLabel: string;
		dockerRun: string;
		dockerComposeLabel: string;
		dockerCompose: string;
		preClass?: string;
	}

	let { dockerRunLabel, dockerRun, dockerComposeLabel, dockerCompose, preClass = '' }: Props = $props();

	const dockerRunLines = useHighlightedCode(
		() => dockerRun,
		() => 'shell'
	);
	const dockerComposeLines = useHighlightedCode(
		() => dockerCompose,
		() => 'yaml'
	);
</script>

<CodeBlock.Root size="sm">
	<CodeBlock.Header>
		<CodeBlock.Title>{dockerRunLabel}</CodeBlock.Title>
		<CodeBlock.Actions>
			<CopyButton text={dockerRun} size="icon" class="size-7" />
		</CodeBlock.Actions>
	</CodeBlock.Header>
	<CodeBlock.Content class={preClass}>
		{#each dockerRunLines.current as line, index (index)}
			<CodeBlock.Line code={line} />
		{/each}
	</CodeBlock.Content>
</CodeBlock.Root>

<CodeBlock.Root size="sm">
	<CodeBlock.Header>
		<CodeBlock.Title>{dockerComposeLabel}</CodeBlock.Title>
		<CodeBlock.Actions>
			<CopyButton text={dockerCompose} size="icon" class="size-7" />
		</CodeBlock.Actions>
	</CodeBlock.Header>
	<CodeBlock.Content class={preClass}>
		{#each dockerComposeLines.current as line, index (index)}
			<CodeBlock.Line code={line} />
		{/each}
	</CodeBlock.Content>
</CodeBlock.Root>

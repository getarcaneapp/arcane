<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as CodeBlock from '#lib/components/ui/code-block/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { useHighlightedCode } from '#lib/hooks/use-highlighted-code.svelte.js';
	import { CodeIcon, EyeOffIcon, EyeOnIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ContainerDetails } from '#lib/types/container.js';
	import { isSensitiveEnvKey } from '#lib/utils/formatting.js';

	interface Props {
		container: ContainerDetails;
	}

	let { container }: Props = $props();

	let showSecrets = $state(false);

	const maskedEnv = $derived(
		(container.config?.env ?? []).map((env) => {
			const separator = env.indexOf('=');
			if (separator === -1) return env;
			const key = env.slice(0, separator);
			return isSensitiveEnvKey(key, env.slice(separator + 1)) ? `${key}=********` : env;
		})
	);
	const hasSensitive = $derived(maskedEnv.some((env, index) => env !== container.config?.env?.[index]));
	const json = $derived(
		JSON.stringify(
			hasSensitive && !showSecrets ? { ...container, config: { ...container.config, env: maskedEnv } } : container,
			null,
			2
		)
	);
	const jsonLines = useHighlightedCode(
		() => json,
		() => 'json'
	);
</script>

<Card.Root>
	<Card.Header icon={CodeIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.containers_inspect_title()}</h2>
			</Card.Title>
			<Card.Description>{m.containers_inspect_description()}</Card.Description>
		</div>
		<div class="ml-auto flex gap-2">
			{#if hasSensitive}
				<ArcaneButton
					action="base"
					icon={showSecrets ? EyeOffIcon : EyeOnIcon}
					customLabel={showSecrets ? m.common_hide() : m.common_show()}
					onclick={() => (showSecrets = !showSecrets)}
				/>
			{/if}
			<CopyButton text={json} variant="outline" size="default">
				{m.common_copy_json()}
			</CopyButton>
		</div>
	</Card.Header>
	<CodeBlock.Root size="sm" variant="ghost">
		<CodeBlock.Content>
			{#each jsonLines.current as line, index (index)}
				<CodeBlock.Line code={line} />
			{/each}
		</CodeBlock.Content>
	</CodeBlock.Root>
</Card.Root>

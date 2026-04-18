<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/container.type';
	import { CodeIcon } from '$lib/icons';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();

	const json = $derived(JSON.stringify(container, null, 2));
</script>

<Card.Root>
	<Card.Header icon={CodeIcon}>
		<div class="flex flex-col space-y-1.5">
			<Card.Title>
				<h2>{m.containers_inspect_title()}</h2>
			</Card.Title>
			<Card.Description>{m.containers_inspect_description()}</Card.Description>
		</div>
		<div class="ml-auto">
			<CopyButton text={json} variant="outline" size="default">
				{m.common_copy_json()}
			</CopyButton>
		</div>
	</Card.Header>
	<Card.Content class="p-0">
		<pre class="bg-muted/40 overflow-auto rounded-b-lg p-4 font-mono text-xs leading-relaxed"><code>{json}</code></pre>
	</Card.Content>
</Card.Root>

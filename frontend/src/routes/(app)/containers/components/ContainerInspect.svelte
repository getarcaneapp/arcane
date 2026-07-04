<script lang="ts">
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerDetailsDto } from '$lib/types/docker';
	import { CodeIcon } from '$lib/icons';

	interface Props {
		container: ContainerDetailsDto;
	}

	let { container }: Props = $props();

	const json = $derived(JSON.stringify(container, null, 2));
</script>

<div class="border-border/70 overflow-hidden rounded-xl border">
	<div class="border-border/50 flex items-start justify-between gap-3 border-b p-4">
		<div class="flex items-start gap-2">
			<CodeIcon class="text-primary mt-0.5 size-4 shrink-0" />
			<div>
				<h2 class="text-sm font-semibold">{m.containers_inspect_title()}</h2>
				<p class="text-muted-foreground text-xs">{m.containers_inspect_description()}</p>
			</div>
		</div>
		<CopyButton text={json} variant="outline" size="default">
			{m.common_copy_json()}
		</CopyButton>
	</div>
	<pre class="bg-muted/40 overflow-auto p-4 font-mono text-xs leading-relaxed"><code>{json}</code></pre>
</div>

<script lang="ts">
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { FolderXIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let { class: className, unstyled = false }: { class?: string; unstyled?: boolean } = $props();
</script>

<Empty.Root variant={unstyled ? 'plain' : 'card'} class={className} role="status" aria-live="polite">
	<Empty.Header>
		<Empty.Media variant="icon">
			<FolderXIcon class="size-10 text-muted-foreground/60" />
		</Empty.Media>
		<Empty.Title><span class="font-semibold">{m.common_no_results_found()}</span></Empty.Title>
		<Empty.Description>{m.common_no_results_hint()}</Empty.Description>
	</Empty.Header>
</Empty.Root>

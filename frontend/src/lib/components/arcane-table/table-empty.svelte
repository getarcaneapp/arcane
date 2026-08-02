<script lang="ts">
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { FolderXIcon } from '#lib/icons';
	import { m } from '#lib/paraglide/messages';

	let { class: className }: { class?: string } = $props();
</script>

<Empty.Root class={className} role="status" aria-live="polite">
	<Empty.Header>
		<Empty.Media variant="icon">
			<FolderXIcon class="size-10 text-muted-foreground/60" />
		</Empty.Media>
		<Empty.Title class="text-lg font-semibold">{m.common_no_results_found()}</Empty.Title>
		<Empty.Description class="text-sm text-muted-foreground">{m.common_no_results_hint()}</Empty.Description>
	</Empty.Header>
</Empty.Root>

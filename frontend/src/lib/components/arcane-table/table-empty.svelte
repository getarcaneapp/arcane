<script lang="ts">
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { FolderXIcon, ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';

	let { onClear }: { onClear?: () => void } = $props();
</script>

<Empty.Root variant="plain" class="h-full" role="status" aria-live="polite">
	<Empty.Header>
		<Empty.Media variant="icon">
			<FolderXIcon class="size-10 text-muted-foreground/60" />
		</Empty.Media>
		<Empty.Title><span class="font-semibold">{m.common_no_results_found()}</span></Empty.Title>
		<Empty.Description>{m.common_no_results_hint()}</Empty.Description>
	</Empty.Header>
	{#if onClear}
		<Empty.Content>
			<ArcaneButton
				action="base"
				tone="outline"
				size="sm"
				icon={ResetIcon}
				customLabel={m.common_clear_filters()}
				onclick={onClear}
			/>
		</Empty.Content>
	{/if}
</Empty.Root>

<script lang="ts">
	import * as Empty from '#lib/components/ui/empty/index.js';
	import { FolderXIcon } from '#lib/icons';
	import { ArcaneButton } from '#lib/components/arcane-button';
	import type { TableEmptyState } from './arcane-table.types.svelte';
	import { m } from '#lib/paraglide/messages';

	let { class: className, state }: { class?: string; state?: TableEmptyState } = $props();
</script>

<Empty.Root class={className} role="status" aria-live="polite">
	<Empty.Header>
		<Empty.Media variant="icon">
			<FolderXIcon class="size-10 text-muted-foreground/60" />
		</Empty.Media>
		<Empty.Title class="text-lg font-semibold">{state?.title ?? m.common_no_results_found()}</Empty.Title>
		<Empty.Description class="text-sm text-muted-foreground">{state?.description ?? m.no_items_description()}</Empty.Description>
	</Empty.Header>
	{#if state?.action}
		<Empty.Content>
			<ArcaneButton
				action="base"
				tone="outline"
				customLabel={state.action.label}
				href={state.action.href}
				onclick={state.action.onclick}
			/>
		</Empty.Content>
	{/if}
</Empty.Root>

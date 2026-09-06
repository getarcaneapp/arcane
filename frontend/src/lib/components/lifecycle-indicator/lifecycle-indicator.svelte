<script lang="ts">
	import { CodeIcon } from '#lib/icons/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { cn } from '#lib/utils.js';

	let {
		scriptPath,
		class: className = ''
	}: {
		scriptPath: string | null | undefined;
		class?: string;
	} = $props();

	const trimmedPath = $derived(typeof scriptPath === 'string' ? scriptPath.trim() : '');
	const tooltipText = $derived(trimmedPath ? m.lifecycle_indicator_tooltip({ path: trimmedPath }) : '');
</script>

{#if trimmedPath}
	<ArcaneTooltip.Root>
		<ArcaneTooltip.Trigger>
			<span class={cn('inline-flex items-center text-muted-foreground', className)} aria-label={tooltipText}>
				<CodeIcon class="size-3.5" />
			</span>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>{tooltipText}</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/if}

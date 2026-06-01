<script lang="ts">
	import { CodeIcon } from '$lib/icons';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';

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
			<span class={cn('text-muted-foreground inline-flex items-center', className)} aria-label={tooltipText}>
				<CodeIcon class="size-3.5" />
			</span>
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>{tooltipText}</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{/if}

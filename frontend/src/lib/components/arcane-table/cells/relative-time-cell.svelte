<script lang="ts">
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip';
	import { formatDateTime, formatRelativeTime, parseInstant } from '#lib/utils/formatting';
	import { m } from '#lib/paraglide/messages';

	let { value }: { value: unknown } = $props();

	const instant = $derived(value ? parseInstant(String(value)) : null);
</script>

{#if instant}
	<ArcaneTooltip.Root>
		<!-- The child snippet renders a plain span: the default trigger wrapper is
		     interactive and would swallow the table's row-expand click. -->
		<ArcaneTooltip.Trigger>
			{#snippet child({ props })}
				<span {...props} class="text-sm whitespace-nowrap">{formatRelativeTime(instant)}</span>
			{/snippet}
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>{formatDateTime(instant, { includeSeconds: true })}</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{:else}
	<span class="text-sm text-muted-foreground">{m.common_na()}</span>
{/if}

<script lang="ts">
	import { formatDistanceToNow } from 'date-fns';
	import * as ArcaneTooltip from '$lib/components/arcane-tooltip';
	import { formatDateTime } from '$lib/utils/formatting';
	import { m } from '$lib/paraglide/messages';

	let { value }: { value: unknown } = $props();

	const date = $derived(value ? new Date(String(value)) : null);
	const valid = $derived(!!date && !Number.isNaN(date.getTime()));
</script>

{#if valid && date}
	<ArcaneTooltip.Root>
		<!-- The child snippet renders a plain span: the default trigger wrapper is
		     interactive and would swallow the table's row-expand click. -->
		<ArcaneTooltip.Trigger>
			{#snippet child({ props })}
				<span {...props} class="text-sm whitespace-nowrap">{formatDistanceToNow(date, { addSuffix: true })}</span>
			{/snippet}
		</ArcaneTooltip.Trigger>
		<ArcaneTooltip.Content>{formatDateTime(date, { includeSeconds: true })}</ArcaneTooltip.Content>
	</ArcaneTooltip.Root>
{:else}
	<span class="text-muted-foreground text-sm">{m.common_na()}</span>
{/if}

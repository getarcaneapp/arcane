<script lang="ts">
	import { CopyButton } from '#lib/components/ui/copy-button';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import type { Event } from '#lib/types/shared';
	import { m } from '#lib/paraglide/messages';
	import { flattenMetadata, stringifyForDisplay } from './event-metadata';

	let { event }: { event: Event } = $props();

	let showRawEvent = $state(false);

	const eventJson = $derived(JSON.stringify(event, null, 2));
	const metadataEntries = $derived(flattenMetadata(event.metadata ?? {}));
	const hasMetadata = $derived(metadataEntries.length > 0);
	const eventErrorMessage = $derived.by(() => {
		const metadataError = event.metadata?.['error'];
		if (typeof metadataError === 'string' && metadataError.trim() !== '') {
			return metadataError;
		}
		if (metadataError !== undefined && metadataError !== null) {
			return stringifyForDisplay(metadataError);
		}
		if (event.severity === 'error' && event.description) {
			return event.description;
		}
		return null;
	});
</script>

<div class="flex min-w-0 flex-col gap-4">
	<div class="flex flex-col gap-4">
		{#if eventErrorMessage}
			<div class="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm break-words text-destructive">
				{eventErrorMessage}
			</div>
		{/if}

		{#if event.resourceId || event.resourceName}
			<div class="grid gap-3 sm:grid-cols-2">
				{#if event.resourceId}
					{@render resourceCell(m.events_resource_id_label(), event.resourceId, m.events_copy_resource_id_title())}
				{/if}
				{#if event.resourceName}
					{@render resourceCell(m.events_resource_name_label(), event.resourceName, m.events_copy_resource_name_title())}
				{/if}
			</div>
		{/if}
	</div>

	<div class="min-w-0 border-t border-border/60">
		<div class="flex flex-wrap items-center justify-between gap-2 py-2.5">
			<span class="text-sm font-semibold">{m.events_metadata_title()}</span>
			<div class="flex items-center gap-2">
				<CopyButton
					text={eventJson}
					variant="ghost"
					size="default"
					class="h-8 px-2 text-xs"
					title={m.events_copy_full_event_json_title()}
				>
					<span class="text-xs">{m.common_copy_json()}</span>
				</CopyButton>
				<ArcaneButton
					action="base"
					tone="ghost"
					size="sm"
					customLabel={`${showRawEvent ? m.common_hide() : m.common_show()} ${m.common_raw()}`}
					onclick={() => (showRawEvent = !showRawEvent)}
				/>
			</div>
		</div>
		{#if showRawEvent}
			<pre class="max-h-80 overflow-auto border-t border-border/50 bg-muted/40 p-4 text-xs leading-relaxed"><code
					class="font-mono">{eventJson}</code
				></pre>
		{:else if hasMetadata}
			<div class="max-h-80 overflow-auto border-t border-border/50">
				{#each metadataEntries as entry (entry.key)}
					<div
						class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] items-start gap-3 border-b border-border/50 py-2 last:border-b-0 sm:grid-cols-[minmax(0,260px)_minmax(0,1fr)]"
					>
						<div class="font-mono text-xs break-all text-muted-foreground">{entry.key}</div>
						<pre class="font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{entry.value}</pre>
					</div>
				{/each}
			</div>
		{:else}
			<div class="border-t border-border/50 py-3 text-xs text-muted-foreground">{m.events_no_metadata_provided()}</div>
		{/if}
	</div>
</div>

{#snippet resourceCell(label: string, value: string, copyTitle: string)}
	<div class="min-w-0">
		<div class="text-xs text-muted-foreground">{label}</div>
		<div class="mt-1 flex items-center justify-between gap-2">
			<div class="min-w-0 font-mono text-sm break-all">{value}</div>
			<CopyButton text={value} size="icon" class="size-7 shrink-0" title={copyTitle} />
		</div>
	</div>
{/snippet}

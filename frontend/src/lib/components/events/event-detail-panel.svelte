<script lang="ts">
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import type { Event } from '$lib/types/shared';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
	import { AlertIcon, InfoIcon } from '$lib/icons';
	import { formatDateTime } from '$lib/utils/formatting';
	import { eventSeverityIconVariant } from './events-labels';
	import { flattenMetadata, stringifyForDisplay } from './event-metadata';

	let { event }: { event: Event } = $props();

	let showRawEvent = $state(false);

	const eventJson = $derived(JSON.stringify(event, null, 2));
	const metadataEntries = $derived(flattenMetadata(event.metadata ?? {}));
	const hasMetadata = $derived(metadataEntries.length > 0);
	const environmentName = $derived.by(() => {
		if (!event.environmentId) {
			return null;
		}
		const matchedEnvironment = environmentStore.available.find((env) => env.id === event.environmentId);
		if (matchedEnvironment) {
			return matchedEnvironment.name;
		}
		if (event.environmentId === LOCAL_DOCKER_ENVIRONMENT_ID) {
			return m.environments_local_badge();
		}
		return event.environmentId;
	});
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

	const severityIconClasses: Record<ReturnType<typeof eventSeverityIconVariant>, string> = {
		emerald: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
		amber: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
		red: 'bg-red-500/10 text-red-600 dark:text-red-400',
		blue: 'bg-blue-500/10 text-blue-600 dark:text-blue-400'
	};
	const severityIconClass = $derived(severityIconClasses[eventSeverityIconVariant(event.severity)]);
</script>

<div class="border-border/60 bg-background overflow-hidden rounded-lg border shadow-sm">
	<div class="space-y-4 px-5 py-4">
		<div class="flex min-w-0 items-start justify-between gap-4">
			<div class="flex min-w-0 items-start gap-3">
				<div class={['flex size-9 shrink-0 items-center justify-center rounded-md', severityIconClass]}>
					{#if event.severity === 'info'}
						<InfoIcon class="size-4.5" aria-hidden="true" />
					{:else}
						<AlertIcon class="size-4.5" aria-hidden="true" />
					{/if}
				</div>
				<div class="min-w-0">
					<h3 class="truncate text-sm font-semibold" title={event.title}>{event.title}</h3>
					{#if event.description}
						<p class="text-muted-foreground mt-0.5 text-sm">{event.description}</p>
					{/if}
				</div>
			</div>
			<CopyButton
				text={eventJson}
				variant="ghost"
				size="default"
				class="h-8 shrink-0 px-2 text-xs"
				title={m.events_copy_full_event_json_title()}
			>
				<span class="text-xs">{m.common_copy_json()}</span>
			</CopyButton>
		</div>

		<div class="text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
			<span class="text-foreground font-medium tabular-nums">{formatDateTime(event.timestamp, { includeSeconds: true })}</span>
			{#if environmentName}
				<span class="text-border">•</span>
				<div class="flex items-center gap-1.5">
					<span>{m.events_environment_label()}</span>
					<span class="text-foreground font-medium">{environmentName}</span>
				</div>
			{/if}
			<span class="text-border">•</span>
			<div class="flex items-center gap-1.5">
				<span>{m.common_user()}</span>
				{#if (event.username ?? 'System') === 'System'}
					<span class="italic">System</span>
				{:else}
					<span class="text-foreground font-medium">{event.username}</span>
				{/if}
			</div>
			<span class="text-border">•</span>
			<span class="font-mono">{event.type}</span>
		</div>

		{#if eventErrorMessage}
			<div class="border-destructive/30 bg-destructive/10 text-destructive rounded-md border p-3 text-sm break-words">
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

	<div class="border-border/60 border-t">
		<div class="flex items-center justify-between px-5 py-2.5">
			<span class="text-sm font-semibold">{m.events_metadata_title()}</span>
			<ArcaneButton
				action="base"
				tone="ghost"
				size="sm"
				customLabel={`${showRawEvent ? m.common_hide() : m.common_show()} ${m.common_raw()}`}
				onclick={() => (showRawEvent = !showRawEvent)}
			/>
		</div>
		{#if showRawEvent}
			<pre class="bg-muted/40 border-border/50 max-h-80 overflow-auto border-t p-4 text-xs leading-relaxed"><code
					class="font-mono">{eventJson}</code
				></pre>
		{:else if hasMetadata}
			<div class="border-border/50 max-h-80 overflow-auto border-t">
				{#each metadataEntries as entry (entry.key)}
					<div class="border-border/50 grid grid-cols-[minmax(0,260px)_1fr] items-start gap-3 border-b px-5 py-2 last:border-b-0">
						<div class="text-muted-foreground font-mono text-xs break-all">{entry.key}</div>
						<pre class="font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{entry.value}</pre>
					</div>
				{/each}
			</div>
		{:else}
			<div class="text-muted-foreground border-border/50 border-t px-5 py-3 text-xs">{m.events_no_metadata_provided()}</div>
		{/if}
	</div>
</div>

{#snippet resourceCell(label: string, value: string, copyTitle: string)}
	<div class="border-border/50 rounded-md border p-3">
		<div class="text-muted-foreground text-xs">{label}</div>
		<div class="mt-1 flex items-center justify-between gap-2">
			<div class="min-w-0 font-mono text-sm break-all">{value}</div>
			<CopyButton text={value} size="icon" class="size-7 shrink-0" title={copyTitle} />
		</div>
	</div>
{/snippet}

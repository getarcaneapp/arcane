<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import type { Event } from '$lib/types/event.type';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore, LOCAL_DOCKER_ENVIRONMENT_ID } from '$lib/stores/environment.store.svelte';
	import { AlertIcon, InfoIcon, EnvironmentsIcon, UserIcon, ClockIcon } from '$lib/icons';

	type Severity = 'success' | 'warning' | 'error' | 'info';

	type MetadataEntry = {
		key: string;
		value: string;
	};

	interface Props {
		open: boolean;
		event: Event | null;
	}

	let { open = $bindable(), event }: Props = $props();

	let showRawEvent = $state(false);

	const eventJson = $derived.by(() => JSON.stringify(event ?? {}, null, 2));
	const metadataJson = $derived.by(() => JSON.stringify(event?.metadata ?? {}, null, 2));
	const formattedTimestamp = $derived(event?.timestamp ? formatDate(event.timestamp) : null);
	const severity = $derived((event?.severity ?? 'info') as Severity);
	const metadataEntries = $derived.by(() => flattenMetadata(event?.metadata ?? {}));
	const hasMetadata = $derived(metadataEntries.length > 0);
	const environmentName = $derived.by(() => {
		if (!event?.environmentId) {
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
		if (!event) {
			return null;
		}
		const metadataError = event.metadata?.error;
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

	function formatDate(timestamp: string): string {
		try {
			return new Date(timestamp).toLocaleString();
		} catch {
			return timestamp;
		}
	}

	function stringifyForDisplay(value: unknown): string {
		if (value === null || value === undefined) {
			return '';
		}
		if (typeof value === 'string') {
			return value;
		}
		if (typeof value === 'number' || typeof value === 'boolean') {
			return String(value);
		}
		try {
			return JSON.stringify(value, null, 2);
		} catch {
			return String(value);
		}
	}

	function flattenMetadata(value: unknown, prefix = ''): MetadataEntry[] {
		if (Array.isArray(value)) {
			if (value.length === 0) {
				return prefix ? [{ key: prefix, value: '[]' }] : [];
			}
			return value.flatMap((item, index) => {
				const key = prefix ? `${prefix}[${index}]` : `[${index}]`;
				return flattenMetadata(item, key);
			});
		}

		if (value && typeof value === 'object') {
			const objectValue = value as Record<string, unknown>;
			const keys = Object.keys(objectValue).sort();
			if (keys.length === 0) {
				return prefix ? [{ key: prefix, value: '{}' }] : [];
			}
			return keys.flatMap((key) => {
				const nextPrefix = prefix ? `${prefix}.${key}` : key;
				return flattenMetadata(objectValue[key], nextPrefix);
			});
		}

		if (!prefix) {
			return [];
		}
		return [{ key: prefix, value: stringifyForDisplay(value) }];
	}

	function getSeverityIconClass(sev: Severity): string {
		const baseClasses: Record<Severity, string> = {
			success: 'text-emerald-600 dark:text-emerald-400',
			warning: 'text-amber-600 dark:text-amber-400',
			error: 'text-red-600 dark:text-red-400',
			info: 'text-blue-600 dark:text-blue-400'
		};
		return baseClasses[sev];
	}

	function handleClose() {
		open = false;
	}

	function toggleRawEvent() {
		showRawEvent = !showRawEvent;
	}
</script>

<ResponsiveDialog bind:open contentClass="sm:max-w-[980px]">
	{#snippet children()}
		<div class="space-y-4 pt-4">
			{@render headerContent()}
			{#if eventErrorMessage}
				{@render errorSection(eventErrorMessage)}
			{/if}
			{@render infoCards()}
			{@render metadataSection()}
			{@render rawEventSection()}
		</div>
	{/snippet}

	{#snippet footer()}
		<ArcaneButton action="base" tone="outline" customLabel={m.common_close()} onclick={handleClose} />
	{/snippet}
</ResponsiveDialog>

{#snippet headerContent()}
	<div class="flex items-start gap-3 border-b pb-4">
		<div class="mt-0.5">
			{#if severity === 'success'}
				<AlertIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{:else if severity === 'warning'}
				<AlertIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{:else if severity === 'error'}
				<AlertIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{:else}
				<InfoIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{/if}
		</div>
		<div class="min-w-0 flex-1">
			<h3 class="truncate text-xl font-semibold" title={event?.title}>
				{event?.title ?? m.events_details_title()}
			</h3>
			{#if event?.description}
				<p class="text-muted-foreground mt-1 text-sm">
					{event.description}
				</p>
			{/if}
			<div class="mt-3 flex flex-wrap items-center gap-2">
				<Badge variant="outline" class="gap-1">
					{event?.type ?? m.common_unknown()}
				</Badge>
				{#if event?.environmentId}
					<Badge variant="outline" class="gap-1">
						<EnvironmentsIcon class="size-3" />
						{m.events_environment_label()}: {environmentName ?? event.environmentId}
					</Badge>
				{/if}
				{#if formattedTimestamp}
					<span class="text-muted-foreground inline-flex items-center gap-1 text-xs">
						<ClockIcon class="size-3" />
						{formattedTimestamp}
					</span>
				{/if}
			</div>
		</div>
	</div>
{/snippet}

{#snippet errorSection(message: string)}
	<div class="rounded-lg border border-red-500/30 bg-red-500/10 p-3">
		<div class="text-xs font-semibold text-red-700 dark:text-red-300">{m.events_error()}</div>
		<p class="mt-1 text-sm break-words text-red-700 dark:text-red-200">{message}</p>
	</div>
{/snippet}

{#snippet infoCards()}
	<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
		{@render infoCard(m.events_resource_id_label(), event?.resourceId, m.events_copy_resource_id_title())}
		{@render infoCard(m.events_resource_name_label(), event?.resourceName, m.events_copy_resource_name_title())}

		<div class="rounded-lg border p-3">
			<div class="text-muted-foreground text-xs">{m.common_user()}</div>
			<div class="mt-1 flex items-center gap-2 text-sm">
				<UserIcon class="text-muted-foreground size-4" />
				{event?.username ?? m.common_unknown()}
			</div>
		</div>
	</div>
{/snippet}

{#snippet infoCard(label: string, value: string | undefined, copyTitle: string)}
	<div class="rounded-lg border p-3">
		<div class="text-muted-foreground text-xs">{label}</div>
		<div class="mt-1 flex items-center justify-between gap-2">
			<div class="text-sm break-all">{value || '-'}</div>
			<CopyButton text={value ?? ''} size="icon" class="size-7" title={copyTitle} />
		</div>
	</div>
{/snippet}

{#snippet metadataSection()}
	<div class="rounded-lg border">
		<div class="flex items-center justify-between border-b px-3 py-2">
			<h3 class="text-sm font-medium">{m.events_metadata_title()}</h3>
			<CopyButton text={metadataJson} variant="outline" size="sm" title="Copy metadata JSON">
				{m.common_copy_json()}
			</CopyButton>
		</div>
		{#if hasMetadata}
			<div class="max-h-[50vh] overflow-auto">
				{#each metadataEntries as entry (entry.key)}
					<div class="grid grid-cols-[minmax(0,260px)_1fr] items-start gap-3 border-b px-3 py-2 last:border-b-0">
						<div class="text-muted-foreground font-mono text-xs break-all">{entry.key}</div>
						<pre class="font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{entry.value}</pre>
					</div>
				{/each}
			</div>
		{:else}
			<div class="text-muted-foreground p-3 text-xs">{m.events_no_metadata_provided()}</div>
		{/if}
	</div>
{/snippet}

{#snippet rawEventSection()}
	<div class="rounded-lg border">
		<div class="flex items-center justify-between border-b px-3 py-2">
			<h3 class="text-sm font-medium">{m.events_raw_event_title()}</h3>
			<div class="flex items-center gap-2">
				<ArcaneButton
					action="base"
					tone="outline"
					size="sm"
					customLabel={`${showRawEvent ? m.common_hide() : m.common_show()} ${m.common_raw()}`}
					onclick={toggleRawEvent}
				/>
				<CopyButton text={eventJson} variant="outline" size="sm" title={m.events_copy_full_event_json_title()}>
					{m.common_copy_json()}
				</CopyButton>
			</div>
		</div>
		{#if showRawEvent}
			<pre class="bg-muted/40 max-h-[60vh] overflow-auto p-3 text-xs leading-relaxed"><code class="font-mono">{eventJson}</code
				></pre>
		{/if}
	</div>
{/snippet}

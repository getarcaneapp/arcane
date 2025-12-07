<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import CopyIcon from 'phosphor-svelte/lib/Copy';
	import InfoIcon from 'phosphor-svelte/lib/Info';
	import TriangleAlertIcon from 'phosphor-svelte/lib/WarningCircle';
	import CheckCircle2Icon from 'phosphor-svelte/lib/CheckCircle';
	import XCircleIcon from 'phosphor-svelte/lib/XCircle';
	import UserIcon from 'phosphor-svelte/lib/User';
	import ClockIcon from 'phosphor-svelte/lib/Clock';
	import ServerIcon from 'phosphor-svelte/lib/HardDrives';
	import TagIcon from 'phosphor-svelte/lib/Tag';
	import type { Event } from '$lib/types/event.type';
	import { UseClipboard } from '$lib/hooks/use-clipboard.svelte';
	import { m } from '$lib/paraglide/messages';

	type Severity = 'success' | 'warning' | 'error' | 'info';

	interface Props {
		open: boolean;
		event: Event | null;
	}

	let { open = $bindable(), event }: Props = $props();

	const clipboard = new UseClipboard();

	const hasMetadata = $derived(!!event?.metadata && Object.keys(event.metadata ?? {}).length > 0);
	const eventJson = $derived.by(() => JSON.stringify(event ?? {}, null, 2));
	const metadataJson = $derived.by(() => JSON.stringify(event?.metadata ?? {}, null, 2));
	const formattedTimestamp = $derived(event?.timestamp ? formatDate(event.timestamp) : null);
	const severity = $derived((event?.severity ?? 'info') as Severity);

	function formatDate(timestamp: string): string {
		try {
			return new Date(timestamp).toLocaleString();
		} catch {
			return timestamp;
		}
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

	function getSeverityBadgeClass(sev: Severity): string {
		const baseClasses: Record<Severity, string> = {
			success: 'bg-emerald-500/15 text-emerald-600 border-emerald-500/30 dark:text-emerald-300',
			warning: 'bg-amber-500/15 text-amber-700 border-amber-500/30 dark:text-amber-300',
			error: 'bg-red-500/15 text-red-600 border-red-500/30 dark:text-red-300',
			info: 'bg-blue-500/15 text-blue-700 border-blue-500/30 dark:text-blue-300'
		};
		return baseClasses[sev];
	}

	function handleCopy(text?: string) {
		if (!text) return;
		clipboard.copy(text);
	}

	function handleClose() {
		open = false;
	}
</script>

<ResponsiveDialog bind:open contentClass="sm:max-w-[980px]">
	{#snippet children()}
		<div class="space-y-4 pt-4">
			{@render headerContent()}
			{@render infoCards()}
			{@render metadataSection()}
			{@render rawEventSection()}
		</div>
	{/snippet}

	{#snippet footer()}
		<Button variant="outline" onclick={handleClose}>
			{m.common_close()}
		</Button>
	{/snippet}
</ResponsiveDialog>

{#snippet headerContent()}
	<div class="flex items-start gap-3 border-b pb-4">
		<div class="mt-0.5">
			{#if severity === 'success'}
				<CheckCircle2Icon class={getSeverityIconClass(severity) + ' size-6'} />
			{:else if severity === 'warning'}
				<TriangleAlertIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{:else if severity === 'error'}
				<XCircleIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{:else}
				<InfoIcon class={getSeverityIconClass(severity) + ' size-6'} />
			{/if}
		</div>
		<div class="min-w-0 flex-1">
			<h3 class="text-xl font-semibold">
				{event?.title ?? m.events_details_title()}
			</h3>
			{#if event?.description}
				<p class="text-muted-foreground mt-1 text-sm">
					{event.description}
				</p>
			{/if}
			<div class="mt-3 flex flex-wrap items-center gap-2">
				<Badge class={`border ${getSeverityBadgeClass(severity)}`}>
					{event?.severity ?? m.common_unknown()}
				</Badge>
				<Badge variant="outline" class="gap-1">
					<TagIcon class="size-3" />
					{event?.type ?? m.common_unknown()}
				</Badge>
				{#if event?.environmentId}
					<Badge variant="outline" class="gap-1">
						<ServerIcon class="size-3" />
						{m.events_environment_label()}: {event.environmentId}
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
			<Button variant="ghost" size="icon" class="size-7" onclick={() => handleCopy(value)} title={copyTitle}>
				<CopyIcon class="size-4" />
			</Button>
		</div>
	</div>
{/snippet}

{#snippet metadataSection()}
	<div class="rounded-lg border">
		<div class="flex items-center justify-between border-b px-3 py-2">
			<h3 class="text-sm font-medium">{m.events_metadata_title()}</h3>
			<Button
				variant="outline"
				size="sm"
				onclick={() => handleCopy(metadataJson)}
				disabled={!hasMetadata}
				title="Copy metadata JSON"
			>
				<CopyIcon class="mr-2 size-3" />
				{m.common_copy_json()}
			</Button>
		</div>
		{#if hasMetadata}
			<pre class="bg-muted/40 max-h-[40vh] overflow-auto p-3 text-xs leading-relaxed"><code class="font-mono">{metadataJson}</code
				></pre>
		{:else}
			<div class="text-muted-foreground p-3 text-xs">{m.events_no_metadata_provided()}</div>
		{/if}
	</div>
{/snippet}

{#snippet rawEventSection()}
	<div class="rounded-lg border">
		<div class="flex items-center justify-between border-b px-3 py-2">
			<h3 class="text-sm font-medium">{m.events_raw_event_title()}</h3>
			<Button variant="outline" size="sm" onclick={() => handleCopy(eventJson)} title={m.events_copy_full_event_json_title()}>
				<CopyIcon class="mr-2 size-3" />
				{m.common_copy_json()}
			</Button>
		</div>
		<pre class="bg-muted/40 max-h-[40vh] overflow-auto p-3 text-xs leading-relaxed"><code class="font-mono">{eventJson}</code
			></pre>
	</div>
{/snippet}

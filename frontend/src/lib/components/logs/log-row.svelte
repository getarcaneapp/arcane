<script lang="ts">
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import { ArrowDownIcon, ArrowRightIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatTime } from '#lib/utils/formatting.js';

	import { getStructuredLog, type LogViewerDisplayEntry, type LogViewerEntry } from './log-viewer.utils';
	import StructuredLogEntry from './structured-log-entry.svelte';

	interface Props {
		displayLog: LogViewerDisplayEntry;
		mobile: boolean;
		type: 'container' | 'project' | 'service';
		showTimestamps: boolean;
		showParsedJson: boolean;
		showStreamLabels: boolean;
		expanded: boolean;
		onExpandedChange: (open: boolean) => void;
	}

	let { displayLog, mobile, type, showTimestamps, showParsedJson, showStreamLabels, expanded, onExpandedChange }: Props =
		$props();

	const SERVICE_COLORS = [
		'text-cyan',
		'text-warning',
		'text-success',
		'text-info',
		'text-purple',
		'text-pink',
		'text-orange',
		'text-teal',
		'text-lime',
		'text-indigo',
		'text-fuchsia',
		'text-rose'
	];

	const first = $derived(displayLog.entries[0]);
	const isStderr = $derived(first?.level === 'stderr' || first?.level === 'error');
	const service = $derived(type === 'project' && showStreamLabels ? first?.service : undefined);
	const timestamp = $derived(showTimestamps ? first?.timestamp : undefined);
	const formattedTimestamp = $derived(timestamp ? formatTime(timestamp, { includeSeconds: true }) || timestamp : '');
	const serviceColor = $derived.by(() => {
		if (!service) return '';
		let hash = 0;
		for (let i = 0; i < service.length; i++) {
			hash = service.charCodeAt(i) + ((hash << 5) - hash);
		}
		return SERVICE_COLORS[Math.abs(hash) % SERVICE_COLORS.length] ?? 'text-foreground/80';
	});

	function isStructuredLogData(value: unknown): value is Record<string, unknown> {
		return !!value && typeof value === 'object' && !Array.isArray(value);
	}
</script>

{#snippet message(entry: LogViewerEntry)}
	{@const structured = showParsedJson ? getStructuredLog(entry) : null}
	{#if structured?.isStructured && isStructuredLogData(structured.parsed)}
		<div class="min-w-0 flex-1">
			<StructuredLogEntry data={structured.parsed} rawJson={entry.message} />
		</div>
	{:else}
		<div class="min-w-0 flex-1 break-words whitespace-pre-wrap">
			<!-- eslint-disable-next-line svelte/no-at-html-tags -- ansiToHtml escapes markup before adding color spans -->
			{@html entry.html}
		</div>
	{/if}
{/snippet}

{#snippet stderrMarker()}
	<span class="flex h-5 w-1.5 shrink-0 items-center">
		{#if isStderr}
			<span
				class="size-1.5 rounded-full bg-destructive"
				role="img"
				aria-label={m.log_stderr_line()}
				title={m.log_stderr_line()}
				data-stream="stderr"
			></span>
		{/if}
	</span>
{/snippet}

{#snippet entries()}
	{#if displayLog.grouped && first}
		<Collapsible.Root open={expanded} onOpenChange={onExpandedChange} class="min-w-0 flex-1">
			<div class="flex min-w-0 items-start gap-2">
				<Collapsible.Trigger>
					{#snippet child({ props })}
						<button
							{...props}
							type="button"
							class="inline-flex h-5 shrink-0 items-center gap-1 rounded-md px-1 text-muted-foreground outline-none hover:bg-foreground/4 focus-visible:ring-3 focus-visible:ring-ring"
						>
							{#if expanded}
								<ArrowDownIcon class="size-3.5" />
							{:else}
								<ArrowRightIcon class="size-3.5" />
							{/if}
							<span class="text-2xs font-medium">+{Math.max(displayLog.entries.length - 1, 0)}</span>
						</button>
					{/snippet}
				</Collapsible.Trigger>

				{@render message(first)}
			</div>
			<Collapsible.Content>
				<div class="mt-1 flex flex-col gap-0.5 border-l border-foreground/8 pl-4">
					{#each displayLog.entries.slice(1) as entry (entry.id)}
						{@render message(entry)}
					{/each}
				</div>
			</Collapsible.Content>
		</Collapsible.Root>
	{:else}
		<div class="flex min-w-0 flex-1 flex-col gap-0.5">
			{#each displayLog.entries as entry (entry.id)}
				{@render message(entry)}
			{/each}
		</div>
	{/if}
{/snippet}

{#if mobile}
	<div class="px-3 py-1 leading-5 transition-colors hover:bg-foreground/4">
		{#if timestamp || service}
			<div class="flex min-w-0 items-center gap-2 text-2xs">
				{#if timestamp}
					<span class="shrink-0 text-muted-foreground tabular-nums" title={timestamp}>{formattedTimestamp}</span>
				{/if}
				{#if service}
					<span class="min-w-0 truncate {serviceColor}" title={service}>{service}</span>
				{/if}
			</div>
		{/if}
		<div class="flex min-w-0 items-start gap-2">
			{@render stderrMarker()}
			{@render entries()}
		</div>
	</div>
{:else}
	<div class="flex min-w-0 items-start gap-3 px-3 py-0.5 leading-5 transition-colors hover:bg-foreground/4">
		{#if timestamp}
			<span class="shrink-0 text-muted-foreground tabular-nums" title={timestamp}>{formattedTimestamp}</span>
		{/if}
		{#if service}
			<span class="w-28 shrink-0 truncate {serviceColor}" title={service}>{service}</span>
		{/if}
		<div class="flex min-w-0 flex-1 items-start gap-2">
			{@render stderrMarker()}
			{@render entries()}
		</div>
	</div>
{/if}

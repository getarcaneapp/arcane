<script lang="ts">
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import { ArrowDownIcon, ArrowRightIcon } from '#lib/icons/index.js';
	import { cn } from '#lib/utils.js';
	import { formatDateTime } from '#lib/utils/formatting.js';

	import { getStructuredLog, type LogViewerDisplayEntry, type LogViewerEntry } from './log-viewer.utils';
	import StructuredLogEntry from './structured-log-entry.svelte';

	interface Props {
		displayLog: LogViewerDisplayEntry;
		index: number;
		mobile: boolean;
		type: 'container' | 'project' | 'service';
		showTimestamps: boolean;
		showParsedJson: boolean;
		showStreamLabels: boolean;
		expanded: boolean;
		onExpandedChange: (open: boolean) => void;
	}

	let { displayLog, index, mobile, type, showTimestamps, showParsedJson, showStreamLabels, expanded, onExpandedChange }: Props =
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
	const level = $derived<LogViewerEntry['level']>(first?.level ?? 'stdout');
	const service = $derived(type === 'project' ? first?.service : undefined);
	const timestamp = $derived(showTimestamps ? first?.timestamp : undefined);
	const formattedTimestamp = $derived(
		timestamp ? formatDateTime(timestamp, { dateStyle: 'short', includeSeconds: true }) || timestamp : ''
	);
	const levelClass = $derived.by(() => {
		switch (level) {
			case 'stderr':
			case 'error':
				return 'text-destructive';
			case 'stdout':
			case 'info':
				return 'text-success';
			default:
				return 'text-foreground/80';
		}
	});
	const serviceColor = $derived.by(() => {
		if (!service) return '';
		let hash = 0;
		for (let i = 0; i < service.length; i++) {
			hash = service.charCodeAt(i) + ((hash << 5) - hash);
		}
		return SERVICE_COLORS[Math.abs(hash) % SERVICE_COLORS.length] ?? 'text-foreground/80';
	});
	const contentClass = $derived(
		mobile ? 'text-sm break-words whitespace-pre-wrap text-foreground/80' : 'break-words whitespace-pre-wrap text-foreground/80'
	);

	function isStructuredLogData(value: unknown): value is Record<string, unknown> {
		return !!value && typeof value === 'object' && !Array.isArray(value);
	}
</script>

{#snippet message(entry: LogViewerEntry)}
	{@const structured = showParsedJson ? getStructuredLog(entry) : null}
	<div class={contentClass}>
		{#if structured?.isStructured && isStructuredLogData(structured.parsed)}
			<StructuredLogEntry data={structured.parsed} rawJson={entry.message} />
		{:else}
			{@html entry.html}
		{/if}
	</div>
{/snippet}

{#snippet levelLabel(labelClass: string)}
	{#if showStreamLabels || (level !== 'stdout' && level !== 'stderr')}
		<span class={cn('shrink-0', labelClass, levelClass)}>
			{level.toUpperCase()}
		</span>
	{:else}
		<span
			class={cn('inline-flex h-4 shrink-0 items-center', labelClass, levelClass)}
			role="img"
			aria-label={level.toUpperCase()}
			title={level.toUpperCase()}
			data-stream={level}
		>
			<span class={cn('size-2 bg-current', level === 'stdout' ? 'rounded-full' : 'rounded-xs')}></span>
		</span>
	{/if}
{/snippet}

{#snippet entries(nestedClass: string, flatClass: string)}
	{#if displayLog.grouped && first}
		<Collapsible.Root open={expanded} onOpenChange={onExpandedChange}>
			<div class="flex min-w-0 items-start gap-2">
				<Collapsible.Trigger>
					{#snippet child({ props })}
						<button
							{...props}
							type="button"
							class="inline-flex shrink-0 items-center gap-1 rounded-xl border border-transparent px-1 py-0.5 text-muted-foreground outline-none hover:bg-white/4 focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
						>
							{#if expanded}
								<ArrowDownIcon class="size-4" />
							{:else}
								<ArrowRightIcon class="size-4" />
							{/if}
							<span class="text-2xs font-medium">+{Math.max(displayLog.entries.length - 1, 0)}</span>
						</button>
					{/snippet}
				</Collapsible.Trigger>

				{@render message(first)}
			</div>
			<Collapsible.Content>
				<div class={nestedClass}>
					{#each displayLog.entries.slice(1) as entry (entry.id)}
						{@render message(entry)}
					{/each}
				</div>
			</Collapsible.Content>
		</Collapsible.Root>
	{:else}
		<div class={flatClass}>
			{#each displayLog.entries as entry (entry.id)}
				{@render message(entry)}
			{/each}
		</div>
	{/if}
{/snippet}

{#if mobile}
	<div
		class={cn(
			'border-l-2 border-transparent px-3 py-2 transition-colors hover:border-info hover:bg-card/50',
			index % 2 === 1 && 'bg-card/60'
		)}
	>
		<div class="mb-1 flex flex-wrap items-center gap-2 text-xs">
			{#if timestamp}
				<span
					class="inline-flex shrink-0 self-start rounded-md border border-info/15 bg-card px-2 py-1 font-semibold text-info tabular-nums"
					title={timestamp}
				>
					{formattedTimestamp}
				</span>
			{/if}
			{#if service}
				<span class="shrink-0 truncate font-semibold {serviceColor}" title={service}>
					{service}
				</span>
			{/if}
			{@render levelLabel('')}
		</div>
		{@render entries('mt-2 space-y-1.5 border-l border-border pl-4', 'flex flex-col gap-1.5')}
	</div>
{:else}
	<div
		class={cn(
			'flex items-start border-l-2 border-transparent px-3 py-1.5 transition-colors hover:border-info hover:bg-card/50',
			index % 2 === 1 && 'bg-card/60'
		)}
	>
		{#if timestamp}
			<span
				class="mr-3 inline-flex min-w-44.5 shrink-0 self-start rounded-md border border-info/15 bg-card px-2.5 py-1 text-2xs font-semibold text-info tabular-nums"
				title={timestamp}
			>
				{formattedTimestamp}
			</span>
		{/if}
		{#if service}
			<span class="mr-3 max-w-30 min-w-30 shrink-0 truncate text-xs font-semibold {serviceColor}" title={service}>
				{service}
			</span>
		{/if}
		{@render levelLabel('mr-2 min-w-fit pt-1 text-xs')}
		<div class="min-w-0 flex-1">
			{@render entries('mt-2 space-y-1 border-l border-border pl-4', 'flex flex-1 flex-col gap-1')}
		</div>
	</div>
{/if}

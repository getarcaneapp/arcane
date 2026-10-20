<script module lang="ts">
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { CheckIcon, CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDateTimeShort, withOccurrenceKeys } from '#lib/utils/formatting.js';

	export function formatTime(timeStr: string | undefined) {
		if (!timeStr) return '-';
		return formatDateTimeShort(timeStr) || timeStr;
	}

	export { sectionHeading, stat, tagGroup, capRow, commitRow, blockRow, infoRow, presentRow, storageDetailsSection };
</script>

{#snippet sectionHeading(title: string)}
	<h3 class="border-b border-border/50 pb-1 text-3xs font-semibold tracking-wider text-muted-foreground uppercase">
		{title}
	</h3>
{/snippet}

{#snippet stat(label: string, value: string | number, dot?: string)}
	<div class="flex items-baseline gap-1.5">
		{#if dot}
			<span class="size-1.5 shrink-0 self-center rounded-full {dot}"></span>
		{/if}
		<span class="text-sm font-semibold tabular-nums">{value}</span>
		<span class="text-3xs tracking-tight whitespace-nowrap text-muted-foreground uppercase">{label}</span>
	</div>
{/snippet}

{#snippet tagGroup(label: string, items: string[] | undefined)}
	<div class="grid grid-cols-label-value items-baseline gap-x-3">
		<span class="text-3xs leading-4 tracking-tight text-muted-foreground uppercase">{label}</span>
		<div class="flex flex-wrap justify-end gap-1">
			{#each withOccurrenceKeys(items) as { value: item, key } (key)}
				<Badge variant="outline" size="sm">{item}</Badge>
			{:else}
				<span class="text-xs text-muted-foreground">-</span>
			{/each}
		</div>
	</div>
{/snippet}

{#snippet capRow(label: string, value: boolean | undefined)}
	<div class="flex items-center justify-between gap-2">
		<span class="text-3xs tracking-tight wrap-anywhere text-muted-foreground uppercase">{label}</span>
		{#if value}
			<CheckIcon class="size-3 shrink-0 text-success" />
			<span class="sr-only">{m.common_yes()}</span>
		{:else}
			<CloseIcon class="size-3 shrink-0 text-muted-foreground/50" />
			<span class="sr-only">{m.common_no()}</span>
		{/if}
	</div>
{/snippet}

{#snippet commitRow(commit: string | undefined)}
	<div class="grid grid-cols-label-value items-baseline gap-x-3">
		<span class="text-3xs leading-4 tracking-tight text-muted-foreground uppercase">{m.docker_info_git_commit_label()}</span>
		<div class="flex items-center justify-end gap-1.5">
			<code class="rounded bg-muted px-1.5 py-0.5 text-xs">{commit?.slice(0, 8) ?? '-'}</code>
			{#if commit}
				<CopyButton text={commit} size="icon" class="size-5" title={m.docker_copy_commit_hash()} />
			{/if}
		</div>
	</div>
{/snippet}

{#snippet blockRow(label: string, value: string)}
	<div class="space-y-0.5 pt-0.5">
		<div class="text-3xs tracking-tight text-muted-foreground uppercase">{label}</div>
		<div
			class="max-h-48 overflow-y-auto rounded-md bg-muted/30 px-2 py-1 font-mono text-2xs leading-relaxed break-all text-foreground/80"
		>
			{value}
		</div>
	</div>
{/snippet}

{#snippet infoRow(label: string, value: string | number | undefined | null, mono: boolean = true)}
	<div class="grid grid-cols-label-value items-baseline gap-x-3">
		<span class="text-3xs leading-4 tracking-tight text-muted-foreground uppercase">{label}</span>
		<span class="text-right text-xs leading-4 wrap-anywhere {mono ? 'font-mono' : ''}">
			{value === undefined || value === null || value === '' ? '-' : value}
		</span>
	</div>
{/snippet}

{#snippet presentRow(label: string, value: string | number | undefined | null, mono: boolean = true)}
	{#if value !== undefined && value !== null && value !== ''}
		{@render infoRow(label, value, mono)}
	{/if}
{/snippet}

{#snippet storageDetailsSection(entries: string[][])}
	<section class="space-y-1.5">
		{@render sectionHeading(m.docker_info_storage_details_section())}
		<div class="space-y-0.5">
			{#each entries as entry (entry)}
				{@render infoRow(entry[0] ?? '', entry[1], false)}
			{/each}
		</div>
	</section>
{/snippet}

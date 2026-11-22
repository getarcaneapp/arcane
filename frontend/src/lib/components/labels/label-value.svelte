<script lang="ts">
	import IconImage from '#lib/components/icon-image.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { ExternalLinkIcon, ImagesIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import type { LabelValue } from '#lib/utils/well-known-labels.js';

	interface Props {
		value: LabelValue;
	}

	let { value }: Props = $props();
</script>

{#if value.kind === 'boolean'}
	<Badge variant={value.value ? 'green' : 'gray'} size="xs">
		{value.value ? m.common_yes() : m.common_no()}
	</Badge>
{:else if value.kind === 'url'}
	<a
		href={value.href}
		target="_blank"
		rel="external noopener noreferrer"
		class="inline-flex items-center gap-1 text-sm break-all text-primary-text hover:underline"
	>
		<span>{value.display}</span>
		<ExternalLinkIcon class="size-3.5 shrink-0" />
	</a>
{:else if value.kind === 'datetime'}
	{@const formatted = formatDateTimeShort(value.iso)}
	{#if formatted}
		<div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
			<span class="text-sm text-foreground">{formatted}</span>
			<span class="font-mono text-xs text-muted-foreground select-all" title={m.common_click_to_select()}>{value.iso}</span>
		</div>
	{:else}
		<span class="cursor-pointer font-mono text-sm break-all text-foreground select-all" title={m.common_click_to_select()}>
			{value.iso}
		</span>
	{/if}
{:else if value.kind === 'icon'}
	<div class="flex items-center gap-2">
		<IconImage
			src={value.previewUrl}
			alt={value.rawValue}
			fallback={ImagesIcon}
			class="size-4"
			containerClass="size-6 shrink-0"
		/>
		<span class="cursor-pointer font-mono text-sm break-all text-foreground select-all" title={m.common_click_to_select()}>
			{value.rawValue}
		</span>
	</div>
{:else if value.kind === 'hours'}
	<span class="font-mono text-sm text-foreground">{m.hours_short({ hours: value.hours })}</span>
{:else if value.kind === 'text'}
	<span class="text-sm text-foreground">{value.text}</span>
{:else}
	<span class="cursor-pointer font-mono text-sm break-all text-foreground select-all" title={m.common_click_to_select()}>
		{value.code}
	</span>
{/if}

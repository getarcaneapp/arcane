<script lang="ts">
	import { HardenedImageBadge } from '#lib/components/badges/index.js';
	import { KeyValueCard } from '#lib/components/resource-detail/index.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { ExternalLinkIcon, ShieldAlertIcon, ShieldXIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { HardenedImageInfo } from '#lib/types/image.js';
	import { formatDate, formatDateTimeShort } from '#lib/utils/formatting.js';
	import { hardenedSupportSummary } from '#lib/utils/hardened-images.js';

	let { info }: { info: HardenedImageInfo } = $props();

	const endOfLife = $derived(formatDate(info.endOfLifeDate, { timeZone: 'UTC' }));
	const ended = $derived(info.supportStatus === 'end_of_life');
	const showAlert = $derived(ended || info.supportStatus === 'ending_soon');
	const variant = $derived([info.variant, info.flavor].filter(Boolean).join(' · '));
</script>

{#snippet textTile(label: string, value: string)}
	<KeyValueCard {label} valueFormat="text" valueTitle={value}>{value}</KeyValueCard>
{/snippet}

{#snippet codeTile(label: string, value: string)}
	<KeyValueCard {label} class="sm:col-span-2 lg:col-span-3" valueTitle={value}>{value}</KeyValueCard>
{/snippet}

<div class="space-y-4">
	{#if showAlert}
		<Alert.Root
			variant={ended ? 'destructive' : 'warning'}
			icon={ended ? ShieldXIcon : ShieldAlertIcon}
			heading={hardenedSupportSummary(info)}
			description={ended
				? m.hardened_image_end_of_life_description({ date: endOfLife })
				: m.hardened_image_ending_soon_description({ date: endOfLife })}
		/>
	{/if}

	<div class="flex flex-wrap items-center gap-2">
		<HardenedImageBadge {info} />
		{#each info.compliance ?? [] as framework (framework)}
			<Badge variant="blue" size="sm">{framework.toUpperCase()}</Badge>
		{/each}
		{#if info.url}
			<a
				href={info.url}
				target="_blank"
				rel="external noopener noreferrer"
				class="ml-auto inline-flex items-center gap-1 text-xs font-medium text-primary-text hover:underline"
			>
				{m.view_in_catalog()}
				<ExternalLinkIcon class="size-3 shrink-0" />
			</a>
		{/if}
	</div>

	<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
		{@render textTile(m.common_name(), info.title || info.name)}
		{#if info.version}
			<KeyValueCard label={m.common_version()} valueTitle={info.version}>{info.version}</KeyValueCard>
		{/if}
		{#if info.distro}
			{@render textTile(m.distribution(), info.distro)}
		{/if}
		{#if variant}
			<KeyValueCard label={m.variant()} valueFormat="text" class="capitalize">{variant}</KeyValueCard>
		{/if}
		{#if info.entitlement}
			<KeyValueCard label={m.entitlement()} valueFormat="text" class="capitalize">{info.entitlement}</KeyValueCard>
		{/if}
		{#if info.releaseDate}
			{@render textTile(m.release_date(), formatDate(info.releaseDate, { timeZone: 'UTC' }))}
		{/if}
		{#if endOfLife}
			<KeyValueCard label={m.end_of_life()} valueFormat="custom">
				<span class="text-sm font-medium text-foreground">{endOfLife}</span>
				<span class="ml-1 text-xs text-muted-foreground">{hardenedSupportSummary(info)}</span>
			</KeyValueCard>
		{/if}
		{#if info.created}
			{@render textTile(m.built(), formatDateTimeShort(info.created))}
		{/if}
		{#if info.shell !== undefined}
			{@render textTile(m.common_shell(), info.shell || m.none())}
		{/if}
		{#if info.packageManager !== undefined}
			{@render textTile(m.package_manager(), info.packageManager || m.none())}
		{/if}
		{#if info.definition}
			{@render codeTile(m.definition(), info.definition)}
		{/if}
		{#if info.chainId}
			{@render codeTile(m.chain_id(), info.chainId)}
		{/if}
	</div>
</div>

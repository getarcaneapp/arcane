<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import { m } from '$lib/paraglide/messages';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import BookOpenIcon from '@lucide/svelte/icons/book-open';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { getApplicationLogo } from '$lib/utils/image.util';
	import GithubIcon from '$lib/icons/github-icon.svelte';

	interface Props {
		open: boolean;
		onOpenChange: (open: boolean) => void;
		versionInfo: AppVersionInformation;
	}

	let { open = $bindable(false), onOpenChange, versionInfo }: Props = $props();

	const shortCommit = $derived(versionInfo.shortRevision || versionInfo.revision?.slice(0, 8) || '-');
	const shortDigest = $derived(versionInfo.currentDigest?.slice(0, 19) || '-');
	const logoUrl = $derived(getApplicationLogo(false));
</script>

<Dialog.Root bind:open {onOpenChange}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title class="flex items-center gap-2">
				<img src={logoUrl} alt="Arcane" class="size-6" />
				{m.version_info_title()}
			</Dialog.Title>
			<Dialog.Description>
				{m.version_info_description()}
			</Dialog.Description>
		</Dialog.Header>

		<div class="space-y-2 rounded-lg border p-3">
			{@render infoRow(m.version_info_version(), versionInfo.displayVersion || versionInfo.currentVersion)}

			{#if versionInfo.currentTag}
				{@render infoRow(m.version_info_tag(), versionInfo.currentTag)}
			{/if}

			{@render infoRowWithCopy(m.version_info_full_commit(), shortCommit, versionInfo.revision)}

			{@render infoRow(m.version_info_go_version(), versionInfo.goVersion || '-')}

			{#if versionInfo.buildTime && versionInfo.buildTime !== 'unknown'}
				{@render infoRow(m.version_info_build_time(), versionInfo.buildTime)}
			{/if}

			{#if versionInfo.currentDigest}
				{@render infoRowWithCopy(m.version_info_digest(), shortDigest, versionInfo.currentDigest)}
			{/if}
		</div>

		<Dialog.Footer class="flex-col gap-2 sm:flex-row">
			{#if versionInfo.releaseUrl}
				<Button variant="outline" class="gap-2" onclick={() => window.open(versionInfo.releaseUrl, '_blank')}>
					<ExternalLinkIcon class="size-4" />
					{m.version_info_view_release()}
				</Button>
			{/if}
			<Button variant="outline" size="icon" onclick={() => window.open('https://getarcane.app', '_blank')} title="Documentation">
				<BookOpenIcon class="size-4" />
			</Button>
			<Button
				variant="outline"
				size="icon"
				onclick={() => window.open('https://github.com/getarcaneapp/arcane', '_blank')}
				title="GitHub"
			>
				<GithubIcon class="size-4" />
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

{#snippet infoRow(label: string, value: string | undefined | null)}
	<div class="flex items-center justify-between gap-4">
		<span class="text-muted-foreground shrink-0 text-xs">{label}</span>
		<Badge variant="secondary" class="text-xs font-normal" title={value ?? ''}>{value || '-'}</Badge>
	</div>
{/snippet}

{#snippet infoRowWithCopy(label: string, displayValue: string, fullValue: string | undefined | null)}
	<div class="flex items-center justify-between gap-4">
		<span class="text-muted-foreground shrink-0 text-xs">{label}</span>
		<div class="flex items-center gap-2">
			<Badge variant="secondary" class="text-xs font-normal" title={fullValue ?? ''}>{displayValue}</Badge>
			{#if fullValue && fullValue !== 'unknown'}
				<CopyButton text={fullValue} size="icon" class="size-6" />
			{/if}
		</div>
	</div>
{/snippet}

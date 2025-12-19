<script lang="ts">
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import { m } from '$lib/paraglide/messages';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { getApplicationLogo } from '$lib/utils/image.util';
	import { ExternalLinkIcon, GithubIcon, BookOpenIcon } from '$lib/icons';

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
				<ArcaneButton
					action="base"
					tone="outline"
					class="gap-2"
					onclick={() => window.open(versionInfo.releaseUrl, '_blank')}
					icon={ExternalLinkIcon}
					customLabel={m.version_info_view_release()}
				/>
			{/if}
			<ArcaneButton
				action="base"
				tone="outline"
				size="icon"
				onclick={() => window.open('https://getarcane.app', '_blank')}
				title="Documentation"
				icon={BookOpenIcon}
			/>
			<ArcaneButton
				action="base"
				tone="outline"
				size="icon"
				onclick={() => window.open('https://github.com/getarcaneapp/arcane', '_blank')}
				title="GitHub"
				icon={GithubIcon}
			/>
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

<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
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

<ResponsiveDialog bind:open {onOpenChange} description={m.version_info_description()} contentClass="sm:max-w-md">
	{#snippet title()}
		<div class="flex items-center gap-2">
			<img src={logoUrl} alt="Arcane" class="size-6" />
			{m.version_info_title()}
		</div>
	{/snippet}

	{#snippet children()}
		<div class="flex flex-col gap-6 pt-4">
			<div class="space-y-1.5 rounded-lg border p-3">
				{@render infoRow(m.version_info_version(), versionInfo.displayVersion || versionInfo.currentVersion)}

				{#if versionInfo.currentTag}
					{@render infoRow(m.version_info_tag(), versionInfo.currentTag)}
				{/if}

				{@render infoRowWithCopy(m.version_info_full_commit(), shortCommit, versionInfo.revision)}

				{@render infoRow(m.version_info_go_version(), versionInfo.goVersion || '-')}

				{#if versionInfo.buildTime && versionInfo.buildTime !== 'unknown'}
					{@render infoRow(m.version_info_build_time(), versionInfo.buildTime, false)}
				{/if}

				{#if versionInfo.currentDigest}
					{@render infoRowWithCopy(m.version_info_digest(), shortDigest, versionInfo.currentDigest)}
				{/if}
			</div>
		</div>
	{/snippet}

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
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
		</div>
	{/snippet}
</ResponsiveDialog>

{#snippet infoRow(label: string, value: string | undefined | null, mono: boolean = true)}
	<div class="flex items-center justify-between gap-4">
		<span class="text-muted-foreground shrink-0 text-xs tracking-tight uppercase">{label}</span>
		<span class="text-sm {mono ? 'font-mono' : ''}" title={value ?? ''}>{value || '-'}</span>
	</div>
{/snippet}

{#snippet infoRowWithCopy(label: string, displayValue: string, fullValue: string | undefined | null)}
	<div class="flex items-center justify-between gap-4">
		<span class="text-muted-foreground shrink-0 text-xs tracking-tight uppercase">{label}</span>
		<div class="flex items-center gap-2">
			<span class="font-mono text-sm" title={fullValue ?? ''}>{displayValue}</span>
			{#if fullValue && fullValue !== 'unknown'}
				<CopyButton text={fullValue} size="icon" class="size-6" />
			{/if}
		</div>
	</div>
{/snippet}

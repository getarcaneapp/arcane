<script lang="ts">
	import { ResponsiveDialog } from '$lib/components/ui/responsive-dialog/index.js';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import type { AppVersionInformation } from '$lib/types/settings';
	import { m } from '$lib/paraglide/messages';
	import { CopyButton } from '$lib/components/ui/copy-button';
	import { getApplicationLogo } from '$lib/utils/docker';
	import { accentColorPreviewStore } from '$lib/utils/theme';
	import { ExternalLinkIcon, GithubIcon, BookOpenIcon } from '$lib/icons';

	interface Props {
		open: boolean;
		onOpenChange: (open: boolean) => void;
		versionInfo: AppVersionInformation;
		debugMode?: boolean;
	}

	let { open = $bindable(false), onOpenChange, versionInfo, debugMode = false }: Props = $props();

	const mockVersionInfo = {
		displayVersion: 'v1.2.4-preview',
		currentVersion: 'v1.2.4-preview',
		currentTag: 'edge',
		revision: 'b9c2a1240c83a54b73b5cf2e5d3f23a9b102837f',
		goVersion: 'go1.22.1',
		nodeVersion: 'v26.0.0',
		svelteKitVersion: '3.0.0-next.1',
		buildTime: '2026-04-15T12:00:00Z',
		enabledFeatures: ['autologin'],
		currentDigest: 'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
		releaseUrl: 'https://github.com/getarcaneapp/arcane/releases/tag/v1.2.4'
	} as AppVersionInformation;

	const displayInfo = $derived(debugMode ? mockVersionInfo : versionInfo);

	const enabledFeatures = $derived((displayInfo.enabledFeatures ?? []).filter(Boolean).join(', '));
	const accentColor = $derived($accentColorPreviewStore);
	const logoUrl = $derived(getApplicationLogo(false, accentColor, accentColor));
</script>

<ResponsiveDialog {open} {onOpenChange} contentClass="sm:max-w-md">
	{#snippet title()}
		<div class="flex items-center gap-3">
			<img src={logoUrl} alt="Arcane" class="size-7" />
			<div class="flex flex-col gap-0.5">
				<span class="text-xl leading-none">{m.version_info_title()}</span>
				<span class="text-sm font-normal text-muted-foreground"
					>{(displayInfo.displayVersion || displayInfo.currentVersion).replace(/^v/, '')}</span
				>
			</div>
		</div>
	{/snippet}

	<div class="flex flex-col gap-0 py-2">
		{#if displayInfo.currentTag}
			{@render infoRow(m.version_info_tag(), displayInfo.currentTag)}
		{/if}

		{@render infoRowWithCopy(m.version_info_full_commit(), displayInfo.revision, displayInfo.revision)}

		{@render infoRow(m.go_version(), displayInfo.goVersion || '-')}

		{@render infoRow(m.version_info_node_version(), displayInfo.nodeVersion || '-')}

		{@render infoRow(m.version_info_sveltekit_version(), displayInfo.svelteKitVersion || '-')}

		{#if displayInfo.buildTime && displayInfo.buildTime !== 'unknown'}
			{@render infoRow(m.build_time(), displayInfo.buildTime, false)}
		{/if}

		{@render infoRow(m.version_info_build_features(), enabledFeatures || '-')}

		{#if displayInfo.currentDigest}
			{@render infoRowWithCopy(m.version_info_digest(), displayInfo.currentDigest, displayInfo.currentDigest)}
		{/if}
	</div>

	{#snippet footer()}
		<div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
			{#if displayInfo.releaseUrl}
				<ArcaneButton
					action="base"
					tone="outline"
					class="gap-2"
					onclick={() => window.open(displayInfo.releaseUrl, '_blank')}
					icon={ExternalLinkIcon}
					customLabel={m.version_info_view_release()}
				/>
			{/if}
			<ArcaneButton
				action="base"
				tone="outline"
				size="icon"
				onclick={() => window.open('https://getarcane.app', '_blank')}
				title={m.common_documentation()}
				icon={BookOpenIcon}
			/>
			<ArcaneButton
				action="base"
				tone="outline"
				size="icon"
				onclick={() => window.open('https://github.com/getarcaneapp/arcane', '_blank')}
				title={m.common_github()}
				icon={GithubIcon}
			/>
		</div>
	{/snippet}
</ResponsiveDialog>

{#snippet infoRow(label: string, value: string | undefined | null, mono: boolean = true)}
	<div class="flex flex-col items-start gap-1 border-b border-border/50 py-3 last:border-0">
		<span class="text-sm font-medium text-foreground">{label}</span>
		<span
			class="text-sm break-all {mono ? 'font-mono text-xs text-muted-foreground' : 'text-muted-foreground'}"
			title={value ?? ''}>{value || '-'}</span
		>
	</div>
{/snippet}

{#snippet infoRowWithCopy(label: string, displayValue: string, fullValue: string | undefined | null)}
	<div class="flex flex-col items-start gap-1 border-b border-border/50 py-3 last:border-0">
		<span class="text-sm font-medium text-foreground">{label}</span>
		<div class="flex w-full items-start justify-between gap-2">
			<span class="mt-0.5 font-mono text-xs break-all text-muted-foreground" title={fullValue ?? ''}
				>{fullValue || displayValue}</span
			>
			{#if fullValue && fullValue !== 'unknown'}
				<CopyButton text={fullValue} size="icon" class="-mt-0.5 size-6 shrink-0" />
			{/if}
		</div>
	</div>
{/snippet}

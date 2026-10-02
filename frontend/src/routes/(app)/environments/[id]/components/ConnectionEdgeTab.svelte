<script lang="ts">
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentConnectionDetails from './EnvironmentConnectionDetails.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { DownloadIcon, ResetIcon } from '#lib/icons/index.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import type { ConnectionEdgeTabProps } from './tab-props';

	let { environment, currentStatus, showMTLSDownloads, isRegeneratingKey, onRegenerateApiKey }: ConnectionEdgeTabProps = $props();

	let mtlsBundleDownloadHref = $derived(`/api/environments/${environment.id}/deployment/mtls/bundle`);
	let mtlsCertificateDownloadHref = $derived(`/api/environments/${environment.id}/deployment/mtls/agent.crt`);
	let mtlsKeyDownloadHref = $derived(`/api/environments/${environment.id}/deployment/mtls/agent.key`);
	let showAgentSecurity = $derived(environment.id !== '0');

	let mtlsCertificateBadge = $derived.by((): { text: string; variant: 'green' | 'amber' | 'red' } | null => {
		const cert = environment.edgeMTLSCertificate;
		if (!cert) return null;
		if (cert.expired) {
			return { text: m.expired(), variant: 'red' };
		}
		if (cert.expiringSoon) {
			return { text: m.environments_edge_mtls_certificate_status_expiring_soon(), variant: 'amber' };
		}
		return { text: m.environments_edge_mtls_certificate_status_valid(), variant: 'green' };
	});
</script>

{#snippet badgeTile(label: string, text: string, variant: BadgeVariant)}
	<div class="flex flex-col gap-1.5 rounded-lg border border-border/50 bg-card/30 p-3">
		<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{label}</div>
		<div><Badge {variant} minWidth="20">{text}</Badge></div>
	</div>
{/snippet}

{#snippet tile(label: string, value: string, opts?: { mono?: boolean; subtext?: string })}
	<div class="flex flex-col gap-1 rounded-lg border border-border/50 bg-card/30 p-3">
		<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{label}</div>
		<div class="text-sm font-medium text-foreground {opts?.mono ? 'font-mono break-all select-all' : ''}">
			{value}
		</div>
		{#if opts?.subtext}
			<div class="text-xs text-muted-foreground">{opts.subtext}</div>
		{/if}
	</div>
{/snippet}

<div class="space-y-8">
	<SettingsSection title={m.connection_edge()} description={m.connection_edge_description()} variant="plain">
		<EnvironmentConnectionDetails {environment} {currentStatus} />
	</SettingsSection>

	{#if showAgentSecurity}
		<SettingsSection title={m.environments_agent_mtls_section_title()} description={m.environments_agent_mtls_description()}>
			{#if mtlsCertificateBadge && environment.edgeMTLSCertificate}
				<div class="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2 lg:grid-cols-3">
					{@render badgeTile(
						m.environments_edge_mtls_certificate_status_label(),
						mtlsCertificateBadge.text,
						mtlsCertificateBadge.variant
					)}
					{@render tile(
						m.environments_edge_mtls_certificate_expires_label(),
						environment.edgeMTLSCertificate.expiresAt
							? formatDateTimeShort(environment.edgeMTLSCertificate.expiresAt) || m.common_unknown()
							: '—',
						{
							subtext:
								environment.edgeMTLSCertificate.daysRemaining !== undefined
									? m.environments_edge_mtls_certificate_days_remaining({
											count: environment.edgeMTLSCertificate.daysRemaining
										})
									: undefined
						}
					)}
					{#if environment.edgeMTLSCertificate.commonName}
						{@render tile(m.environments_edge_mtls_certificate_common_name_label(), environment.edgeMTLSCertificate.commonName, {
							mono: true
						})}
					{/if}
				</div>
			{/if}

			{#if showMTLSDownloads}
				<SettingsRow
					label={m.environments_agent_mtls_downloads_label()}
					description={m.environments_agent_mtls_downloads_description()}
					layout="wide"
				>
					<div class="flex flex-wrap items-center gap-2">
						<ArcaneButton
							action="base"
							tone="outline"
							href={mtlsBundleDownloadHref}
							rel="external"
							icon={DownloadIcon}
							customLabel={m.environments_agent_mtls_download_bundle()}
						/>
						<ArcaneButton
							action="base"
							tone="outline"
							href={mtlsCertificateDownloadHref}
							rel="external"
							icon={DownloadIcon}
							customLabel={m.environments_agent_mtls_download_certificate()}
						/>
						<ArcaneButton
							action="base"
							tone="outline"
							href={mtlsKeyDownloadHref}
							rel="external"
							icon={DownloadIcon}
							customLabel={m.environments_agent_mtls_download_key()}
						/>
					</div>
				</SettingsRow>
			{/if}

			<SettingsRow
				label={m.environments_regenerate_api_key()}
				description={m.environments_regenerate_dialog_message()}
				layout="switch"
			>
				<ArcaneButton
					action="base"
					tone="outline"
					onclick={onRegenerateApiKey}
					disabled={isRegeneratingKey}
					loading={isRegeneratingKey}
					icon={ResetIcon}
					customLabel={m.environments_regenerate_api_key()}
				/>
			</SettingsRow>
		</SettingsSection>
	{/if}
</div>

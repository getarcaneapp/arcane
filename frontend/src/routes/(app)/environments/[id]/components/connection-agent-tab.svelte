<script lang="ts">
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { DownloadIcon, ResetIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';

	import EnvironmentConnectionDetails from './environment-connection-details.svelte';
	import type { ConnectionAgentTabProps } from './tab-props';

	let { environment, currentStatus, showMTLSDownloads, isRegeneratingKey, onRegenerateApiKey }: ConnectionAgentTabProps =
		$props();

	let mtlsBundleDownloadHref = $derived(`/api/environments/${environment.id}/deployment/mtls/bundle`);
	let mtlsCertificateDownloadHref = $derived(`/api/environments/${environment.id}/deployment/mtls/agent.crt`);
	let mtlsKeyDownloadHref = $derived(`/api/environments/${environment.id}/deployment/mtls/agent.key`);

	let mtlsCertificateBadge = $derived.by((): { text: string; variant: 'green' | 'amber' | 'red' } | null => {
		const cert = environment.agentMTLSCertificate;
		if (!cert) return null;
		if (cert.expired) {
			return { text: m.expired(), variant: 'red' };
		}
		if (cert.expiringSoon) {
			return { text: m.environments_agent_mtls_certificate_status_expiring_soon(), variant: 'amber' };
		}
		return { text: m.environments_agent_mtls_certificate_status_valid(), variant: 'green' };
	});
</script>

<div class="space-y-8">
	<EnvironmentConnectionDetails {environment} {currentStatus} />

	<section class="flex flex-col gap-4">
		<h2 class="px-1 text-base font-semibold">{m.environments_agent_security_title()}</h2>
		<div class="flex flex-col divide-y divide-border/50 px-1">
			{#if mtlsCertificateBadge && environment.agentMTLSCertificate}
				{@const cert = environment.agentMTLSCertificate}
				<div class="flex flex-wrap items-center justify-between gap-4 py-5 first:pt-0 last:pb-0">
					<div class="min-w-0">
						<div class="flex items-center gap-2">
							<span class="text-sm font-semibold">{m.environments_agent_mtls_certificate_status_label()}</span>
							<Badge variant={mtlsCertificateBadge.variant} size="sm" hoverEffect="none">{mtlsCertificateBadge.text}</Badge>
						</div>
						{#if cert.commonName}
							<div class="mt-1 font-mono text-xs break-all text-muted-foreground select-all">{cert.commonName}</div>
						{/if}
					</div>
					{#if cert.expiresAt}
						<div class="text-right text-xs">
							<div class="text-muted-foreground">
								{m.environments_agent_mtls_certificate_expires_on({
									date: formatDateTimeShort(cert.expiresAt) || m.common_unknown()
								})}
							</div>
							{#if cert.daysRemaining !== undefined}
								<div class="font-medium">
									{m.environments_agent_mtls_certificate_days_remaining({ count: cert.daysRemaining })}
								</div>
							{/if}
						</div>
					{/if}
				</div>
			{/if}

			{#if showMTLSDownloads}
				<div class="flex flex-wrap items-center justify-between gap-4 py-5 first:pt-0 last:pb-0">
					<div class="min-w-0">
						<div class="text-sm font-semibold">{m.environments_agent_mtls_downloads_label()}</div>
						<p class="text-xs text-muted-foreground">{m.environments_agent_mtls_downloads_description()}</p>
					</div>
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
				</div>
			{/if}

			<div class="flex flex-wrap items-center justify-between gap-4 py-5 first:pt-0 last:pb-0">
				<div class="min-w-0">
					<div class="text-sm font-semibold">{m.environments_regenerate_api_key()}</div>
					<p class="text-xs text-muted-foreground">{m.environments_regenerate_api_key_description()}</p>
				</div>
				<ArcaneButton
					action="base"
					tone="outline-destructive"
					onclick={onRegenerateApiKey}
					disabled={isRegeneratingKey}
					loading={isRegeneratingKey}
					icon={ResetIcon}
					customLabel={m.environments_regenerate_api_key()}
				/>
			</div>
		</div>
	</section>
</div>

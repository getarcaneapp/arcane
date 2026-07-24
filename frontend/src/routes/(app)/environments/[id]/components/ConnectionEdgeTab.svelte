<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge';
	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import EnvironmentConnectionDetails from './EnvironmentConnectionDetails.svelte';
	import { m } from '#lib/paraglide/messages';
	import { ConnectionIcon, DownloadIcon, ResetIcon, SecurityIcon } from '#lib/icons';
	import { formatDateTimeShort } from '#lib/utils/formatting';
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

	function formatDateTime(value?: string): string {
		if (!value) return m.common_never();

		const date = new Date(value);
		if (Number.isNaN(date.getTime())) {
			return m.common_unknown();
		}

		return formatDateTimeShort(date);
	}
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

<div class="space-y-6">
	<Card.Root class="flex flex-col">
		<Card.Header icon={ConnectionIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.connection_edge()}</h2>
				</Card.Title>
				<Card.Description>{m.connection_edge_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="p-4">
			<EnvironmentConnectionDetails {environment} {currentStatus} />
		</Card.Content>
	</Card.Root>

	{#if showAgentSecurity}
		<Card.Root class="flex flex-col">
			<Card.Header icon={SecurityIcon}>
				<div class="flex flex-col space-y-1.5">
					<Card.Title>
						<h2>{m.environments_agent_mtls_section_title()}</h2>
					</Card.Title>
					<Card.Description>{m.environments_agent_mtls_description()}</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="space-y-6 p-4">
				{#if mtlsCertificateBadge && environment.edgeMTLSCertificate}
					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
						{@render badgeTile(
							m.environments_edge_mtls_certificate_status_label(),
							mtlsCertificateBadge.text,
							mtlsCertificateBadge.variant
						)}
						{@render tile(
							m.environments_edge_mtls_certificate_expires_label(),
							environment.edgeMTLSCertificate.expiresAt ? formatDateTime(environment.edgeMTLSCertificate.expiresAt) : '—',
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
							{@render tile(
								m.environments_edge_mtls_certificate_common_name_label(),
								environment.edgeMTLSCertificate.commonName,
								{ mono: true }
							)}
						{/if}
					</div>
				{/if}

				{#if showMTLSDownloads}
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
				{/if}

				<div class={showMTLSDownloads || mtlsCertificateBadge ? 'border-t pt-6' : ''}>
					<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
						<div class="space-y-0.5">
							<h3 class="text-sm font-medium">{m.environments_regenerate_api_key()}</h3>
							<p class="text-xs text-muted-foreground">{m.environments_regenerate_dialog_message()}</p>
						</div>
						<ArcaneButton
							action="base"
							tone="outline"
							onclick={onRegenerateApiKey}
							disabled={isRegeneratingKey}
							loading={isRegeneratingKey}
							icon={ResetIcon}
							customLabel={m.environments_regenerate_api_key()}
							class="shrink-0"
						/>
					</div>
				</div>
			</Card.Content>
		</Card.Root>
	{/if}
</div>

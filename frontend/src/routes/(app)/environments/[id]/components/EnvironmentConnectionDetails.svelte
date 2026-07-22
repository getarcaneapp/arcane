<script lang="ts">
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge, type BadgeVariant } from '$lib/components/ui/badge';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import type { Environment, EnvironmentStatus } from '$lib/types/environment';
	import { formatDateTimeShort } from '$lib/utils/formatting';

	let {
		environment,
		currentStatus
	}: {
		environment: Environment;
		currentStatus: EnvironmentStatus;
	} = $props();

	let controlPlaneBadge = $derived.by((): { text: string; variant: 'blue' | 'green' | 'gray' } | null => {
		if (!environment.isEdge || !environment.lastPollAt) {
			return null;
		}

		if (environment.connected) {
			return { text: m.environments_edge_polling_active(), variant: 'green' };
		}

		if (currentStatus === 'standby') {
			return { text: m.environments_edge_polling_standby(), variant: 'blue' };
		}

		return { text: m.environments_edge_polling_inactive(), variant: 'gray' };
	});

	let tunnelBadge = $derived.by((): { text: string; variant: 'green' | 'blue' | 'gray' | 'amber' | 'red' } => {
		if (environment.connected) {
			return { text: m.environments_edge_tunnel_transmitting(), variant: 'green' };
		}
		if (currentStatus === 'standby') {
			return { text: m.environments_edge_tunnel_dormant(), variant: 'gray' };
		}
		if (currentStatus === 'pending') {
			return { text: m.environments_edge_tunnel_negotiating(), variant: 'amber' };
		}
		return { text: m.disconnected(), variant: 'red' };
	});

	let tunnelTypeBadge = $derived.by((): { text: string; variant: 'blue' | 'purple' | 'gray' } | null => {
		if (!environment.lastPollAt) {
			return null;
		}

		if (environment.edgeTransport === 'websocket') {
			return { text: 'WebSocket', variant: 'purple' };
		}

		if (environment.edgeTransport === 'grpc') {
			return { text: 'gRPC', variant: 'blue' };
		}

		return { text: m.inactive(), variant: 'gray' };
	});

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
	<Card.Root variant="subtle">
		<Card.Content class="flex flex-col gap-1.5 p-4">
			<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{label}</div>
			<div><Badge {variant} minWidth="20">{text}</Badge></div>
		</Card.Content>
	</Card.Root>
{/snippet}

{#snippet tile(label: string, value: string, opts?: { mono?: boolean; subtext?: string })}
	<Card.Root variant="subtle">
		<Card.Content class="flex flex-col gap-1 p-4">
			<div class="text-xs font-semibold tracking-wide text-muted-foreground uppercase">{label}</div>
			<div class={cn('text-sm font-medium text-foreground', opts?.mono && 'font-mono break-all select-all')}>
				{value}
			</div>
			{#if opts?.subtext}
				<div class="text-xs text-muted-foreground">{opts.subtext}</div>
			{/if}
		</Card.Content>
	</Card.Root>
{/snippet}

<div class="space-y-3">
	<div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
		{#if controlPlaneBadge}
			{@render badgeTile(m.environments_edge_control_plane_label(), controlPlaneBadge.text, controlPlaneBadge.variant)}
		{/if}
		{@render badgeTile(m.environments_edge_live_tunnel_label(), tunnelBadge.text, tunnelBadge.variant)}
		{#if tunnelTypeBadge}
			{@render badgeTile(m.environments_edge_tunnel_type_label(), tunnelTypeBadge.text, tunnelTypeBadge.variant)}
		{/if}
		{@render tile(m.environments_edge_connected_since_label(), formatDateTime(environment.connectedAt))}
		{@render tile(m.environments_edge_last_heartbeat_label(), formatDateTime(environment.lastHeartbeat))}
		{#if controlPlaneBadge}
			{@render tile(m.environments_edge_last_poll_label(), formatDateTime(environment.lastPollAt))}
		{/if}
	</div>

	{#if mtlsCertificateBadge && environment.edgeMTLSCertificate}
		<div class="space-y-4 border-t pt-6">
			<div class="space-y-1">
				<h3 class="text-sm font-medium">{m.environments_agent_mtls_section_title()}</h3>
				<p class="text-xs text-muted-foreground">{m.environments_agent_mtls_description()}</p>
			</div>

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
					{@render tile(m.environments_edge_mtls_certificate_common_name_label(), environment.edgeMTLSCertificate.commonName, {
						mono: true
					})}
				{/if}
			</div>
		</div>
	{/if}
</div>

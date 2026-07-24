<script lang="ts">
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge';
	import { cn } from '#lib/utils';
	import { m } from '#lib/paraglide/messages';
	import type { Environment, EnvironmentStatus } from '#lib/types/environment';
	import { formatDateTimeShort } from '#lib/utils/formatting';

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
		<div class={cn('text-sm font-medium text-foreground', opts?.mono && 'font-mono break-all select-all')}>
			{value}
		</div>
		{#if opts?.subtext}
			<div class="text-xs text-muted-foreground">{opts.subtext}</div>
		{/if}
	</div>
{/snippet}

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

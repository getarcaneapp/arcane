<script lang="ts">
	import { createSubscriber } from 'svelte/reactivity';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import {
		AlertTriangleIcon,
		ArrowDownIcon,
		ConnectionIcon,
		EnvironmentsIcon,
		type IconType,
		LockIcon,
		RemoteEnvironmentIcon,
		ShieldCheckIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { Environment, EnvironmentStatus } from '#lib/types/environment.js';
	import { cn } from '#lib/utils.js';
	import {
		formatDateTimeShort,
		formatElapsedTime,
		formatRelativeTime,
		instantEpochMilliseconds,
		nowInstantString
	} from '#lib/utils/formatting.js';

	let {
		environment,
		currentStatus
	}: {
		environment: Environment;
		currentStatus: EnvironmentStatus;
	} = $props();

	// Heartbeats arrive every 30s, so three missed ones means the tunnel is struggling.
	const staleHeartbeatMs = 90_000;

	// Relative times keep ticking between data refreshes; 10s keeps second-level labels close without a per-second render.
	const subscribeToClock = createSubscriber((update) => {
		const timer = setInterval(() => {
			if (!document.hidden) update();
		}, 10_000);
		return () => clearInterval(timer);
	});
	let now = $derived.by(() => {
		subscribeToClock();
		return nowInstantString();
	});

	// Edge agents are live while their tunnel is up; direct agents while the manager can reach them.
	let live = $derived(environment.isEdge ? Boolean(environment.connected) : currentStatus === 'online');
	let directSecure = $derived(environment.apiUrl.startsWith('https://'));

	let status = $derived.by((): { label: string; dot: string; description?: string } => {
		if (live) {
			return { label: m.environments_edge_status_connected(), dot: 'bg-success' };
		}
		if (currentStatus === 'standby') {
			return {
				label: m.environments_edge_status_standby(),
				dot: 'bg-info',
				description: m.environments_edge_standby_description()
			};
		}
		if (currentStatus === 'pending') {
			return { label: m.environments_edge_status_connecting(), dot: 'bg-warning' };
		}
		return { label: m.disconnected(), dot: 'bg-destructive', description: m.environments_edge_disconnected_description() };
	});

	let heartbeatStale = $derived.by(() => {
		const heartbeat = instantEpochMilliseconds(environment.lastHeartbeat);
		const current = instantEpochMilliseconds(now);
		return heartbeat !== null && current !== null && current - heartbeat > staleHeartbeatMs;
	});

	let transportLabel = $derived.by(() => {
		const transport = environment.edgeTransport ?? environment.lastEdgeTransport;
		if (transport === 'grpc') return m.environments_edge_grpc_label();
		if (transport === 'websocket') return m.environments_edge_websocket_label();
		return undefined;
	});

	let multiplexed = $derived(environment.edgeDataPlane === 'mux');
	let polling = $derived(Boolean(environment.lastPollAt));
	let securityLabel = $derived(
		environment.edgeSecurityMode === 'mtls' ? m.environments_edge_mtls_label() : m.environments_edge_security_token()
	);
	let pathCaption = $derived(
		[
			transportLabel,
			environment.connected && (multiplexed ? m.environments_edge_data_plane_mux() : m.environments_edge_data_plane_legacy())
		]
			.filter(Boolean)
			.join(' · ')
	);

	// Command names are dotted (container.list); everything else is a protocol feature such as mux-v1.
	let features = $derived((environment.edgeCapabilities ?? []).filter((capability) => !capability.includes('.')));
	let commandCount = $derived((environment.edgeCapabilities ?? []).length - features.length);
</script>

{#snippet stat(label: string, value: string, hint?: string)}
	<div class="flex flex-col gap-0.5">
		<div class="text-2xs font-semibold tracking-wide text-muted-foreground uppercase">{label}</div>
		<div class="text-sm font-semibold break-all">{value}</div>
		{#if hint}
			<div class="text-xs text-muted-foreground">{hint}</div>
		{/if}
	</div>
{/snippet}

{#snippet endpoint(Icon: IconType, name: string)}
	<div class="flex min-w-0 shrink-0 flex-col items-center gap-1.5">
		<div class="flex size-9 items-center justify-center rounded-md border border-border">
			<Icon class="size-4" />
		</div>
		<span class="max-w-32 truncate text-xs text-muted-foreground">{name}</span>
	</div>
{/snippet}

{#snippet detail(label: string, value: string, mono = false)}
	<div class="flex flex-col gap-0.5 sm:flex-row sm:gap-6">
		<dt class="shrink-0 text-muted-foreground sm:w-32">{label}</dt>
		<dd class={cn('min-w-0', mono && 'font-mono break-all select-all')}>{value}</dd>
	</div>
{/snippet}

<div class="flex flex-col gap-6 px-1">
	<div class="flex flex-col gap-1.5">
		<div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
			<span class={cn('size-2 rounded-full', status.dot)}></span>
			<span class="text-base font-semibold">{status.label}</span>
			{#if environment.connected}
				<Badge variant={heartbeatStale ? 'amber' : 'green'} size="sm" hoverEffect="none">
					{heartbeatStale ? m.environments_edge_health_stale() : m.environments_edge_health_healthy()}
				</Badge>
				<span class="text-xs text-muted-foreground">
					{#if environment.connectedAt}
						{m.environments_edge_uptime({ duration: formatElapsedTime(environment.connectedAt, { base: now }) })}
					{/if}
					{#if environment.lastHeartbeat}
						· {m.environments_edge_heartbeat_ago({ time: formatRelativeTime(environment.lastHeartbeat, { base: now }) })}
					{/if}
				</span>
			{/if}
		</div>
		{#if status.description}
			<p class="text-sm text-muted-foreground">
				{status.description}
				{#if transportLabel}
					{m.environments_edge_last_transport({ transport: transportLabel })}
				{/if}
			</p>
		{/if}
	</div>

	<div class={cn('flex items-start gap-3', environment.isEdge ? 'flow-edge' : 'flow-direct')}>
		{@render endpoint(EnvironmentsIcon, window.location.host)}
		<div class={cn('flow-track flow-track-manager mt-4 min-w-6 flex-1', live && 'flow-track-live')}></div>
		<div class="flex shrink-0 flex-col items-center gap-1.5">
			<div class="flex size-9 items-center justify-center rounded-md border border-border">
				{#if environment.edgeSecurityMode === 'mtls'}
					<ShieldCheckIcon class="size-4" />
				{:else if environment.isEdge || directSecure}
					<LockIcon class="size-4" />
				{:else}
					<ConnectionIcon class="size-4" />
				{/if}
			</div>
			<span class="text-xs whitespace-nowrap text-muted-foreground">
				{#if environment.isEdge}
					{pathCaption || securityLabel}
				{:else}
					{directSecure ? m.environments_edge_https_label() : m.environments_edge_http_label()}
				{/if}
			</span>
		</div>
		<div class={cn('flow-track flow-track-agent mt-4 min-w-6 flex-1', live && 'flow-track-live')}></div>
		{@render endpoint(RemoteEnvironmentIcon, environment.name)}
	</div>

	{#if environment.connected}
		<div class="grid grid-cols-2 gap-x-6 gap-y-5 border-t border-border/50 pt-5 lg:grid-cols-4">
			{#if environment.edgeRoundTripMs !== undefined}
				{@render stat(
					m.environments_edge_stat_round_trip(),
					m.environments_edge_round_trip_ms({ ms: environment.edgeRoundTripMs.toFixed(1) })
				)}
			{/if}
			{@render stat(m.environments_edge_stat_open_streams(), String(environment.edgeOpenStreams ?? 0))}
			{@render stat(
				m.environments_edge_stat_mode(),
				polling ? m.environments_edge_mode_polling() : m.environments_edge_mode_persistent(),
				polling
					? m.environments_edge_mode_polling_hint({ time: formatRelativeTime(environment.lastPollAt, { base: now }) })
					: undefined
			)}
			{@render stat(m.environments_edge_stat_security(), securityLabel)}
		</div>

		<Collapsible.Root>
			<Collapsible.Trigger>
				{#snippet child({ props })}
					<button
						{...props}
						type="button"
						class="group flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
					>
						<ArrowDownIcon class="size-4 -rotate-90 transition-transform group-data-[state=open]:rotate-0" />
						{m.environments_edge_session_details()}
					</button>
				{/snippet}
			</Collapsible.Trigger>
			<Collapsible.Content>
				<dl class="mt-3 flex flex-col gap-2 text-xs">
					{@render detail(
						m.environments_edge_connected_since_label(),
						environment.connectedAt ? formatDateTimeShort(environment.connectedAt) || m.common_unknown() : m.common_never()
					)}
					{#if environment.edgeSessionId}
						{@render detail(m.environments_edge_session_id(), environment.edgeSessionId, true)}
					{/if}
					{#if environment.edgeAgentInstance}
						{@render detail(m.environments_edge_agent_instance(), environment.edgeAgentInstance, true)}
					{/if}
					{@render detail(
						m.environments_edge_features(),
						[...features, m.environments_edge_command_count({ count: commandCount })].join(' · ')
					)}
				</dl>
			</Collapsible.Content>
		</Collapsible.Root>

		{#if !multiplexed}
			<Alert.Root
				variant="warning-subtle"
				icon={AlertTriangleIcon}
				heading={m.environments_edge_legacy_title()}
				description={m.environments_edge_legacy_description()}
			/>
		{/if}
	{/if}

	{#if !environment.isEdge}
		<div class="grid grid-cols-2 gap-x-6 gap-y-5 border-t border-border/50 pt-5 lg:grid-cols-4">
			{@render stat(m.environments_api_url(), environment.apiUrl)}
			{#if environment.lastSeen}
				{@render stat(m.dashboard_all_last_seen(), formatRelativeTime(environment.lastSeen, { base: now }))}
			{/if}
		</div>
	{/if}
</div>

<style>
	/* A live tunnel carries a packet: edge agents send it agent → manager; direct agents bounce it manager → agent → manager. */
	.flow-track {
		position: relative;
		height: 2px;
		border-radius: 9999px;
		background: var(--border);
	}
	.flow-track-live.flow-track-manager {
		--packet: var(--success);
		background: color-mix(in srgb, var(--success) 35%, transparent);
	}
	.flow-track-live.flow-track-agent {
		--packet: var(--info);
		background: color-mix(in srgb, var(--info) 35%, transparent);
	}
	.flow-track-live::after {
		content: '';
		position: absolute;
		top: 50%;
		left: 0;
		width: 8px;
		height: 8px;
		border-radius: 9999px;
		background: var(--packet);
		box-shadow: 0 0 8px var(--packet);
		opacity: 0;
		transform: translate(-50%, -50%);
	}
	.flow-edge .flow-track-live.flow-track-agent::after {
		animation: edge-agent-leg 2.4s ease-in-out infinite;
	}
	.flow-edge .flow-track-live.flow-track-manager::after {
		animation: edge-manager-leg 2.4s ease-in-out infinite;
	}
	.flow-direct .flow-track-live.flow-track-manager::after {
		animation: direct-manager-leg 4.8s ease-in-out infinite;
	}
	.flow-direct .flow-track-live.flow-track-agent::after {
		animation: direct-agent-leg 4.8s ease-in-out infinite;
	}
	/* Edge: the agent leg runs first (right → left), then the manager leg. */
	@keyframes edge-agent-leg {
		0% {
			left: 100%;
			opacity: 0;
		}
		5%,
		42% {
			opacity: 1;
		}
		47%,
		100% {
			left: 0;
			opacity: 0;
		}
	}
	@keyframes edge-manager-leg {
		0%,
		50% {
			left: 100%;
			opacity: 0;
		}
		55%,
		92% {
			opacity: 1;
		}
		97%,
		100% {
			left: 0;
			opacity: 0;
		}
	}
	/* Direct: out along the manager leg then the agent leg, and back the same way. */
	@keyframes direct-manager-leg {
		0% {
			left: 0;
			opacity: 0;
		}
		3%,
		19% {
			opacity: 1;
		}
		22% {
			left: 100%;
			opacity: 0;
		}
		75% {
			left: 100%;
			opacity: 0;
		}
		78%,
		94% {
			opacity: 1;
		}
		97%,
		100% {
			left: 0;
			opacity: 0;
		}
	}
	@keyframes direct-agent-leg {
		0%,
		25% {
			left: 0;
			opacity: 0;
		}
		28%,
		44% {
			opacity: 1;
		}
		47%,
		50% {
			left: 100%;
			opacity: 0;
		}
		53%,
		69% {
			opacity: 1;
		}
		72%,
		100% {
			left: 0;
			opacity: 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.flow-track-live::after {
			animation: none;
		}
	}
</style>

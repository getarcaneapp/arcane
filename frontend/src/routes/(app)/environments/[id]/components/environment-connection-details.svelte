<script lang="ts">
	import { createSubscriber } from 'svelte/reactivity';

	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Collapsible from '#lib/components/ui/collapsible/index.js';
	import {
		ArrowDownIcon,
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

	let live = $derived(Boolean(environment.connected));

	let status = $derived.by((): { label: string; dot: string; description?: string } => {
		if (live) {
			return { label: m.environments_agent_status_connected(), dot: 'bg-success' };
		}
		if (currentStatus === 'pending') {
			return {
				label: m.environments_agent_status_connecting(),
				dot: 'bg-warning',
				description: m.environments_agent_pending_description()
			};
		}
		return { label: m.disconnected(), dot: 'bg-destructive', description: m.environments_agent_disconnected_description() };
	});

	let heartbeatStale = $derived.by(() => {
		const heartbeat = instantEpochMilliseconds(environment.lastHeartbeat);
		const current = instantEpochMilliseconds(now);
		return heartbeat !== null && current !== null && current - heartbeat > staleHeartbeatMs;
	});

	let transportLabel = $derived.by(() => {
		const transport = environment.agentTransport ?? environment.lastAgentTransport;
		if (transport === 'grpc') return m.environments_agent_grpc_label();
		if (transport === 'websocket') return m.environments_agent_websocket_label();
		return undefined;
	});

	let securityLabel = $derived(
		environment.agentSecurityMode === 'mtls' ? m.environments_agent_mtls_label() : m.environments_agent_security_token()
	);
	let pathCaption = $derived(
		[transportLabel, environment.connected && m.environments_agent_data_plane_mux()].filter(Boolean).join(' · ')
	);
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
					{heartbeatStale ? m.environments_agent_health_stale() : m.environments_agent_health_healthy()}
				</Badge>
				<span class="text-xs text-muted-foreground">
					{#if environment.connectedAt}
						{m.environments_agent_uptime({ duration: formatElapsedTime(environment.connectedAt, { base: now }) })}
					{/if}
					{#if environment.lastHeartbeat}
						· {m.environments_agent_heartbeat_ago({ time: formatRelativeTime(environment.lastHeartbeat, { base: now }) })}
					{/if}
				</span>
			{/if}
		</div>
		{#if status.description}
			<p class="text-sm text-muted-foreground">
				{status.description}
				{#if transportLabel}
					{m.environments_agent_last_transport({ transport: transportLabel })}
				{/if}
			</p>
		{/if}
	</div>

	<div class="flow-agent flex items-start gap-3">
		{@render endpoint(EnvironmentsIcon, window.location.host)}
		<div class={cn('flow-track flow-track-manager mt-4 min-w-6 flex-1', live && 'flow-track-live')}></div>
		<div class="flex shrink-0 flex-col items-center gap-1.5">
			<div class="flex size-9 items-center justify-center rounded-md border border-border">
				{#if environment.agentSecurityMode === 'mtls'}
					<ShieldCheckIcon class="size-4" />
				{:else}
					<LockIcon class="size-4" />
				{/if}
			</div>
			<span class="text-xs whitespace-nowrap text-muted-foreground">
				{pathCaption || securityLabel}
			</span>
		</div>
		<div class={cn('flow-track flow-track-agent mt-4 min-w-6 flex-1', live && 'flow-track-live')}></div>
		{@render endpoint(RemoteEnvironmentIcon, environment.name)}
	</div>

	{#if environment.connected}
		<div class="grid grid-cols-2 gap-x-6 gap-y-5 border-t border-border/50 pt-5 lg:grid-cols-4">
			{#if environment.agentRoundTripMs !== undefined}
				{@render stat(
					m.environments_agent_stat_round_trip(),
					m.environments_agent_round_trip_ms({ ms: environment.agentRoundTripMs.toFixed(1) })
				)}
			{/if}
			{@render stat(m.environments_agent_stat_open_streams(), String(environment.agentOpenStreams ?? 0))}
			{@render stat(m.environments_agent_stat_mode(), m.environments_agent_mode_persistent())}
			{@render stat(m.environments_agent_stat_security(), securityLabel)}
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
						{m.environments_agent_session_details()}
					</button>
				{/snippet}
			</Collapsible.Trigger>
			<Collapsible.Content>
				<dl class="mt-3 flex flex-col gap-2 text-xs">
					{@render detail(
						m.environments_agent_connected_since_label(),
						environment.connectedAt ? formatDateTimeShort(environment.connectedAt) || m.common_unknown() : m.common_never()
					)}
					{#if environment.agentSessionId}
						{@render detail(m.environments_agent_session_id(), environment.agentSessionId, true)}
					{/if}
					{#if environment.agentInstance}
						{@render detail(m.environments_agent_instance(), environment.agentInstance, true)}
					{/if}
					{@render detail(m.environments_agent_features(), (environment.agentCapabilities ?? []).join(' · '))}
				</dl>
			</Collapsible.Content>
		</Collapsible.Root>
	{/if}
</div>

<style>
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
	.flow-agent .flow-track-live.flow-track-agent::after {
		animation: agent-leg 2.4s ease-in-out infinite;
	}
	.flow-agent .flow-track-live.flow-track-manager::after {
		animation: manager-leg 2.4s ease-in-out infinite;
	}
	/* Agent: the agent leg runs first (right → left), then the manager leg. */
	@keyframes agent-leg {
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
	@keyframes manager-leg {
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
	@media (prefers-reduced-motion: reduce) {
		.flow-track-live::after {
			animation: none;
		}
	}
</style>

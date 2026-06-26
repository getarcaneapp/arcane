<script lang="ts">
	import { formatDistance } from 'date-fns';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import { HashIcon, HealthIcon, TagIcon } from '$lib/icons';
	import type { Environment, EnvironmentStatus } from '$lib/types/environment';
	import type { AppVersionInformation } from '$lib/types/settings';

	let {
		environment,
		currentStatus,
		isLoadingVersion,
		remoteVersion,
		versionInformation
	}: {
		environment: Environment;
		currentStatus: EnvironmentStatus;
		isLoadingVersion: boolean;
		remoteVersion: AppVersionInformation | null;
		versionInformation: AppVersionInformation | null | undefined;
	} = $props();

	let transportBadge = $derived.by((): { text: string; variant: 'blue' | 'purple' | 'gray' } => {
		if (!environment.isEdge) {
			return { text: 'HTTP', variant: 'gray' };
		}

		// Prefer the live tunnel transport; fall back to the last one used so
		// disconnected or poll-only agents still show what they connect with.
		const transport = (environment.connected ? environment.edgeTransport : undefined) ?? environment.lastEdgeTransport;
		if (!transport) {
			return { text: 'Edge', variant: 'gray' };
		}

		if (transport === 'websocket') {
			return { text: 'WebSocket', variant: 'purple' };
		}

		return { text: 'gRPC', variant: 'blue' };
	});

	let statusBadge = $derived.by((): { text: string; variant: 'green' | 'blue' | 'amber' | 'red' } => {
		switch (currentStatus) {
			case 'online':
				return { text: m.common_online(), variant: 'green' };
			case 'standby':
				return { text: m.common_standby(), variant: 'blue' };
			case 'pending':
				return { text: m.common_pending(), variant: 'amber' };
			case 'error':
				return { text: m.common_error(), variant: 'red' };
			default:
				return { text: m.common_offline(), variant: 'red' };
		}
	});

	let statusDotClass = $derived.by((): string => {
		switch (statusBadge.variant) {
			case 'green':
				return 'bg-emerald-500 shadow-[0_0_8px_var(--color-emerald-500)]';
			case 'blue':
				return 'bg-blue-500';
			case 'amber':
				return 'bg-amber-500';
			default:
				return 'bg-red-500';
		}
	});

	let localDisplayVersion = $derived(
		versionInformation?.displayVersion || versionInformation?.currentTag || versionInformation?.currentVersion || 'Unknown'
	);

	let remoteDisplayVersion = $derived(
		remoteVersion?.displayVersion || remoteVersion?.currentTag || remoteVersion?.currentVersion || ''
	);

	function formatRelative(value: string | undefined, base: number): string {
		if (!value) return m.common_never();

		const date = new Date(value);
		if (Number.isNaN(date.getTime())) {
			return m.common_unknown();
		}

		return formatDistance(date, base, { addSuffix: true });
	}

	// The relative heartbeat ("2 minutes ago") must keep ticking even when
	// lastHeartbeat stops changing (e.g. a silent agent); otherwise it would
	// freeze at the value from the last fetch. Recompute against a ticking base.
	let nowTick = $state(Date.now());

	$effect(() => {
		if (!environment.isEdge) return;
		const interval = setInterval(() => (nowTick = Date.now()), 30_000);
		return () => clearInterval(interval);
	});

	let heartbeatRelative = $derived(formatRelative(environment.lastHeartbeat, nowTick));
</script>

<div class="bg-muted/40 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-lg px-4 py-3">
	<div class="flex items-center gap-2">
		<span class={cn('size-2.5 rounded-full transition-colors', statusDotClass)}></span>
		<span class="text-sm font-semibold">{statusBadge.text}</span>
	</div>
	<StatusBadge text={transportBadge.text} variant={transportBadge.variant} />
	<div class="text-muted-foreground flex items-center gap-1.5 text-sm">
		<TagIcon class="size-4 shrink-0" />
		{#if environment.id === '0'}
			<span class="font-mono">{localDisplayVersion}</span>
			{#if versionInformation?.updateAvailable}
				<Badge variant="secondary" class="bg-amber-500/10 text-amber-600 hover:bg-amber-500/20 dark:text-amber-400">
					{m.sidebar_update_available()}: {versionInformation.newestVersion}
				</Badge>
			{/if}
		{:else if isLoadingVersion}
			<Spinner class="size-4" />
			<span>{m.common_action_checking()}</span>
		{:else if remoteVersion}
			<span class="font-mono">{remoteDisplayVersion}</span>
			{#if remoteVersion.updateAvailable}
				<Badge variant="secondary" class="bg-amber-500/10 text-amber-600 hover:bg-amber-500/20 dark:text-amber-400">
					{m.sidebar_update_available()}: {remoteVersion.newestVersion}
				</Badge>
				{#if remoteVersion.releaseUrl}
					<a
						href={remoteVersion.releaseUrl}
						target="_blank"
						rel="noopener noreferrer"
						class="text-xs text-blue-500 hover:underline"
					>
						{m.version_info_view_release()}
					</a>
				{/if}
			{/if}
		{:else if currentStatus === 'online' || currentStatus === 'standby'}
			<span>{m.environments_version_unavailable()}</span>
		{:else}
			<span>{m.common_offline()}</span>
		{/if}
	</div>
	{#if environment.isEdge && environment.lastHeartbeat}
		<div class="text-muted-foreground flex items-center gap-1.5 text-sm">
			<HealthIcon class="size-4 shrink-0" />
			<span>{heartbeatRelative}</span>
		</div>
	{/if}
	<div class="text-muted-foreground flex items-center gap-1.5 text-sm sm:ml-auto">
		<HashIcon class="size-4 shrink-0" />
		<span class="font-mono">{environment.id}</span>
	</div>
</div>

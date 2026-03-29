<script lang="ts">
	import { format } from 'date-fns';
	import { onMount } from 'svelte';
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { toast } from 'svelte-sonner';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { m } from '$lib/paraglide/messages';
	import { environmentManagementService } from '$lib/services/env-mgmt-service.js';
	import type { AppVersionInformation } from '$lib/types/application-configuration';
	import type { Environment, EnvironmentStatus } from '$lib/types/environment.type';
	import { isEnvironmentOnline, resolveEnvironmentStatus } from '$lib/utils/environment-status';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import * as Card from '$lib/components/ui/card/index.js';
	import Label from '$lib/components/ui/label/label.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { AlertIcon, ArrowLeftIcon, EnvironmentsIcon, GitBranchIcon, RefreshIcon, SettingsIcon, TestIcon } from '$lib/icons';

	let { data } = $props();
	let { environment, settings, versionInformation } = $derived(data);

	let refreshedEnvironment: Environment | null = $state(null);
	let runtimeEnvironment: Environment = $derived.by(() => {
		const refreshed = refreshedEnvironment;
		return refreshed && refreshed.id === environment.id ? refreshed : environment;
	});

	let isRefreshing = $state(false);
	let isTestingConnection = $state(false);
	let isSyncing = $state(false);
	let remoteVersion = $state<AppVersionInformation | null>(null);
	let isLoadingVersion = $state(false);
	let statusOverride = $state<EnvironmentStatus | null>(null);
	let currentStatus = $derived(resolveEnvironmentStatus(runtimeEnvironment, statusOverride));
	let isCurrentlyOnline = $derived(isEnvironmentOnline(runtimeEnvironment, statusOverride));
	let isCurrentlyStandby = $derived(currentStatus === 'standby');
	let transportBadge = $derived.by((): { text: string; variant: 'blue' | 'purple' | 'gray' } => {
		if (!runtimeEnvironment.isEdge) {
			return { text: 'HTTP', variant: 'gray' };
		}

		if (runtimeEnvironment.lastPollAt) {
			return { text: m.environments_edge_polling_label(), variant: 'blue' };
		}

		if (!runtimeEnvironment.connected || !runtimeEnvironment.edgeTransport) {
			return { text: 'Edge', variant: 'gray' };
		}

		if (runtimeEnvironment.edgeTransport === 'websocket') {
			return { text: 'WebSocket', variant: 'purple' };
		}

		return { text: 'gRPC', variant: 'blue' };
	});
	let controlPlaneBadge = $derived.by((): { text: string; variant: 'blue' | 'green' | 'gray' } | null => {
		if (!runtimeEnvironment.isEdge || !runtimeEnvironment.lastPollAt) {
			return null;
		}

		if (runtimeEnvironment.connected) {
			return { text: m.environments_edge_polling_active(), variant: 'green' };
		}

		if (currentStatus === 'standby') {
			return { text: m.environments_edge_polling_standby(), variant: 'blue' };
		}

		return { text: m.environments_edge_polling_inactive(), variant: 'gray' };
	});
	let localDisplayVersion = $derived(
		versionInformation?.displayVersion || versionInformation?.currentTag || versionInformation?.currentVersion || 'Unknown'
	);
	let remoteDisplayVersion = $derived(
		remoteVersion?.displayVersion || remoteVersion?.currentTag || remoteVersion?.currentVersion || ''
	);
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
	let tunnelBadge = $derived.by((): { text: string; variant: 'green' | 'blue' | 'gray' | 'amber' | 'red' } => {
		if (!runtimeEnvironment.isEdge) {
			return statusBadge;
		}
		if (runtimeEnvironment.connected) {
			return { text: m.environments_edge_tunnel_transmitting(), variant: 'green' };
		}
		if (currentStatus === 'standby') {
			return { text: m.environments_edge_tunnel_dormant(), variant: 'gray' };
		}
		if (currentStatus === 'pending') {
			return { text: m.environments_edge_tunnel_negotiating(), variant: 'amber' };
		}
		return { text: m.environments_edge_tunnel_disconnected(), variant: 'red' };
	});
	let tunnelTypeBadge = $derived.by((): { text: string; variant: 'blue' | 'purple' | 'gray' } | null => {
		if (!runtimeEnvironment.isEdge || !runtimeEnvironment.lastPollAt) {
			return null;
		}

		if (runtimeEnvironment.edgeTransport === 'websocket') {
			return { text: 'WebSocket', variant: 'purple' };
		}

		if (runtimeEnvironment.edgeTransport === 'grpc') {
			return { text: 'gRPC', variant: 'blue' };
		}

		return { text: m.environments_edge_tunnel_type_inactive(), variant: 'gray' };
	});

	function buildEnvironmentSettingsUrl(tab?: string): string {
		const url = new URL(page.url);
		url.pathname = '/settings/environments';
		url.searchParams.set('environment', environment.id);

		if (!tab || tab === 'general' || tab === 'details') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', tab);
		}

		return url.toString();
	}

	$effect(() => {
		const tab = page.url.searchParams.get('tab');
		if (!tab || tab === 'general' || tab === 'details') return;

		if (tab === 'gitops') {
			goto(`/environments/${environment.id}/gitops`, { replaceState: true });
			return;
		}

		goto(buildEnvironmentSettingsUrl(tab), { replaceState: true });
	});

	$effect(() => {
		if (environment.id !== '0' && isCurrentlyOnline && !remoteVersion && !isLoadingVersion) {
			fetchVersion();
		}
	});

	onMount(() => {
		if (environment.isEdge) {
			void refreshRuntimeEnvironment();
		}

		const interval = window.setInterval(() => {
			if (!environment.isEdge) return;
			void refreshRuntimeEnvironment();
		}, 5000);

		return () => window.clearInterval(interval);
	});

	async function refreshRuntimeEnvironment() {
		try {
			const latestEnvironment = await environmentManagementService.get(environment.id);
			if (latestEnvironment.id === environment.id) {
				refreshedEnvironment = latestEnvironment;
			}
		} catch (error) {
			console.debug('Failed to refresh environment runtime state:', error);
		}
	}

	async function fetchVersion() {
		try {
			isLoadingVersion = true;
			remoteVersion = await environmentManagementService.getVersion(environment.id);
		} catch (error) {
			console.error('Failed to fetch environment version:', error);
		} finally {
			isLoadingVersion = false;
		}
	}

	async function refreshEnvironment() {
		if (isRefreshing) return;

		try {
			isRefreshing = true;
			statusOverride = null;
			remoteVersion = null;
			await invalidateAll();
		} catch (error) {
			console.error('Failed to refresh environment:', error);
			toast.error(m.common_refresh_failed({ resource: m.resource_environment() }));
		} finally {
			isRefreshing = false;
		}
	}

	async function syncEnvironment() {
		if (isSyncing) return;

		try {
			isSyncing = true;
			await environmentManagementService.sync(environment.id);
			toast.success(m.sync_environment_success());
		} catch (error) {
			console.error('Failed to sync environment:', error);
			toast.error(m.sync_environment_failed());
		} finally {
			isSyncing = false;
		}
	}

	async function testConnection() {
		if (isTestingConnection) return;

		try {
			isTestingConnection = true;
			const result = await environmentManagementService.testConnection(environment.id);
			statusOverride = environment.isEdge ? null : (result.status as EnvironmentStatus);

			if (result.status === 'online') {
				toast.success(m.environments_test_connection_success());
			} else {
				toast.error(m.environments_test_connection_error());
			}

			await invalidateAll();
		} catch (error) {
			statusOverride = environment.isEdge ? null : 'offline';
			toast.error(m.environments_test_connection_failed());
			console.error(error);
		} finally {
			isTestingConnection = false;
		}
	}

	function formatDateTime(value?: string): string {
		if (!value) return m.common_never();

		const date = new Date(value);
		if (Number.isNaN(date.getTime())) {
			return m.common_unknown();
		}

		return format(date, 'PP p');
	}
</script>

<div class="container mx-auto max-w-full space-y-6 overflow-hidden p-2 sm:p-6">
	<div class="space-y-3 sm:space-y-4">
		<ArcaneButton
			action="base"
			tone="ghost"
			onclick={() => goto('/environments')}
			class="w-fit gap-2"
			icon={ArrowLeftIcon}
			customLabel={m.common_back_to({ resource: m.environments_title() })}
		/>

		<div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
			<div class="flex-1">
				<h1 class="text-xl font-bold wrap-break-word sm:text-2xl">{environment.name}</h1>
				<p class="text-muted-foreground mt-1.5 text-sm wrap-break-word sm:text-base">{m.environments_page_subtitle()}</p>
			</div>

			<div class="flex flex-wrap items-center gap-2">
				<ArcaneButton
					action="base"
					tone="outline"
					onclick={() => goto(buildEnvironmentSettingsUrl())}
					icon={SettingsIcon}
					customLabel={m.settings_title()}
				/>
				<ArcaneButton
					action="base"
					tone="outline"
					onclick={() => goto(`/environments/${environment.id}/gitops`)}
					icon={GitBranchIcon}
					customLabel={m.git_syncs_title()}
				/>
				{#if environment.id !== '0'}
					<ArcaneButton
						action="base"
						tone="outline"
						onclick={syncEnvironment}
						disabled={isSyncing}
						loading={isSyncing}
						icon={RefreshIcon}
						customLabel={m.sync_environment()}
					/>
				{/if}
				<ArcaneButton
					action="refresh"
					tone="outline"
					onclick={refreshEnvironment}
					disabled={isRefreshing}
					loading={isRefreshing}
				/>
			</div>
		</div>

		{#if environment.enabled && settings && isCurrentlyStandby}
			<div
				class="flex items-start gap-3 rounded-lg border border-blue-500/30 bg-blue-500/10 p-4 text-blue-900 dark:text-blue-200"
			>
				<AlertIcon class="mt-0.5 size-5 shrink-0 text-blue-600 dark:text-blue-400" />
				<div class="flex-1 space-y-1">
					<p class="text-sm font-medium">{m.common_status()}: {m.common_standby()}</p>
				</div>
			</div>
		{:else if !environment.enabled || !isCurrentlyOnline || !settings}
			<div
				class="flex items-start gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-4 text-amber-900 dark:text-amber-200"
			>
				<AlertIcon class="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
				<div class="flex-1 space-y-1">
					<p class="text-sm font-medium">
						{#if !environment.enabled}
							{m.environments_warning_disabled()}
						{:else if !isCurrentlyOnline}
							{m.common_status()}: {currentStatus === 'pending'
								? m.common_pending()
								: currentStatus === 'error'
									? m.common_error()
									: m.common_offline()}
						{:else if !settings}
							{m.environments_warning_no_settings()}
						{/if}
					</p>
				</div>
			</div>
		{/if}
	</div>

	<Card.Root class="flex flex-col">
		<Card.Header icon={EnvironmentsIcon}>
			<div class="flex flex-col space-y-1.5">
				<Card.Title>
					<h2>{m.environments_overview_title()}</h2>
				</Card.Title>
				<Card.Description>{m.environments_basic_info_description()}</Card.Description>
			</div>
		</Card.Header>
		<Card.Content class="space-y-4 p-4">
			<div class="grid gap-4 rounded-lg border p-4 sm:grid-cols-2">
				<div>
					<Label class="text-muted-foreground text-xs font-medium">{m.common_name()}</Label>
					<div class="mt-1 text-sm font-medium">{runtimeEnvironment.name}</div>
				</div>
				<div>
					<Label class="text-muted-foreground text-xs font-medium">{m.common_enabled()}</Label>
					<div class="mt-1">
						<StatusBadge
							text={runtimeEnvironment.enabled ? m.common_enabled() : m.common_disabled()}
							variant={runtimeEnvironment.enabled ? 'green' : 'red'}
						/>
					</div>
				</div>
				<div class="sm:col-span-2">
					<div class="flex flex-wrap items-start justify-between gap-3">
						<div class="min-w-0">
							<Label class="text-muted-foreground text-xs font-medium">{m.environments_api_url()}</Label>
							<div class="mt-1 font-mono text-sm break-all">{runtimeEnvironment.apiUrl}</div>
							<p class="text-muted-foreground mt-1.5 text-xs">{m.environments_api_url_help()}</p>
						</div>
						<ArcaneButton
							action="base"
							icon={TestIcon}
							onclick={testConnection}
							disabled={isTestingConnection}
							loading={isTestingConnection}
							customLabel={m.environments_test_connection()}
							loadingLabel={m.environments_testing_connection()}
							class="shrink-0"
						/>
					</div>
				</div>
			</div>

			<div class="grid grid-cols-2 gap-4 rounded-lg border p-4">
				<div>
					<Label class="text-muted-foreground text-xs font-medium">{m.environments_environment_id_label()}</Label>
					<div class="mt-1 font-mono text-sm">{runtimeEnvironment.id}</div>
				</div>
				<div>
					<Label class="text-muted-foreground text-xs font-medium">{m.common_status()}</Label>
					<div class="mt-1">
						<StatusBadge text={statusBadge.text} variant={statusBadge.variant} />
					</div>
				</div>
				<div>
					<Label class="text-muted-foreground text-xs font-medium">{m.common_type()}</Label>
					<div class="mt-1">
						<StatusBadge text={transportBadge.text} variant={transportBadge.variant} />
					</div>
				</div>
				{#if runtimeEnvironment.isEdge}
					{#if controlPlaneBadge}
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.environments_edge_control_plane_label()}</Label>
							<div class="mt-1">
								<StatusBadge text={controlPlaneBadge.text} variant={controlPlaneBadge.variant} />
							</div>
						</div>
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.environments_edge_last_poll_label()}</Label>
							<div class="mt-1 font-mono text-sm">{formatDateTime(runtimeEnvironment.lastPollAt)}</div>
						</div>
					{/if}
					<div>
						<Label class="text-muted-foreground text-xs font-medium">{m.environments_edge_live_tunnel_label()}</Label>
						<div class="mt-1">
							<StatusBadge text={tunnelBadge.text} variant={tunnelBadge.variant} />
						</div>
					</div>
					{#if tunnelTypeBadge}
						<div>
							<Label class="text-muted-foreground text-xs font-medium">{m.environments_edge_tunnel_type_label()}</Label>
							<div class="mt-1">
								<StatusBadge text={tunnelTypeBadge.text} variant={tunnelTypeBadge.variant} />
							</div>
						</div>
					{/if}
					<div>
						<Label class="text-muted-foreground text-xs font-medium">{m.environments_edge_connected_since_label()}</Label>
						<div class="mt-1 font-mono text-sm">{formatDateTime(runtimeEnvironment.connectedAt)}</div>
					</div>
					<div>
						<Label class="text-muted-foreground text-xs font-medium">{m.environments_edge_last_heartbeat_label()}</Label>
						<div class="mt-1 font-mono text-sm">{formatDateTime(runtimeEnvironment.lastHeartbeat)}</div>
					</div>
				{/if}
				<div class="col-span-2 border-t pt-4">
					<Label class="text-muted-foreground text-xs font-medium">{m.version_info_version()}</Label>
					<div class="mt-1 flex items-center gap-2">
						{#if runtimeEnvironment.id === '0'}
							<span class="font-mono text-sm">{localDisplayVersion}</span>
							{#if versionInformation?.updateAvailable}
								<Badge variant="secondary" class="bg-amber-500/10 text-amber-600 hover:bg-amber-500/20 dark:text-amber-400">
									{m.sidebar_update_available()}: {versionInformation.newestVersion}
								</Badge>
							{/if}
						{:else if isLoadingVersion}
							<Spinner />
							<span class="text-muted-foreground text-sm">{m.common_action_checking()}</span>
						{:else if remoteVersion}
							<span class="font-mono text-sm">{remoteDisplayVersion}</span>
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
							<span class="text-muted-foreground text-sm">{m.environments_version_unavailable()}</span>
						{:else}
							<span class="text-muted-foreground text-sm">{m.common_offline()}</span>
						{/if}
					</div>
				</div>
			</div>
		</Card.Content>
	</Card.Root>
</div>

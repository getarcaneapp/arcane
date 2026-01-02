<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { invalidateAll } from '$app/navigation';
	import ActionButtons from '$lib/components/action-buttons.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { format } from 'date-fns';
	import bytes from 'bytes';
	import { onDestroy, untrack } from 'svelte';
	import { page } from '$app/state';
	import type {
		ContainerDetailsDto,
		ContainerNetworkSettings,
		ContainerStats as ContainerStatsType
	} from '$lib/types/container.type';
	import { m } from '$lib/paraglide/messages';
	import TabbedPageLayout from '$lib/layouts/tabbed-page-layout.svelte';
	import { type TabItem } from '$lib/components/tab-bar/index.js';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import ContainerOverview from '../components/ContainerOverview.svelte';
	import ContainerStats from '../components/ContainerStats.svelte';
	import ContainerConfiguration from '../components/ContainerConfiguration.svelte';
	import ContainerNetwork from '../components/ContainerNetwork.svelte';
	import ContainerStorage from '../components/ContainerStorage.svelte';
	import ContainerLogsPanel from '../components/ContainerLogsPanel.svelte';
	import ContainerShell from '../components/ContainerShell.svelte';
	import { createContainerStatsWebSocket, type ReconnectingWebSocket } from '$lib/utils/ws';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import {
		ArrowLeftIcon,
		AlertIcon,
		RefreshIcon,
		VolumesIcon,
		FileTextIcon,
		SettingsIcon,
		NetworksIcon,
		TerminalIcon,
		ContainersIcon,
		StatsIcon
	} from '$lib/icons';

	let { data } = $props();
	let container = $derived(data?.container as ContainerDetailsDto);
	let stats = $state(null as ContainerStatsType | null);

	let starting = $state(false);
	let stopping = $state(false);
	let restarting = $state(false);
	let removing = $state(false);
	let isRefreshing = $state(false);

	let selectedTab = $state<string>('overview');
	let autoScrollLogs = $state(true);
	let isStreaming = $state(false);

	let statsWebSocket: ReconnectingWebSocket<any> | null = $state(null);
	let isConnecting = $state(false);
	let hasInitialStatsLoaded = $state(false);
	let statsStreamEnabled = $state(false);

	const cleanContainerName = (name: string | undefined): string => {
		if (!name) return m.common_not_found_title({ resource: m.containers_title() });
		return name.replace(/^\/+/, '');
	};

	const containerDisplayName = $derived(cleanContainerName(container?.name));

	async function startStatsStream() {
		if (isConnecting || statsWebSocket || !container?.id || !container.state?.running) {
			return;
		}

		isConnecting = true;
		statsStreamEnabled = true;
		try {
			const envId = await environmentStore.getCurrentEnvironmentId();

			const ws = createContainerStatsWebSocket({
				getEnvId: () => envId,
				containerId: container.id,
				onMessage: (statsData) => {
					if (statsData.removed) {
						invalidateAll();
						return;
					}
					stats = statsData;
					hasInitialStatsLoaded = true;
				},
				onOpen: () => {
					isConnecting = false;
				},
				onError: (err) => {
					console.error('Stats WebSocket error:', err);
					isConnecting = false;
				},
				onClose: () => {
					isConnecting = false;
				},
				maxBackoff: 5000,
				shouldReconnect: () => {
					return statsStreamEnabled && container?.state?.running === true;
				}
			});

			ws.connect();
			statsWebSocket = ws;
		} catch (error) {
			console.error('Failed to connect to stats stream:', error);
			isConnecting = false;
		}
	}

	function closeStatsStream() {
		statsStreamEnabled = false;
		if (statsWebSocket) {
			statsWebSocket.close();
			statsWebSocket = null;
		}
		isConnecting = false;
		hasInitialStatsLoaded = false;
	}

	$effect(() => {
		const isStatsTab = selectedTab === 'stats';

		untrack(() => {
			const containerRunning = container?.state?.running;
			const hasWebSocket = !!statsWebSocket;

			if (isStatsTab && containerRunning && !hasWebSocket) {
				void startStatsStream();
			} else if (!isStatsTab && hasWebSocket) {
				closeStatsStream();
			}
		});
	});

	onDestroy(() => {
		closeStatsStream();
	});

	const calculateCPUPercent = (statsData: ContainerStatsType | null): number => {
		if (!statsData || !statsData.cpu_stats || !statsData.precpu_stats) {
			return 0;
		}

		const cpuDelta = statsData.cpu_stats.cpu_usage.total_usage - (statsData.precpu_stats.cpu_usage?.total_usage || 0);
		const systemDelta = statsData.cpu_stats.system_cpu_usage - (statsData.precpu_stats.system_cpu_usage || 0);

		if (systemDelta > 0 && cpuDelta > 0) {
			const cpuPercent = (cpuDelta / systemDelta) * 100.0;
			return Math.min(Math.max(cpuPercent, 0), 100);
		}
		return 0;
	};

	const cpuUsagePercent = $derived(calculateCPUPercent(stats));

	const cpuLimit = $derived.by(() => {
		if (container?.hostConfig?.nanoCpus) {
			return container.hostConfig.nanoCpus / 1e9;
		}
		return stats?.cpu_stats?.online_cpus || 0;
	});
	const memoryUsageBytes = $derived(stats?.memory_stats?.usage || 0);
	const memoryLimitBytes = $derived(stats?.memory_stats?.limit || 0);
	const memoryUsageFormatted = $derived(bytes.format(memoryUsageBytes || 0) || '0 B');
	const memoryLimitFormatted = $derived(bytes.format(memoryLimitBytes || 0) || '0 B');
	const memoryUsagePercent = $derived(memoryLimitBytes > 0 ? (memoryUsageBytes / memoryLimitBytes) * 100 : 0);

	const getPrimaryIpAddress = (networkSettings: ContainerNetworkSettings | undefined | null): string => {
		if (!networkSettings?.networks) return 'N/A';

		for (const networkName in networkSettings.networks) {
			const net = networkSettings.networks[networkName];
			if (net?.ipAddress) return net.ipAddress;
		}
		return 'N/A';
	};

	const primaryIpAddress = $derived(getPrimaryIpAddress(container?.networkSettings));

	$effect(() => {
		starting = false;
		stopping = false;
		restarting = false;
		removing = false;
	});

	async function refreshData() {
		isRefreshing = true;
		await invalidateAll();
		setTimeout(() => {
			isRefreshing = false;
		}, 500);
	}

	function handleLogStart() {
		isStreaming = true;
	}

	function handleLogStop() {
		isStreaming = false;
	}

	function handleLogClear() {
		invalidateAll();
	}

	function handleToggleAutoScroll() {}

	const hasEnvVars = $derived(!!(container?.config?.env && container.config.env.length > 0));
	const hasPorts = $derived(!!(container?.ports && container.ports.length > 0));
	const hasLabels = $derived(!!(container?.labels && Object.keys(container.labels).length > 0));
	const showConfiguration = $derived(hasEnvVars || hasPorts || hasLabels);

	const hasNetworks = $derived(
		!!(container?.networkSettings?.networks && Object.keys(container.networkSettings.networks).length > 0)
	);
	const hasMounts = $derived(!!(container?.mounts && container.mounts.length > 0));
	const showStats = $derived(!!container?.state?.running);
	const showShell = $derived(!!container?.state?.running);

	const tabItems = $derived<TabItem[]>([
		{ value: 'overview', label: m.common_overview(), icon: ContainersIcon },
		...(showStats ? [{ value: 'stats', label: m.containers_nav_metrics(), icon: StatsIcon }] : []),
		{ value: 'logs', label: m.containers_nav_logs(), icon: FileTextIcon },
		...(showShell ? [{ value: 'shell', label: m.common_shell(), icon: TerminalIcon }] : []),
		...(showConfiguration ? [{ value: 'config', label: m.common_configuration(), icon: SettingsIcon }] : []),
		...(hasNetworks ? [{ value: 'network', label: m.containers_nav_networks(), icon: NetworksIcon }] : []),
		...(hasMounts ? [{ value: 'storage', label: m.containers_nav_storage(), icon: VolumesIcon }] : [])
	]);

	$effect(() => {
		if (!tabItems.some((t) => t.value === selectedTab)) {
			selectedTab = tabItems[0]?.value ?? 'overview';
		}
	});

	function onTabChange(value: string) {
		selectedTab = value;
	}

	function parseDockerDate(input: string | Date | undefined | null): Date | null {
		if (!input) return null;
		if (input instanceof Date) return isNaN(input.getTime()) ? null : input;

		const s = String(input).trim();
		if (!s || s.startsWith('0001-01-01')) return null;

		const m = s.match(/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(\.\d+)?Z$/);
		let normalized = s;
		if (m) {
			const base = m[1];
			const frac = m[2] ? m[2].slice(1) : '';
			const ms = frac ? '.' + frac.slice(0, 3).padEnd(3, '0') : '';
			normalized = `${base}${ms}Z`;
		}

		const d = new Date(normalized);
		return isNaN(d.getTime()) ? null : d;
	}

	function formatDockerDate(input: string | Date | undefined | null, fmt = 'PP p'): string {
		const d = parseDockerDate(input);
		return d ? format(d, fmt) : 'N/A';
	}

	const backUrl = $derived.by(() => {
		const from = page.url.searchParams.get('from');
		const projectId = page.url.searchParams.get('projectId');

		if (from === 'project' && projectId) {
			return `/projects/${projectId}`;
		}

		return '/containers';
	});
</script>

{#if container}
	<TabbedPageLayout {backUrl} backLabel={m.common_back()} {tabItems} {selectedTab} {onTabChange}>
		{#snippet headerInfo()}
			<div class="flex items-center gap-2">
				<h1 class="max-w-[300px] truncate text-lg font-semibold" title={containerDisplayName}>
					{containerDisplayName}
				</h1>
				{#if container?.state}
					<StatusBadge
						variant={container.state.status === 'running' ? 'green' : container.state.status === 'exited' ? 'red' : 'amber'}
						text={container.state.status}
					/>
				{/if}
			</div>
		{/snippet}

		{#snippet headerActions()}
			<ActionButtons
				id={container.id}
				name={containerDisplayName}
				type="container"
				itemState={container.state?.running ? 'running' : 'stopped'}
				desktopVariant="adaptive"
				loading={{ start: starting, stop: stopping, restart: restarting, remove: removing }}
			/>
		{/snippet}

		{#snippet tabContent(activeTab)}
			<Tabs.Content value="overview" class="h-full">
				<ContainerOverview {container} {primaryIpAddress} />
			</Tabs.Content>

			{#if showStats}
				<Tabs.Content value="stats" class="h-full">
					{#if selectedTab === 'stats'}
						<ContainerStats
							{container}
							{stats}
							{cpuUsagePercent}
							{cpuLimit}
							{memoryUsageFormatted}
							{memoryLimitFormatted}
							{memoryUsagePercent}
							loading={!hasInitialStatsLoaded}
						/>
					{/if}
				</Tabs.Content>
			{/if}

			<Tabs.Content value="logs" class="h-full">
				{#if selectedTab === 'logs'}
					<ContainerLogsPanel
						containerId={container?.id}
						bind:autoScroll={autoScrollLogs}
						onStart={handleLogStart}
						onStop={handleLogStop}
						onClear={handleLogClear}
						onToggleAutoScroll={handleToggleAutoScroll}
					/>
				{/if}
			</Tabs.Content>

			{#if showShell}
				<Tabs.Content value="shell" class="h-full">
					{#if selectedTab === 'shell'}
						<ContainerShell containerId={container?.id} />
					{/if}
				</Tabs.Content>
			{/if}

			{#if showConfiguration}
				<Tabs.Content value="config" class="h-full">
					<ContainerConfiguration {container} {hasEnvVars} {hasLabels} />
				</Tabs.Content>
			{/if}

			{#if hasNetworks}
				<Tabs.Content value="network" class="h-full">
					<ContainerNetwork {container} />
				</Tabs.Content>
			{/if}

			{#if hasMounts}
				<Tabs.Content value="storage" class="h-full">
					<ContainerStorage {container} />
				</Tabs.Content>
			{/if}
		{/snippet}
	</TabbedPageLayout>
{:else}
	<div class="flex min-h-screen items-center justify-center">
		<div class="text-center">
			<div class="bg-muted/50 mb-6 inline-flex rounded-full p-6">
				<AlertIcon class="text-muted-foreground size-10" />
			</div>
			<h2 class="mb-3 text-2xl font-medium">{m.common_not_found_title({ resource: m.container() })}</h2>
			<p class="text-muted-foreground mb-8 max-w-md text-center">
				{m.common_not_found_description({ resource: m.container().toLowerCase() })}
			</p>
			<div class="flex justify-center gap-4">
				<ArcaneButton action="base" href="/containers">
					<ArrowLeftIcon class="size-4" />
					{m.common_back_to({ resource: m.containers_title() })}
				</ArcaneButton>
				<ArcaneButton action="refresh" onclick={refreshData}>
					{m.common_retry()}
				</ArcaneButton>
			</div>
		</div>
	</div>
{/if}

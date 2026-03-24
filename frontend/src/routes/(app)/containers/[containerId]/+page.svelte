<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { invalidateAll } from '$app/navigation';
	import ActionButtons from '$lib/components/action-buttons.svelte';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import bytes from '$lib/utils/bytes';
	import { tick } from 'svelte';
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
	import ContainerComposePanel from '../components/ContainerComposePanel.svelte';
	import ContainerDetailStatsSync from '../components/container-detail-stats-sync.svelte';
	import IconImage from '$lib/components/icon-image.svelte';
	import { getArcaneIconUrlFromLabels } from '$lib/utils/arcane-labels';
	import { calculateMemoryUsage } from '$lib/utils/container-stats.utils';
	import {
		ArrowLeftIcon,
		AlertIcon,
		VolumesIcon,
		FileTextIcon,
		SettingsIcon,
		NetworksIcon,
		TerminalIcon,
		ContainersIcon,
		StatsIcon,
		CodeIcon
	} from '$lib/icons';
	import { parse as parseYaml } from 'yaml';
	import type { IncludeFile } from '$lib/types/project.type';
	let { data } = $props();
	let container = $derived(data?.container as ContainerDetailsDto);
	let stats = $state(null as ContainerStatsType | null);

	let selectedTab = $state<string>('overview');
	let autoScrollLogs = $state(true);
	let hasInitialStatsLoaded = $state(false);

	const cleanContainerName = (name: string | undefined): string => {
		if (!name) return m.common_not_found_title({ resource: m.containers_title() });
		return name.replace(/^\/+/, '');
	};

	const containerDisplayName = $derived(cleanContainerName(container?.name));
	const containerIconUrl = $derived(getArcaneIconUrlFromLabels(container?.labels));

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
	const memoryUsageBytes = $derived(calculateMemoryUsage(stats));
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

	async function refreshData() {
		await invalidateAll();
	}

	function handleLogClear() {
		void invalidateAll();
	}

	const hasEnvVars = $derived(!!(container?.config?.env && container.config.env.length > 0));
	const hasPorts = $derived(!!(container?.ports && container.ports.length > 0));
	const hasLabels = $derived(!!(container?.labels && Object.keys(container.labels).length > 0));
	const showConfiguration = $derived(hasEnvVars || hasLabels);

	const hasNetworks = $derived(
		!!(container?.networkSettings?.networks && Object.keys(container.networkSettings.networks).length > 0)
	);
	const showNetworkTab = $derived(hasNetworks || hasPorts);
	const hasMounts = $derived(!!(container?.mounts && container.mounts.length > 0));
	const showStats = $derived(!!container?.state?.running);
	const showShell = $derived(!!container?.state?.running);

	const project = $derived(data?.project ?? null);
	const composeInfo = $derived(container?.composeInfo ?? null);
	const composeServiceName = $derived(composeInfo?.serviceName ?? '');
	const rootComposeFilename = $derived.by(() => {
		const cf = composeInfo?.configFiles;
		if (!cf) return 'compose.yml';
		const first = cf.split(',')[0].trim();
		return first.split('/').pop() || 'compose.yml';
	});

	// Find which file (root compose or an include file) directly defines this service.
	// Returns { includeFile: null } for root compose, { includeFile: <file> } for a sub-file,
	// or null if the service isn't found anywhere (hides the tab).
	const serviceComposeSource = $derived(
		(() => {
			if (!project || !composeServiceName || !composeInfo) return null;

			const hasService = (content: string): boolean => {
				try {
					const parsed = parseYaml(content) as Record<string, unknown> | null;
					return !!(parsed?.services && (parsed.services as Record<string, unknown>)[composeServiceName]);
				} catch {
					return false;
				}
			};

			if (project.composeContent && hasService(project.composeContent)) {
				return { includeFile: null as IncludeFile | null };
			}

			for (const f of project.includeFiles ?? []) {
				if (hasService(f.content)) {
					return { includeFile: f };
				}
			}

			return null;
		})()
	);

	const showComposeTab = $derived(!!composeInfo && !!serviceComposeSource);

	const tabItems = $derived<TabItem[]>([
		{ value: 'overview', label: m.common_overview(), icon: ContainersIcon },
		...(showStats ? [{ value: 'stats', label: m.containers_nav_metrics(), icon: StatsIcon }] : []),
		{ value: 'logs', label: m.containers_nav_logs(), icon: FileTextIcon },
		...(showShell ? [{ value: 'shell', label: m.common_shell(), icon: TerminalIcon }] : []),
		...(showConfiguration ? [{ value: 'config', label: m.common_configuration(), icon: SettingsIcon }] : []),
		...(showNetworkTab ? [{ value: 'network', label: m.containers_nav_networks(), icon: NetworksIcon }] : []),
		...(hasMounts ? [{ value: 'storage', label: m.containers_nav_storage(), icon: VolumesIcon }] : []),
		...(showComposeTab ? [{ value: 'compose', label: m.tabs_compose(), icon: CodeIcon }] : [])
	]);

	const activeTab = $derived.by(() => {
		if (tabItems.some((t) => t.value === selectedTab)) {
			return selectedTab;
		}

		return tabItems[0]?.value ?? 'overview';
	});

	function onTabChange(value: string) {
		selectedTab = value;
	}

	async function navigateToNetworkPortMappings() {
		if (!showNetworkTab) return;

		selectedTab = 'network';
		await tick();

		requestAnimationFrame(() => {
			document.getElementById('container-port-mappings')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
		});
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
	<ContainerDetailStatsSync
		containerId={container.id}
		enabled={activeTab === 'stats' && !!container.state?.running}
		bind:stats
		bind:hasInitialStatsLoaded
	/>

	<TabbedPageLayout {backUrl} backLabel={m.common_back()} {tabItems} selectedTab={activeTab} {onTabChange}>
		{#snippet headerInfo()}
			<div class="flex items-center gap-2">
				<IconImage
					src={containerIconUrl}
					alt={containerDisplayName}
					fallback={ContainersIcon}
					class="size-5"
					containerClass="size-9"
				/>
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
			/>
		{/snippet}

		{#snippet tabContent(activeTab)}
			<Tabs.Content value="overview" class="h-full">
				<ContainerOverview
					{container}
					{primaryIpAddress}
					onViewPortMappings={showNetworkTab ? navigateToNetworkPortMappings : undefined}
				/>
			</Tabs.Content>

			{#if showStats}
				<Tabs.Content value="stats" class="h-full">
					{#if activeTab === 'stats'}
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
				{#if activeTab === 'logs'}
					<ContainerLogsPanel containerId={container?.id} bind:autoScroll={autoScrollLogs} onClear={handleLogClear} />
				{/if}
			</Tabs.Content>

			{#if showShell}
				<Tabs.Content value="shell" class="h-full">
					{#if activeTab === 'shell'}
						<ContainerShell containerId={container?.id} />
					{/if}
				</Tabs.Content>
			{/if}

			{#if showConfiguration}
				<Tabs.Content value="config" class="h-full">
					<ContainerConfiguration {container} {hasEnvVars} {hasLabels} />
				</Tabs.Content>
			{/if}

			{#if showNetworkTab}
				<Tabs.Content value="network" class="h-full">
					<ContainerNetwork {container} />
				</Tabs.Content>
			{/if}

			{#if hasMounts}
				<Tabs.Content value="storage" class="h-full">
					<ContainerStorage {container} />
				</Tabs.Content>
			{/if}

			{#if project && serviceComposeSource}
				<Tabs.Content value="compose" class="h-full min-h-0">
					{#key `${project?.id}-${serviceComposeSource?.includeFile?.relativePath ?? 'root'}`}
						<ContainerComposePanel
							{project}
							serviceName={composeServiceName}
							includeFile={serviceComposeSource.includeFile}
							rootFilename={rootComposeFilename}
						/>
					{/key}
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

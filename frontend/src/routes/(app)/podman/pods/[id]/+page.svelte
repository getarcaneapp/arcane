<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';

	import ActionButtonGroup from '#lib/components/action-button-group/action-button-group.svelte';
	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import EnvironmentCell from '#lib/components/arcane-table/cells/environment-cell.svelte';
	import ResourceNotFound from '#lib/components/resource-not-found.svelte';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { useUrlTab } from '#lib/hooks/use-url-tab.svelte.js';
	import {
		AlertTriangleIcon,
		ContainersIcon,
		InfoIcon,
		InspectIcon,
		LayoutListIcon,
		NetworksIcon,
		StatsIcon,
		CpuIcon
	} from '#lib/icons/index.js';
	import TabbedPageLayout from '#lib/layouts/tabbed-page-layout.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService, type PodmanPodAction } from '#lib/services/podman-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';

	import PodContainers from '../components/pod-containers.svelte';
	import PodInspect from '../components/pod-inspect.svelte';
	import PodNetwork from '../components/pod-network.svelte';
	import PodOverview from '../components/pod-overview.svelte';
	import PodProcesses from '../components/pod-processes.svelte';
	import PodResources from '../components/pod-resources.svelte';
	import PodStats from '../components/pod-stats.svelte';
	import { confirmRemovePod, podActionLabels, runPodAction } from '../pod-actions';

	let { data } = $props();

	const queryClient = useQueryClient();
	const envId = $derived(data.envId);
	const podQuery = createQuery(() => ({
		queryKey: queryKeys.podman.pod(envId, data.pod.id),
		queryFn: () => podmanService.getPod(data.pod.id, envId),
		initialData: data.pod,
		refetchInterval: 10000,
		retry: false
	}));
	const pod = $derived(podQuery.data ?? data.pod);
	const isRunning = $derived(pod.state.toLowerCase() === 'running');
	const canManage = $derived(hasPermission(PERMISSIONS.PODMAN_PODS, envId));

	let pendingAction = $state<PodmanPodAction | null>(null);

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.environment(envId) });
	}

	function lifecycleButton(action: PodmanPodAction, placement: 'primary' | 'secondary' | 'menu'): ActionButton {
		return {
			id: action,
			action,
			group: 'lifecycle',
			placement,
			label: podActionLabels[action](),
			loading: pendingAction === action,
			disabled: pendingAction !== null,
			onclick: () =>
				runPodAction({ ...pod, environmentId: envId }, action, refresh, (loading) => (pendingAction = loading ? action : null))
		};
	}

	const actions: ActionButton[] = $derived(
		canManage
			? [
					isRunning ? lifecycleButton('stop', 'primary') : lifecycleButton('start', 'primary'),
					lifecycleButton('restart', 'secondary'),
					pod.state.toLowerCase() === 'paused' ? lifecycleButton('unpause', 'menu') : lifecycleButton('pause', 'menu'),
					lifecycleButton('kill', 'menu'),
					{
						id: 'remove',
						action: 'remove',
						group: 'danger',
						destructive: true,
						placement: 'menu',
						label: m.common_remove(),
						onclick: () => confirmRemovePod({ ...pod, environmentId: envId }, () => goto('/podman/pods'))
					}
				]
			: []
	);

	const urlTab = useUrlTab({
		tabs: () => [
			{ value: 'overview', label: m.common_overview(), icon: InfoIcon },
			{ value: 'containers', label: m.containers(), icon: ContainersIcon },
			{ value: 'network', label: m.resource_networks_cap(), icon: NetworksIcon },
			{ value: 'resources', label: m.common_resources(), icon: CpuIcon },
			{ value: 'stats', label: m.containers_nav_metrics(), icon: StatsIcon },
			{ value: 'processes', label: m.containers_processes_title(), icon: LayoutListIcon },
			{ value: 'inspect', label: m.common_inspect(), icon: InspectIcon }
		],
		defaultTab: () => 'overview'
	});
</script>

{#if pod}
	<TabbedPageLayout
		backUrl="/podman/pods"
		backLabel={m.common_back()}
		tabItems={urlTab.items}
		selectedTab={urlTab.value}
		onTabChange={urlTab.select}
	>
		{#snippet headerInfo()}
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
				<h1 class="max-w-40 min-w-0 truncate text-lg font-semibold sm:max-w-56 md:max-w-72 lg:max-w-88" title={pod.name}>
					{pod.name}
				</h1>
				<Badge variant={podmanStateVariant(pod.state)} minWidth="20">{pod.state}</Badge>
				<EnvironmentCell value={envId} />
			</div>
		{/snippet}

		{#snippet headerActions()}
			{#if actions.length > 0}
				<ActionButtonGroup buttons={actions} />
			{/if}
		{/snippet}

		{#snippet subHeader()}
			{#if podQuery.isError}
				<Alert.Root variant="destructive" class="mt-3">
					<AlertTriangleIcon class="size-4" />
					<Alert.Title>{m.podman_updates_paused()}</Alert.Title>
					<Alert.Description>{extractApiErrorMessage(podQuery.error)}</Alert.Description>
				</Alert.Root>
			{/if}
		{/snippet}

		{#snippet tabContent(activeTab)}
			<Tabs.Content value="overview" class="h-full">
				<PodOverview {pod} />
			</Tabs.Content>

			<Tabs.Content value="containers" class="h-full">
				<PodContainers {pod} {envId} />
			</Tabs.Content>

			<Tabs.Content value="network" class="h-full">
				<PodNetwork infraConfig={pod.infraConfig} />
			</Tabs.Content>

			<Tabs.Content value="resources" class="h-full">
				<PodResources {pod} />
			</Tabs.Content>

			<Tabs.Content value="stats" class="h-full">
				{#if activeTab === 'stats'}
					<PodStats podName={pod.name} {envId} {isRunning} />
				{/if}
			</Tabs.Content>

			<Tabs.Content value="processes" class="h-full">
				{#if activeTab === 'processes'}
					<PodProcesses podId={pod.id} {envId} {isRunning} />
				{/if}
			</Tabs.Content>

			<Tabs.Content value="inspect" class="h-full">
				<PodInspect {pod} />
			</Tabs.Content>
		{/snippet}
	</TabbedPageLayout>
{:else}
	<ResourceNotFound
		resource={m.podman_pod()}
		resourceListTitle={m.podman_pods_title()}
		backHref="/podman/pods"
		onRetry={() => podQuery.refetch()}
	/>
{/if}

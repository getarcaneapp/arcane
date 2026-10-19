<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';

	import type { ActionButton } from '#lib/components/action-button-group/types.js';
	import { DetailSection, KeyValueCard, KeyValueGrid } from '#lib/components/resource-detail/index.js';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import MonoCell from '#lib/components/vulnerability/mono-cell.svelte';
	import { ContainersIcon, InfoIcon, LayoutListIcon, StatsIcon } from '#lib/icons/index.js';
	import { ResourceDetailLayout } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService, type PodmanPodAction } from '#lib/services/podman-service.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDateTime } from '#lib/utils/formatting.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';

	import { confirmRemovePod, podActionLabels, runPodAction } from '../pod-actions';

	let { data } = $props();

	const queryClient = useQueryClient();
	const envId = $derived(data.envId);
	const podQuery = createQuery(() => ({
		queryKey: queryKeys.podman.pod(envId, data.pod.id),
		queryFn: () => podmanService.getPod(data.pod.id),
		initialData: data.pod,
		refetchInterval: 10000,
		retry: false
	}));
	const pod = $derived(podQuery.data ?? data.pod);
	const isRunning = $derived(pod.state.toLowerCase() === 'running');
	const canManage = $derived(hasPermission('podman:pods', envId));

	const statsQuery = createQuery(() => ({
		queryKey: queryKeys.podman.podStats(envId, pod.name),
		queryFn: () => podmanService.getPodStats([pod.name]),
		enabled: isRunning,
		refetchInterval: 5000,
		retry: false
	}));

	const topQuery = createQuery(() => ({
		queryKey: queryKeys.podman.podTop(envId, pod.id),
		queryFn: () => podmanService.getPodTop(pod.id),
		enabled: isRunning,
		refetchInterval: 5000,
		retry: false
	}));

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
			onclick: () => runPodAction(pod, action, refresh, (loading) => (pendingAction = loading ? action : null))
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
						onclick: () => confirmRemovePod(pod, () => goto('/podman/pods'))
					}
				]
			: []
	);

	const portBindings = $derived(
		Object.entries(pod.infraConfig?.portBindings ?? {}).flatMap(([containerPort, bindings]) =>
			(bindings ?? []).map((binding) => `${binding.hostIp ? `${binding.hostIp}:` : ''}${binding.hostPort} → ${containerPort}`)
		)
	);
</script>

<ResourceDetailLayout
	backUrl="/podman/pods"
	backLabel={m.common_back_to({ resource: m.podman_pods_title() })}
	title={pod.name}
	subtitle={pod.id}
	{actions}
>
	{#snippet badges()}
		<Badge variant={podmanStateVariant(pod.state)} minWidth="20">{pod.state}</Badge>
	{/snippet}

	<DetailSection title={m.common_overview()} icon={InfoIcon}>
		<KeyValueGrid>
			<KeyValueCard label={m.common_id()}>{pod.id}</KeyValueCard>
			<KeyValueCard label={m.common_created()}>{formatDateTime(pod.created) || pod.created}</KeyValueCard>
			{#if pod.hostname}
				<KeyValueCard label={m.hostname()}>{pod.hostname}</KeyValueCard>
			{/if}
			<KeyValueCard label={m.podman_pod_infra()}>{pod.createInfra ? m.common_yes() : m.common_no()}</KeyValueCard>
			{#if pod.sharedNamespaces?.length}
				<KeyValueCard label={m.podman_pod_shared_namespaces()}>{pod.sharedNamespaces.join(', ')}</KeyValueCard>
			{/if}
			{#if pod.infraConfig?.networks?.length}
				<KeyValueCard label={m.resource_networks_cap()}>{pod.infraConfig.networks.join(', ')}</KeyValueCard>
			{/if}
			{#if portBindings.length > 0}
				<KeyValueCard label={m.common_ports()}>{portBindings.join(', ')}</KeyValueCard>
			{/if}
		</KeyValueGrid>
	</DetailSection>

	<DetailSection title={m.containers()} icon={ContainersIcon}>
		{#if pod.containers.length === 0}
			<EmptyState icon={ContainersIcon} title={m.podman_pod_no_containers()} variant="plain" />
		{:else}
			<div class="divide-y rounded-lg border">
				{#each pod.containers as member (member.id)}
					<div class="flex items-center justify-between gap-3 p-3">
						<a class="min-w-0 truncate font-medium hover:underline" href={`/containers/${member.id}`}>{member.name}</a>
						<div class="flex items-center gap-2">
							{#if member.id === pod.infraContainerId}
								<Badge variant="gray" size="sm">{m.podman_pod_infra()}</Badge>
							{/if}
							<Badge variant={podmanStateVariant(member.state)} size="sm">{member.state}</Badge>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</DetailSection>

	<DetailSection title={m.podman_pod_stats()} icon={StatsIcon}>
		{#if !isRunning}
			<EmptyState icon={StatsIcon} title={m.common_unavailable()} description={m.podman_pod_not_running()} variant="plain" />
		{:else if statsQuery.isError}
			<EmptyState
				icon={StatsIcon}
				title={m.common_load_failed({ resource: m.podman_pod_stats() })}
				description={extractApiErrorMessage(statsQuery.error)}
				actionLabel={m.common_retry()}
				onAction={() => statsQuery.refetch()}
				variant="plain"
			/>
		{:else}
			<div class="overflow-x-auto rounded-lg border">
				<Table.Root aria-label={m.podman_pod_stats()}>
					<Table.Header>
						<Table.Row>
							<Table.Head scope="col">{m.common_name()}</Table.Head>
							<Table.Head scope="col">{m.cpu_usage()}</Table.Head>
							<Table.Head scope="col">{m.memory_usage()}</Table.Head>
							<Table.Head scope="col">{m.containers_block_io()}</Table.Head>
							<Table.Head scope="col">{m.podman_net_io()}</Table.Head>
							<Table.Head scope="col">{m.podman_pids()}</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each statsQuery.data ?? [] as stat (stat.cid)}
							<Table.Row>
								<Table.Cell>{stat.name}</Table.Cell>
								<Table.Cell><MonoCell value={stat.cpu} /></Table.Cell>
								<Table.Cell><MonoCell value={stat.memUsage} /></Table.Cell>
								<Table.Cell><MonoCell value={stat.blockIo} /></Table.Cell>
								<Table.Cell><MonoCell value={stat.netIo} /></Table.Cell>
								<Table.Cell><MonoCell value={stat.pids} /></Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		{/if}
	</DetailSection>

	<DetailSection title={m.containers_processes_title()} icon={LayoutListIcon}>
		{#if !isRunning}
			<EmptyState icon={LayoutListIcon} title={m.common_unavailable()} description={m.podman_pod_not_running()} variant="plain" />
		{:else if topQuery.isError}
			<EmptyState
				icon={LayoutListIcon}
				title={m.common_load_failed({ resource: m.containers_processes_title() })}
				description={extractApiErrorMessage(topQuery.error)}
				actionLabel={m.common_retry()}
				onAction={() => topQuery.refetch()}
				variant="plain"
			/>
		{:else if topQuery.data}
			<div class="overflow-x-auto rounded-lg border">
				<Table.Root aria-label={m.containers_processes_title()}>
					<Table.Header>
						<Table.Row>
							{#each topQuery.data.titles as title, index (index)}
								<Table.Head scope="col">{title}</Table.Head>
							{/each}
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each topQuery.data.processes as cells, rowIndex (rowIndex)}
							<Table.Row>
								{#each cells as cell, cellIndex (cellIndex)}
									<Table.Cell><MonoCell value={cell} /></Table.Cell>
								{/each}
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</div>
		{/if}
	</DetailSection>
</ResourceDetailLayout>

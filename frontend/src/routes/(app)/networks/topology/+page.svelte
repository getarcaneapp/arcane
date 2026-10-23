<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import NetworkDiagram from '#lib/components/network-diagram/network-diagram.svelte';
	import { fetchEnvironmentTopology } from '#lib/components/network-diagram/topology.js';
	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertTriangleIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import settingsStore from '#lib/stores/config-store.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	let { data } = $props();

	const scope = $derived(environmentStore.shownKey);

	const topologyQuery = createQuery(() => ({
		queryKey: queryKeys.networks.topology(scope),
		queryFn: fetchEnvironmentTopology,
		initialData: data.scope === scope ? data.topology : undefined
	}));
	const topology = $derived(topologyQuery.data ?? null);

	async function refresh() {
		await topologyQuery.refetch();
	}

	const isRefreshing = $derived(topologyQuery.isFetching && !topologyQuery.isPending);

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: topologyQuery.isFetching
		}
	]);
</script>

<ResourcePageLayout title={m.networks_topology_title()} subtitle={m.networks_topology_subtitle()} {actionButtons}>
	{#snippet mainContent()}
		{#if topology}
			<div class="space-y-4">
				{#if topology.failedEnvironments.length > 0}
					<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon}>
						<div class="flex flex-wrap items-center justify-between gap-2">
							<Alert.Description>
								{m.all_environments_unavailable({
									environments: topology.failedEnvironments
										.map(({ environment, message }) => `${environment.name} (${message})`)
										.join(', ')
								})}
							</Alert.Description>
							<ArcaneButton
								action="refresh"
								size="sm"
								customLabel={m.common_retry()}
								onClickPromise={async () =>
									handleApiResultWithCallbacks({ result: await tryCatch(topologyQuery.refetch()), message: m.common_failed() })}
							/>
						</div>
					</Alert.Root>
				{/if}
				{#if topology.excludedEnvironments.length > 0}
					<Alert.Root variant="warning-subtle" size="sm" icon={AlertTriangleIcon}>
						<Alert.Description>
							{m.all_environments_excluded({
								count: topology.excludedEnvironments.length,
								limit: settingsStore.current?.allEnvironmentsLimit ?? 0
							})}
						</Alert.Description>
					</Alert.Root>
				{/if}
				<NetworkDiagram {topology} />
			</div>
		{/if}
	{/snippet}
</ResourcePageLayout>

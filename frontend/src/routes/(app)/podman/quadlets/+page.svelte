<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertTriangleIcon, FileTextIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { podmanQuadletListQuery } from '#lib/query/podman.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';

	import QuadletsTable from './components/quadlets-table.svelte';

	let { data } = $props();

	const pageState = new ResourceListPageState(untrack(() => data.requestOptions));
	const quadletEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses, (status) => status.quadletsSupported));
	const canInstall = $derived(quadletEnvironments.some((env) => hasPermission('podman:quadlets', env.id)));

	const quadletsQuery = createQuery(() => {
		const scope = pageState.scope;
		return {
			...podmanQuadletListQuery(scope, quadletEnvironments, pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});

	const resourcesReady = $derived(quadletsQuery.data?.envId === pageState.scope);
	const quadlets = $derived(quadletsQuery.data?.value ?? data.quadlets);

	const isRefreshing = $derived(quadletsQuery.isFetching && !quadletsQuery.isPending);

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canInstall) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.podman_quadlet_install(),
				href: '/podman/quadlets/new'
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: () => quadletsQuery.refetch(),
			loading: isRefreshing,
			disabled: quadletsQuery.isFetching
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.podman_quadlets_total(),
			value: resourcesReady ? (quadlets.pagination?.totalItems ?? quadlets.data.length) : 0,
			icon: FileTextIcon,
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout title={m.podman_quadlets_title()} subtitle={m.podman_quadlets_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<div class="space-y-4">
			{#if quadletEnvironments.length === 0}
				<Alert.Root variant="warning-subtle">
					<AlertTriangleIcon class="size-4 text-warning" />
					<Alert.Title>{m.common_unavailable()}</Alert.Title>
					<Alert.Description>{m.podman_quadlets_unsupported()}</Alert.Description>
				</Alert.Root>
			{/if}

			{#if resourcesReady}
				{#key pageState.scope}
					<QuadletsTable
						{quadlets}
						bind:requestOptions={pageState.requestOptions}
						podmanStatuses={data.podmanStatuses}
						onRetry={() => quadletsQuery.refetch()}
					/>
				{/key}
			{/if}
		</div>
	{/snippet}
</ResourcePageLayout>

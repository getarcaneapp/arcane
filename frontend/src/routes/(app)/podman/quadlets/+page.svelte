<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';

	import * as Alert from '#lib/components/ui/alert/index.js';
	import { AlertTriangleIcon, FileTextIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { fetchAcrossEnvironments } from '#lib/utils/environments.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import QuadletsTable from './components/quadlets-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.requestOptions));
	const quadletEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses, (status) => status.quadletsSupported));
	const canInstall = $derived(quadletEnvironments.some((env) => hasPermission(PERMISSIONS.PODMAN_QUADLETS, env.id)));

	const quadletsQuery = createQuery(() => {
		const queryScope = pageState.scope;
		const environments = quadletEnvironments;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.podman.quadlets(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) => podmanService.getQuadlets(id, request, config)),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});

	const resourcesReady = $derived(quadletsQuery.data?.envId === pageState.scope);
	const quadlets = $derived(quadletsQuery.data?.value ?? data.quadlets);

	async function loadQuadlets(options: SearchPaginationSortRequest = pageState.requestOptions, requestedScope = pageState.scope) {
		const environments = quadletEnvironments;
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.podman.quadlets(requestedScope, options),
			staleTime: 0,
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) => podmanService.getQuadlets(id, request, config))
		});
	}

	async function refresh() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(loadQuadlets()),
			message: m.common_refresh_failed({ resource: m.podman_quadlets_title() })
		});
	}

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
			onclick: refresh,
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
						bind:quadlets={
							() => quadlets,
							(value) => queryClient.setQueryData(queryKeys.podman.quadlets(pageState.scope, pageState.requestOptions), value)
						}
						bind:requestOptions={pageState.requestOptions}
						podmanStatuses={data.podmanStatuses}
						onRefreshData={loadQuadlets}
					/>
				{/key}
			{/if}
		</div>
	{/snippet}
</ResourcePageLayout>

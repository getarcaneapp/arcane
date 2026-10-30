<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ServiceEditorDialog from '#lib/components/dialogs/service-editor-dialog.svelte';
	import { swarmEnvironmentsQueryOptions } from '#lib/components/swarm/swarm-environments.js';
	import { DockIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmServiceListQuery } from '#lib/query/swarm.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmServiceCreateSpec } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { destinationEnvironmentId } from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmServicesTable from './components/services-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	const swarmQuery = createQuery(() => swarmEnvironmentsQueryOptions('swarm:read'));
	const swarm = $derived(swarmQuery.data ?? data.swarm);
	const isLoading = $state({ creating: false });
	let showCreateDialog = $state(false);
	let createEnvironmentId = $state('');

	const servicesQuery = createQuery(() => {
		const scope = swarm.scope;
		return {
			...swarmServiceListQuery(swarm, pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});
	const services = $derived(servicesQuery.data?.value ?? data.services);
	const isRefreshing = $derived(servicesQuery.isFetching && !servicesQuery.isPending);

	async function refresh() {
		await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(servicesQuery.refetch({ throwOnError: true })),
			message: m.common_refresh_failed({ resource: m.services() })
		});
	}

	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);

	const canCreateService = $derived(swarm.managers.some((env) => hasPermission('swarm:services', env.id)));

	function openCreateDialog() {
		createEnvironmentId = destinationEnvironmentId(createEnvironmentId, 'swarm:services', swarm.managers);
		showCreateDialog = true;
	}

	async function handleCreateService(payload: { spec: Record<string, unknown>; options?: Record<string, unknown> }) {
		const spec = payload.spec as unknown as SwarmServiceCreateSpec;
		isLoading.creating = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.createService({ spec }, createEnvironmentId)),
			message: m.common_create_failed({ resource: `${m.swarm_service()} "${spec.Name}"` }),
			setLoadingState: (v) => (isLoading.creating = v),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: `${m.swarm_service()} "${spec.Name}"` }));
				showCreateDialog = false;
				await queryClient.invalidateQueries({ queryKey: queryKeys.swarm.listPrefix(swarm.scope, 'services') });
			}
		});
	}

	const actionButtons = $derived.by(() =>
		createRefreshActionButtons({
			create: {
				allowed: canCreateService,
				label: m.common_create_button({ resource: m.swarm_service() }),
				onclick: openCreateDialog
			},
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isRefreshing
		})
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_services_total(),
			value: totalServices,
			icon: DockIcon,
			iconColor: 'text-info'
		}
	]);
</script>

{#if showCreateDialog}
	<ServiceEditorDialog
		bind:open={showCreateDialog}
		bind:environmentId={createEnvironmentId}
		environments={swarm.managers}
		environmentPermission="swarm:services"
		title={m.swarm_service_create_title()}
		description={m.swarm_service_create_description()}
		submitLabel={m.common_create()}
		submitAction="create"
		initialSpec=""
		isLoading={isLoading.creating}
		onSubmit={handleCreateService}
	/>
{/if}

<ResourcePageLayout title={m.services()} subtitle={m.swarm_services_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmServicesTable
			{services}
			invalidateKey={queryKeys.swarm.listPrefix(swarm.scope, 'services')}
			bind:requestOptions={pageState.requestOptions}
			onRetry={() => servicesQuery.refetch()}
		/>
	{/snippet}
</ResourcePageLayout>

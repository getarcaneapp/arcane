<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ServiceEditorDialog from '#lib/components/dialogs/service-editor-dialog.svelte';
	import { swarmEnvironmentsQueryOptions, type SwarmEnvironments } from '#lib/components/swarm/swarm-environments.js';
	import { DockIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { swarmService } from '#lib/services/swarm-service.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import type { SwarmServiceCreateSpec } from '#lib/types/swarm.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { destinationEnvironmentId, fetchAcrossEnvironments, withFailedEnvironments } from '#lib/utils/environments.js';
	import { createRefreshActionButtons } from '#lib/utils/resource-actions.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import SwarmServicesTable from './components/services-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	let requestOptions = $state<SearchPaginationSortRequest>(untrack(() => data.requestOptions));
	const swarmQuery = createQuery(() => swarmEnvironmentsQueryOptions('swarm:read'));
	const swarm = $derived(swarmQuery.data ?? data.swarm);
	let isLoading = $state({ creating: false });
	let showCreateDialog = $state(false);
	let createEnvironmentId = $state('');

	function servicesQueryOptions(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		const { environments, failedEnvironments, scope } = current;
		return {
			queryKey: queryKeys.swarm.services(scope, options),
			queryFn: async () =>
				withFailedEnvironments(
					await fetchAcrossEnvironments(environments, options, (id, request, config) =>
						swarmService.getServices(request, id, config)
					),
					failedEnvironments
				)
		};
	}

	const servicesQuery = createQuery(() => ({
		...servicesQueryOptions(requestOptions),
		placeholderData: (previous) => previous
	}));
	let services = $derived(servicesQuery.data ?? data.services);
	const isRefreshing = $derived(servicesQuery.isFetching && !servicesQuery.isPending);

	function fetchServices(options: SearchPaginationSortRequest, current: SwarmEnvironments = swarm) {
		requestOptions = options;
		return queryClient.query({ ...servicesQueryOptions(options, current), staleTime: 0 });
	}

	async function refresh() {
		const discovered = await swarmQuery.refetch();
		await handleApiResultWithCallbacks({
			result: await tryCatch(fetchServices(requestOptions, discovered.data ?? swarm)),
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
				await refresh();
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
		<SwarmServicesTable bind:services bind:requestOptions {fetchServices} />
	{/snippet}
</ResourcePageLayout>

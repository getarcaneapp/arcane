<script lang="ts">
	import { DockIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { toast } from 'svelte-sonner';
	import { untrack } from 'svelte';
	import { tryCatch } from '$lib/utils/api';
	import { handleApiResultWithCallbacks } from '$lib/utils/api';
	import { ResourcePageLayout, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/api';
	import { createRefreshActionButtons } from '$lib/utils/resource-actions';
	import type { SwarmServiceCreateSpec } from '$lib/types/swarm';
	import SwarmServicesTable from './services-table.svelte';
	import ServiceEditorDialog from '$lib/components/dialogs/service-editor-dialog.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { environmentStore } from '$lib/stores/environment.store.svelte';

	let { data } = $props();

	let services = $state(untrack(() => data.services));
	let requestOptions = $state(untrack(() => data.requestOptions));
	let isLoading = $state({ refresh: false, creating: false });
	let showCreateDialog = $state(false);

	async function refresh() {
		await parallelRefresh(
			{
				services: {
					fetch: () => swarmService.getServices(requestOptions),
					onSuccess: (data) => {
						services = data;
					},
					errorMessage: m.common_refresh_failed({ resource: m.services() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);

	const currentEnvId = $derived(environmentStore.selected?.id);
	const canCreateService = $derived(hasPermission('swarm:services', currentEnvId));

	async function handleCreateService(payload: { spec: Record<string, unknown>; options?: Record<string, unknown> }) {
		const spec = payload.spec as unknown as SwarmServiceCreateSpec;
		handleApiResultWithCallbacks({
			result: await tryCatch(swarmService.createService({ spec })),
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
			canCreate: canCreateService,
			createLabel: m.common_create_button({ resource: m.swarm_service() }),
			onCreate: () => (showCreateDialog = true),
			refreshLabel: m.common_refresh(),
			onRefresh: refresh,
			refreshing: isLoading.refresh
		})
	);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_services_total(),
			value: totalServices,
			icon: DockIcon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

{#if showCreateDialog}
	<ServiceEditorDialog
		bind:open={showCreateDialog}
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
		<SwarmServicesTable bind:services bind:requestOptions />
	{/snippet}
</ResourcePageLayout>

<script lang="ts">
	import { DockIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { swarmService } from '$lib/services/swarm-service';
	import { toast } from 'svelte-sonner';
	import { untrack } from 'svelte';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import SwarmServicesTable from './services-table.svelte';
	import CreateServiceDialog from '$lib/components/dialogs/create-service-dialog.svelte';

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
					errorMessage: m.common_refresh_failed({ resource: m.swarm_services_title() })
				}
			},
			(v) => (isLoading.refresh = v)
		);
	}

	useEnvironmentRefresh(refresh);

	const totalServices = $derived(services?.pagination?.totalItems ?? services?.data?.length ?? 0);

	async function handleCreateService(spec: Record<string, unknown>) {
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

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.swarm_service() }),
			onclick: () => (showCreateDialog = true)
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refresh,
			disabled: isLoading.refresh
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.swarm_services_total(),
			value: totalServices,
			icon: DockIcon,
			iconColor: 'text-blue-500'
		}
	]);
</script>

<CreateServiceDialog bind:open={showCreateDialog} onSubmit={handleCreateService} isLoading={isLoading.creating} />

<ResourcePageLayout title={m.swarm_services_title()} subtitle={m.swarm_services_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<SwarmServicesTable bind:services bind:requestOptions />
	{/snippet}
</ResourcePageLayout>

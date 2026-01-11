<script lang="ts">
	import CreateContainerDialog from '$lib/components/dialogs/create-container-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { containerService } from '$lib/services/container-service';
	import ContainerTable from './container-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';
	import { BoxIcon } from '$lib/icons';

	let { data } = $props();

	let containers = $state(untrack(() => data.containers));
	let containerStatusCounts = $state(untrack(() => data.containerStatusCounts));
	let requestOptions = $state(untrack(() => data.containerRequestOptions));
	let selectedIds = $state([]);
	let isCreateDialogOpen = $state(false);
	let isLoading = $state({ checking: false, create: false, refreshing: false });

	async function refresh() {
		await parallelRefresh(
			{
				containers: {
					fetch: () => containerService.getContainers(requestOptions),
					onSuccess: (data) => {
						containers = data;
						containerStatusCounts = data.counts ?? { runningContainers: 0, stoppedContainers: 0, totalContainers: 0 };
					},
					errorMessage: m.common_refresh_failed({ resource: m.containers_title() })
				}
			},
			(v) => (isLoading.refreshing = v)
		);
	}

	useEnvironmentRefresh(refresh);

	async function handleCheckForUpdates() {
		isLoading.checking = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(imageService.runAutoUpdate()),
			message: m.containers_check_updates_failed(),
			setLoadingState: (v) => (isLoading.checking = v),
			onSuccess: async () => {
				toast.success(m.containers_check_updates_success());
				containers = await containerService.getContainers(requestOptions);
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		{
			id: 'create',
			action: 'create',
			label: m.common_create_button({ resource: m.resource_container_cap() }),
			onclick: () => (isCreateDialogOpen = true),
			loading: isLoading.create,
			disabled: isLoading.create
		},
		{
			id: 'check-updates',
			action: 'update',
			label: m.containers_check_updates(),
			onclick: handleCheckForUpdates,
			loading: isLoading.checking,
			disabled: isLoading.checking
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.common_total(),
			value: containerStatusCounts.totalContainers,
			icon: BoxIcon
		},
		{
			title: m.common_running(),
			value: containerStatusCounts.runningContainers,
			icon: BoxIcon,
			iconColor: 'text-green-500'
		},
		{
			title: m.common_stopped(),
			value: containerStatusCounts.stoppedContainers,
			icon: BoxIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.containers_title()} subtitle={m.containers_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<ContainerTable bind:containers bind:selectedIds bind:requestOptions />
	{/snippet}

	{#snippet additionalContent()}
		<CreateContainerDialog
			bind:open={isCreateDialogOpen}
			isLoading={isLoading.create}
			onSubmit={async (options) => {
				isLoading.create = true;
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.createContainer(options)),
					message: m.containers_create_failed(),
					setLoadingState: (v) => (isLoading.create = v),
					onSuccess: async () => {
						toast.success(m.common_create_success({ resource: m.resource_container() }));
						containers = await containerService.getContainers(requestOptions);
						isCreateDialogOpen = false;
					}
				});
			}}
		/>
	{/snippet}
</ResourcePageLayout>

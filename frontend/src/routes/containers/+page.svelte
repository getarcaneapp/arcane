<script lang="ts">
	import BoxIcon from '@lucide/svelte/icons/box';
	import CreateContainerDialog from '$lib/components/dialogs/create-container-dialog.svelte';
	import { toast } from 'svelte-sonner';
	import { tryCatch } from '$lib/utils/try-catch';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { containerService } from '$lib/services/container-service';
	import ContainerTable from './container-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { imageService } from '$lib/services/image-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';

	let { data } = $props();

	let containers = $state(untrack(() => data.containers));
	let containerStatusCounts = $state(untrack(() => data.containerStatusCounts));
	let containerRequestOptions = $state(untrack(() => data.containerRequestOptions));

	let requestOptions = $state(untrack(() => data.containerRequestOptions));
	let selectedIds = $state([]);
	let isCreateDialogOpen = $state(false);

	let isLoading = $state({
		checking: false,
		create: false,
		refreshing: false
	});

	const baseServerUrl = $derived(
		(data.settings as any)?.serverBaseUrl ?? (data.settings as any)?.baseServerUrl ?? (data.settings as any)?.baseUrl ?? ''
	);

	async function handleCheckForUpdates() {
		isLoading.checking = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(imageService.runAutoUpdate()),
			message: m.containers_check_updates_failed(),
			setLoadingState: (value) => (isLoading.checking = value),
			async onSuccess() {
				toast.success(m.containers_check_updates_success());
				containers = await containerService.getContainers(requestOptions);
			}
		});
	}

	async function refreshContainers() {
		isLoading.refreshing = true;
		let refreshingContainerList = true;
		let refreshingContainerCounts = true;
		handleApiResultWithCallbacks({
			result: await tryCatch(containerService.getContainers(requestOptions)),
			message: m.common_refresh_failed({ resource: m.containers_title() }),
			setLoadingState: (value) => {
				refreshingContainerList = value;
				isLoading.refreshing = refreshingContainerCounts || refreshingContainerList;
			},
			async onSuccess(newContainers) {
				containers = newContainers;
			}
		});
		handleApiResultWithCallbacks({
			result: await tryCatch(containerService.getContainerStatusCounts()),
			message: m.common_refresh_failed({ resource: m.containers_title() }),
			setLoadingState: (value) => {
				refreshingContainerCounts = value;
				isLoading.refreshing = refreshingContainerCounts || refreshingContainerList;
			},
			async onSuccess(newStatusCounts) {
				containerStatusCounts = newStatusCounts;
			}
		});
	}

	let lastEnvId: string | null = null;
	$effect(() => {
		const env = environmentStore.selected;
		if (!env) return;
		if (lastEnvId === null) {
			lastEnvId = env.id;
			return;
		}
		if (env.id !== lastEnvId) {
			lastEnvId = env.id;
			refreshContainers();
		}
	});

	const actionButtons: ActionButton[] = $derived.by(() => [
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
			action: 'inspect',
			label: m.containers_check_updates(),
			onclick: handleCheckForUpdates,
			loading: isLoading.checking,
			disabled: isLoading.checking
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refreshContainers,
			loading: isLoading.refreshing,
			disabled: isLoading.refreshing
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.common_total(),
			value: containerStatusCounts.totalContainers,
			icon: BoxIcon,
			class: 'border-l-primary border-l-4 transition-shadow hover:shadow-lg'
		},
		{
			title: m.common_running(),
			value: containerStatusCounts.runningContainers,
			icon: BoxIcon,
			iconColor: 'text-green-500',
			bgColor: 'bg-green-500/10',
			class: 'border-l-4 border-l-green-500'
		},
		{
			title: m.common_stopped(),
			value: containerStatusCounts.stoppedContainers,
			icon: BoxIcon,
			iconColor: 'text-amber-500',
			class: 'border-l-4 border-l-amber-500'
		}
	]);
</script>

<ResourcePageLayout
	title={m.containers_title()}
	subtitle={m.containers_subtitle()}
	{actionButtons}
	{statCards}
	statCardsColumns={3}
>
	{#snippet mainContent()}
		<ContainerTable bind:containers bind:selectedIds bind:requestOptions {baseServerUrl} />
	{/snippet}

	{#snippet additionalContent()}
		<CreateContainerDialog
			bind:open={isCreateDialogOpen}
			availableVolumes={[]}
			availableNetworks={[]}
			availableImages={[]}
			isLoading={isLoading.create}
			onSubmit={async (options) => {
				isLoading.create = true;
				handleApiResultWithCallbacks({
					result: await tryCatch(containerService.createContainer(options)),
					message: m.containers_create_failed(),
					setLoadingState: (value) => (isLoading.create = value),
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

<script lang="ts">
	// fallow-ignore-file code-duplication -- volume and network pages share ResourceListPageState lifecycle wiring but retain domain-specific queries and mutations
	import { VolumesIcon, VolumeUnusedIcon } from '$lib/icons';
	import { toast } from 'svelte-sonner';
	import CreateVolumeSheet from '$lib/components/sheets/create-volume-sheet.svelte';
	import type { VolumeCreateRequest, VolumeUsageCounts } from '$lib/types/docker';
	import VolumeTable from './volume-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import { ResourceListPageState } from '$lib/utils/resource-list-page.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { queryKeys } from '$lib/query/query-keys';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(
		untrack(() => data.volumes),
		untrack(() => data.volumeRequestOptions)
	);
	let previousEnvId = untrack(() => pageState.envId);
	let displayedEnvId = $state<string | null>(untrack(() => (data.envId === pageState.envId ? data.envId : null)));
	const resourcesReady = $derived(displayedEnvId === pageState.envId);
	const countsFallback: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const volumesQuery = createQuery(() => {
		const queryEnvId = pageState.envId;
		return {
			queryKey: queryKeys.volumes.table(queryEnvId, pageState.requestOptions),
			queryFn: () => volumeService.getVolumesForEnvironment(queryEnvId, pageState.requestOptions),
			initialData: data.envId === queryEnvId ? data.volumes : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});

	const createVolumeMutation = createMutation(() => ({
		mutationKey: ['volumes', 'create', pageState.envId],
		mutationFn: ({ options, requestedEnvId }: { options: VolumeCreateRequest; requestedEnvId: string }) =>
			volumeService.createVolume(options, requestedEnvId),
		onSuccess: async (data, options) => {
			const name = options.options.name?.trim() || m.common_unknown();
			toast.success(
				m.common_create_success({ resource: `${m.resource_volume()} "${name}"` }),
				activityToastOptions(extractActivityId(data))
			);
			if (options.requestedEnvId === pageState.envId) {
				await loadVolumes(pageState.requestOptions, options.requestedEnvId);
				pageState.isCreateDialogOpen = false;
			}
		},
		onError: (_error, options) => {
			const name = options.options.name?.trim() || m.common_unknown();
			toast.error(m.common_create_failed({ resource: `${m.resource_volume()} "${name}"` }));
		}
	}));

	$effect(() => {
		if (volumesQuery.data?.envId === pageState.envId) {
			pageState.items = volumesQuery.data.value;
			displayedEnvId = pageState.envId;
		}
	});

	$effect(() => {
		if (pageState.envId === previousEnvId) return;
		previousEnvId = pageState.envId;
		displayedEnvId = null;
		pageState.selectedIds = [];
		pageState.isCreateDialogOpen = false;
	});

	async function handleCreate(options: VolumeCreateRequest) {
		await createVolumeMutation.mutateAsync({ options, requestedEnvId: pageState.envId });
	}

	async function loadVolumes(options = pageState.requestOptions, requestedEnvId = pageState.envId) {
		pageState.requestOptions = options;
		const next = await queryClient.fetchQuery({
			queryKey: queryKeys.volumes.table(requestedEnvId, options),
			queryFn: () => volumeService.getVolumesForEnvironment(requestedEnvId, options)
		});
		if (requestedEnvId !== pageState.envId) {
			return;
		}
		pageState.items = next;
		displayedEnvId = requestedEnvId;
	}

	async function refresh() {
		await loadVolumes();
	}

	const isRefreshing = $derived(volumesQuery.isFetching && !volumesQuery.isPending);
	const volumeUsageCounts = $derived(resourcesReady ? (pageState.items.counts ?? countsFallback) : countsFallback);

	const canCreateVolume = $derived(hasPermission('volumes:create', pageState.envId));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateVolume) {
			buttons.push({
				id: 'create',
				action: 'create',
				label: m.common_create_button({ resource: m.resource_volume_cap() }),
				onclick: () => (pageState.isCreateDialogOpen = true),
				loading: createVolumeMutation.isPending,
				disabled: !resourcesReady || createVolumeMutation.isPending
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: volumesQuery.isFetching
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.volumes_stat_total(),
			value: volumeUsageCounts.total,
			icon: VolumesIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.unused_volumes(),
			value: volumeUsageCounts.unused,
			icon: VolumeUnusedIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.resource_volumes_cap()} subtitle={m.volumes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			<VolumeTable
				bind:volumes={pageState.items}
				bind:selectedIds={pageState.selectedIds}
				bind:requestOptions={pageState.requestOptions}
				onRefreshData={loadVolumes}
			/>
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<CreateVolumeSheet
			bind:open={pageState.isCreateDialogOpen}
			isLoading={createVolumeMutation.isPending}
			onSubmit={handleCreate}
		/>
	{/snippet}
</ResourcePageLayout>

<script lang="ts">
	// fallow-ignore-file code-duplication -- volume and network pages share ResourceListPageState lifecycle wiring but retain domain-specific queries and mutations
	import { VolumesIcon, VolumeUnusedIcon } from '#lib/icons/index.js';
	import { toast } from 'svelte-sonner';
	import CreateVolumeSheet from '#lib/components/sheets/create-volume-sheet.svelte';
	import type { VolumeCreateRequest, VolumeUsageCounts } from '#lib/types/docker.js';
	import VolumeTable from './volume-table.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { volumeService } from '#lib/services/volume-service.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { untrack } from 'svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.volumeRequestOptions));
	const countsFallback: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

	const volumesQuery = createQuery(() => {
		const queryEnvId = pageState.envId;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.volumes.table(queryEnvId, options),
			queryFn: () => volumeService.getVolumesForEnvironment(queryEnvId, options),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryEnvId) return previous;
				return undefined;
			},
			initialData: data.envId === queryEnvId ? data.volumes : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});

	const resourcesReady = $derived(volumesQuery.data?.envId === pageState.envId);
	const volumes = $derived(volumesQuery.data?.value ?? data.volumes);

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

	useEnvironmentRefresh(() => {
		pageState.selectedIds = [];
		pageState.isCreateDialogOpen = false;
	});

	async function handleCreate(options: VolumeCreateRequest) {
		await createVolumeMutation.mutateAsync({ options, requestedEnvId: pageState.envId });
	}

	async function loadVolumes(options = pageState.requestOptions, requestedEnvId = pageState.envId) {
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.volumes.table(requestedEnvId, options),
			queryFn: () => volumeService.getVolumesForEnvironment(requestedEnvId, options)
		});
	}

	async function refresh() {
		await loadVolumes();
	}

	const isRefreshing = $derived(volumesQuery.isFetching && !volumesQuery.isPending);
	const volumeUsageCounts = $derived.by(() => {
		if (resourcesReady) return volumes.counts ?? countsFallback;
		return countsFallback;
	});

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
			{#key pageState.envId}
				<VolumeTable
					bind:volumes={
						() => volumes,
						(value) => queryClient.setQueryData(queryKeys.volumes.table(pageState.envId, pageState.requestOptions), value)
					}
					bind:selectedIds={pageState.selectedIds}
					bind:requestOptions={pageState.requestOptions}
					onRefreshData={loadVolumes}
				/>
			{/key}
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

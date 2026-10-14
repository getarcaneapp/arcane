<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import CreateVolumeSheet from '#lib/components/sheets/create-volume-sheet.svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	// fallow-ignore-file code-duplication -- volume and network pages share ResourceListPageState lifecycle wiring but retain domain-specific queries and mutations
	import { VolumesIcon, VolumeUnusedIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { volumeService } from '#lib/services/volume-service.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { VolumeCreateRequest, VolumeUsageCounts } from '#lib/types/docker.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { fetchAcrossEnvironments, hasShownPermission } from '#lib/utils/environments.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';

	import VolumeTable from './components/volume-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.volumeRequestOptions));
	const countsFallback: VolumeUsageCounts = { inuse: 0, unused: 0, total: 0 };

	// The load already cached the first page under its key; seeding every key with it would hide other pages and filters.
	const volumesQuery = createQuery(() => {
		const queryScope = pageState.scope;
		const environments = environmentStore.shown;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.volumes.table(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					volumeService.getVolumesForEnvironment(id, request, config)
				),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});

	const resourcesReady = $derived(volumesQuery.data?.envId === pageState.scope);
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
			if (environmentStore.shown.some((env) => env.id === options.requestedEnvId)) {
				await loadVolumes();
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

	async function handleCreate(options: VolumeCreateRequest, environmentId: string) {
		await createVolumeMutation.mutateAsync({ options, requestedEnvId: environmentId });
	}

	async function loadVolumes(options = pageState.requestOptions, requestedScope = pageState.scope) {
		const environments = environmentStore.shown;
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.volumes.table(requestedScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) =>
					volumeService.getVolumesForEnvironment(id, request, config)
				)
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

	const canCreateVolume = $derived(hasShownPermission('volumes:create'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canCreateVolume) {
			buttons.push({
				id: 'create',
				action: 'create',
				placement: 'primary',
				label: m.common_create_button({ resource: m.resource_volume_cap() }),
				onclick: () => (pageState.isCreateDialogOpen = true),
				loading: createVolumeMutation.isPending,
				disabled: !resourcesReady || createVolumeMutation.isPending
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
			disabled: volumesQuery.isFetching
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.volumes_stat_total(),
			value: volumeUsageCounts.total,
			icon: VolumesIcon,
			iconColor: 'text-info'
		},
		{
			title: m.unused_volumes(),
			value: volumeUsageCounts.unused,
			icon: VolumeUnusedIcon,
			iconColor: 'text-warning'
		}
	]);
</script>

<ResourcePageLayout title={m.resource_volumes_cap()} subtitle={m.volumes_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			{#key pageState.scope}
				<VolumeTable
					bind:volumes={
						() => volumes,
						(value) => queryClient.setQueryData(queryKeys.volumes.table(pageState.scope, pageState.requestOptions), value)
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

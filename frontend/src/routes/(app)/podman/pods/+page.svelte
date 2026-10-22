<script lang="ts">
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import CreatePodSheet from '#lib/components/sheets/create-pod-sheet.svelte';
	import { useEnvironmentRefresh } from '#lib/hooks/use-environment-refresh.svelte.js';
	import { BoxIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { PERMISSIONS } from '#lib/permissions.generated.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { PodmanPodCreateRequest } from '#lib/types/podman.js';
	import type { SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { acrossEnvironmentsNote, fetchAcrossEnvironments, settleAcrossEnvironments } from '#lib/utils/environments.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import PodsTable from './components/pods-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.requestOptions));
	const podmanEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses));
	const manageableEnvironments = $derived(podmanEnvironments.filter((env) => hasPermission(PERMISSIONS.PODMAN_PODS, env.id)));
	let isLoading = $state({ create: false, prune: false });

	const podsQuery = createQuery(() => {
		const queryScope = pageState.scope;
		const environments = podmanEnvironments;
		const options = pageState.requestOptions;
		return {
			queryKey: queryKeys.podman.pods(queryScope, options),
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) => podmanService.getPods(id, request, config)),
			placeholderData: (previous, query) => {
				if (query?.queryKey[1] === queryScope) return previous;
				return undefined;
			},
			select: (value) => ({ envId: queryScope, value })
		};
	});

	const resourcesReady = $derived(podsQuery.data?.envId === pageState.scope);
	const pods = $derived(podsQuery.data?.value ?? data.pods);

	async function loadPods(options: SearchPaginationSortRequest = pageState.requestOptions, requestedScope = pageState.scope) {
		const environments = podmanEnvironments;
		pageState.requestOptions = options;
		await queryClient.query({
			queryKey: queryKeys.podman.pods(requestedScope, options),
			staleTime: 0,
			queryFn: () =>
				fetchAcrossEnvironments(environments, options, (id, request, config) => podmanService.getPods(id, request, config))
		});
	}

	async function refresh() {
		await handleApiResultWithCallbacks({
			result: await tryCatch(loadPods()),
			message: m.common_refresh_failed({ resource: m.podman_pods_title() })
		});
	}

	useEnvironmentRefresh(() => {
		pageState.isCreateDialogOpen = false;
	});

	async function handleCreate(request: PodmanPodCreateRequest, environmentId: string) {
		isLoading.create = true;
		await handleApiResultWithCallbacks({
			result: await tryCatch(podmanService.createPod(request, environmentId)),
			message: m.common_create_failed({ resource: request.name }),
			setLoadingState: (value) => (isLoading.create = value),
			onSuccess: async () => {
				toast.success(m.common_create_success({ resource: request.name }));
				pageState.isCreateDialogOpen = false;
				await refresh();
			}
		});
	}

	function handlePrune() {
		const environments = manageableEnvironments;
		openConfirmDialog({
			title: m.podman_pods_prune(),
			message: `${m.podman_pods_prune_confirm()} ${acrossEnvironmentsNote(environments)}`.trim(),
			confirm: {
				label: m.podman_pods_prune(),
				destructive: true,
				action: async () => {
					isLoading.prune = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(settleAcrossEnvironments(environments, (id) => podmanService.prunePods(id))),
						message: m.podman_pods_prune_failed(),
						setLoadingState: (value) => (isLoading.prune = value),
						onSuccess: async (results) => {
							const count = results.reduce((total, { value }) => total + (value?.length ?? 0), 0);
							toast.success(m.podman_pods_prune_success({ count }));
							await refresh();
						}
					});
				}
			}
		});
	}

	const isRefreshing = $derived(podsQuery.isFetching && !podsQuery.isPending);

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (manageableEnvironments.length > 0) {
			buttons.push(
				{
					id: 'create',
					action: 'create',
					placement: 'primary',
					label: m.common_create_button({ resource: m.podman_pod() }),
					onclick: () => (pageState.isCreateDialogOpen = true),
					loading: isLoading.create,
					disabled: !resourcesReady || isLoading.create
				},
				{
					id: 'prune',
					action: 'prune',
					placement: 'secondary',
					label: m.podman_pods_prune(),
					onclick: handlePrune,
					loading: isLoading.prune,
					disabled: isLoading.prune
				}
			);
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: podsQuery.isFetching
		});
		return buttons;
	});

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.podman_pods_total(),
			value: resourcesReady ? (pods.pagination?.totalItems ?? pods.data.length) : 0,
			icon: BoxIcon,
			iconColor: 'text-info'
		}
	]);
</script>

<ResourcePageLayout title={m.podman_pods_title()} subtitle={m.podman_pods_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			{#key pageState.scope}
				<PodsTable
					bind:pods={
						() => pods,
						(value) => queryClient.setQueryData(queryKeys.podman.pods(pageState.scope, pageState.requestOptions), value)
					}
					bind:requestOptions={pageState.requestOptions}
					onRefreshData={loadPods}
				/>
			{/key}
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<CreatePodSheet
			bind:open={pageState.isCreateDialogOpen}
			isLoading={isLoading.create}
			environments={manageableEnvironments}
			onSubmit={handleCreate}
		/>
	{/snippet}
</ResourcePageLayout>

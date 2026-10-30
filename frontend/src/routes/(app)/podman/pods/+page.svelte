<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import CreatePodSheet from '#lib/components/sheets/create-pod-sheet.svelte';
	import { BoxIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { podmanPodListQuery } from '#lib/query/podman.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService } from '#lib/services/podman-service.js';
	import type { EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanPodCreateRequest } from '#lib/types/podman.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { acrossEnvironmentsNote, settleAcrossEnvironments } from '#lib/utils/environments.js';
	import { shownPodmanEnvironments } from '#lib/utils/podman.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import PodsTable from './components/pods-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.requestOptions));
	const podmanEnvironments = $derived(shownPodmanEnvironments(data.podmanStatuses));
	const manageableEnvironments = $derived(podmanEnvironments.filter((env) => hasPermission('podman:pods', env.id)));

	const podsQuery = createQuery(() => {
		const scope = pageState.scope;
		return {
			...podmanPodListQuery(scope, podmanEnvironments, pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});

	const resourcesReady = $derived(podsQuery.data?.envId === pageState.scope);
	const pods = $derived(podsQuery.data?.value ?? data.pods);

	const createPodMutation = createMutation(() => ({
		mutationKey: ['podman-pods', 'create', pageState.scope],
		mutationFn: ({ request, environmentId }: { request: PodmanPodCreateRequest; environmentId: string }) =>
			podmanService.createPod(request, environmentId),
		onSuccess: async (_data, { request }) => {
			toast.success(m.common_create_success({ resource: request.name }));
			pageState.isCreateDialogOpen = false;
			await queryClient.invalidateQueries({ queryKey: queryKeys.podman.podsPrefix(pageState.scope) });
		},
		onError: (error, { request }) => {
			toast.error(m.common_create_failed({ resource: request.name }), { description: extractApiErrorMessage(error) });
		}
	}));

	const prunePodsMutation = createMutation(() => ({
		mutationKey: ['podman-pods', 'prune', pageState.scope],
		mutationFn: (environments: EnvironmentRef[]) => settleAcrossEnvironments(environments, (id) => podmanService.prunePods(id)),
		onSuccess: async (results) => {
			const count = results.reduce((total, { value }) => total + (value?.length ?? 0), 0);
			toast.success(m.podman_pods_prune_success({ count }));
			await queryClient.invalidateQueries({ queryKey: queryKeys.podman.podsPrefix(pageState.scope) });
		},
		onError: (error) => {
			toast.error(m.podman_pods_prune_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	async function handleCreate(request: PodmanPodCreateRequest, environmentId: string) {
		const result = await tryCatch(createPodMutation.mutateAsync({ request, environmentId }));
		return result.error;
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
					await tryCatch(prunePodsMutation.mutateAsync(environments));
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
					loading: createPodMutation.isPending,
					disabled: !resourcesReady || createPodMutation.isPending
				},
				{
					id: 'prune',
					action: 'prune',
					placement: 'secondary',
					label: m.podman_pods_prune(),
					onclick: handlePrune,
					loading: prunePodsMutation.isPending,
					disabled: prunePodsMutation.isPending
				}
			);
		}
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: () => podsQuery.refetch(),
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
				<PodsTable {pods} bind:requestOptions={pageState.requestOptions} onRetry={() => podsQuery.refetch()} />
			{/key}
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<CreatePodSheet
			bind:open={pageState.isCreateDialogOpen}
			isLoading={createPodMutation.isPending}
			environments={manageableEnvironments}
			onSubmit={handleCreate}
		/>
	{/snippet}
</ResourcePageLayout>

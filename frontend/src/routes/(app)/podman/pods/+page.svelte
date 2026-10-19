<script lang="ts">
	import { goto } from '$app/navigation';
	import { createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ArcaneTable from '#lib/components/arcane-table/arcane-table.svelte';
	import { UniversalMobileCard } from '#lib/components/arcane-table/index.js';
	import type { ColumnSpec, MobileFieldVisibility } from '#lib/components/arcane-table/index.js';
	import RowActionsMenu from '#lib/components/arcane-table/row-actions-menu.svelte';
	import { openConfirmDialog } from '#lib/components/confirm-dialog/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import EmptyState from '#lib/components/states/empty-state.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { BoxIcon, ContainersIcon, StartIcon, TrashIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { podmanService, type PodmanPodAction } from '#lib/services/podman-service.js';
	import { STREAM_CHANNEL_EVENTS } from '#lib/services/stream-service.js';
	import { clientStream } from '#lib/stores/client-stream.svelte.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { PodmanPod } from '#lib/types/podman.js';
	import type { Paginated, SearchPaginationSortRequest } from '#lib/types/shared.js';
	import { extractApiErrorMessage, handleApiResultWithCallbacks } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDateTime } from '#lib/utils/formatting.js';
	import { podmanStateVariant } from '#lib/utils/podman.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { confirmRemovePod, podActionLabels, runPodAction } from './pod-actions';

	const queryClient = useQueryClient();
	const currentEnvId = $derived(environmentStore.selected?.id ?? '0');
	const canManagePods = $derived(hasPermission('podman:pods', currentEnvId));

	const podsQuery = createQuery(() => ({
		queryKey: queryKeys.podman.pods(currentEnvId),
		queryFn: () => podmanService.getPods(),
		refetchInterval: 10000,
		retry: false
	}));

	const pods = $derived(podsQuery.data ?? []);
	const runningPods = $derived(pods.filter((pod) => pod.status.toLowerCase() === 'running').length);

	let requestOptions = $state<SearchPaginationSortRequest>({ pagination: { page: 1, limit: 20 } });
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let isPruning = $state(false);

	function buildTableData(items: PodmanPod[]): Paginated<PodmanPod> {
		return {
			data: items,
			pagination: {
				totalPages: 1,
				totalItems: items.length,
				currentPage: 1,
				itemsPerPage: Math.max(items.length, 1)
			}
		};
	}

	const tableData = $derived(buildTableData(pods));

	async function refresh() {
		await queryClient.invalidateQueries({ queryKey: queryKeys.podman.pods(currentEnvId) });
	}

	onMount(() =>
		clientStream.subscribe(STREAM_CHANNEL_EVENTS, {
			onEvent: (event) => {
				if (typeof event === 'object' && event !== null && 'type' in event && event.type === 'changed') {
					void queryClient.invalidateQueries({ queryKey: queryKeys.podman.environment(currentEnvId) });
				}
			}
		})
	);

	function handlePrune() {
		openConfirmDialog({
			title: m.podman_pods_prune(),
			message: m.podman_pods_prune_confirm(),
			confirm: {
				label: m.podman_pods_prune(),
				destructive: true,
				action: async () => {
					isPruning = true;
					await handleApiResultWithCallbacks({
						result: await tryCatch(podmanService.prunePods()),
						message: m.podman_pods_prune_failed(),
						setLoadingState: (value) => (isPruning = value),
						onSuccess: async (reports) => {
							toast.success(m.podman_pods_prune_success({ count: reports?.length ?? 0 }));
							await refresh();
						}
					});
				}
			}
		});
	}

	const actionButtons: ActionButton[] = $derived([
		...(canManagePods
			? [
					{
						id: 'create',
						action: 'create' as const,
						placement: 'primary' as const,
						label: m.common_create_button({ resource: m.podman_pod() }),
						href: '/podman/pods/new'
					},
					{
						id: 'prune',
						action: 'prune' as const,
						placement: 'secondary' as const,
						label: m.podman_pods_prune(),
						loading: isPruning,
						disabled: isPruning,
						onclick: handlePrune
					}
				]
			: []),
		{
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: podsQuery.isFetching,
			disabled: podsQuery.isFetching
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{ title: m.podman_pods_total(), value: pods.length, icon: BoxIcon, iconColor: 'text-info' },
		{ title: m.common_running(), value: runningPods, icon: StartIcon, iconColor: 'text-success' }
	]);

	const lifecycleActions: PodmanPodAction[] = ['start', 'stop', 'restart', 'pause', 'unpause', 'kill'];

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'status', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'containers', title: m.containers(), cell: ContainersCell },
		{ accessorKey: 'networks', title: m.resource_networks_cap(), cell: NetworksCell },
		{ accessorKey: 'created', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<PodmanPod>[];

	const mobileFields = [
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'containers', label: m.containers(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: false }
	];
</script>

<ResourcePageLayout
	title={m.podman_pods_title()}
	subtitle={m.podman_pods_subtitle()}
	icon={BoxIcon}
	{actionButtons}
	{statCards}
	environmentPicker
>
	{#snippet mainContent()}
		{#if podsQuery.isError}
			<EmptyState
				icon={BoxIcon}
				title={m.common_load_failed({ resource: m.podman_pods_title() })}
				description={extractApiErrorMessage(podsQuery.error)}
				actionLabel={m.common_retry()}
				onAction={() => podsQuery.refetch()}
			/>
		{:else if !podsQuery.isPending && pods.length === 0}
			<EmptyState icon={BoxIcon} title={m.podman_pods_empty()} />
		{:else}
			<ArcaneTable
				persistKey="arcane-podman-pods-table"
				items={tableData}
				bind:requestOptions
				bind:mobileFieldVisibility
				loading={podsQuery.isPending}
				selectionDisabled
				withoutSearch
				withoutPagination
				onRefresh={async () => {
					await refresh();
					return buildTableData(pods);
				}}
				{columns}
				{mobileFields}
				rowActions={RowActions}
				mobileCard={PodMobileCard}
			/>
		{/if}
	{/snippet}
</ResourcePageLayout>

{#snippet NameCell({ item }: { item: PodmanPod })}
	<a class="font-medium hover:underline" href={`/podman/pods/${encodeURIComponent(item.id)}`}>{item.name}</a>
{/snippet}

{#snippet StatusCell({ item }: { item: PodmanPod })}
	<Badge variant={podmanStateVariant(item.status)} minWidth="20">{item.status}</Badge>
{/snippet}

{#snippet ContainersCell({ item }: { item: PodmanPod })}
	<span class="text-muted-foreground">{item.containers?.length ?? 0}</span>
{/snippet}

{#snippet NetworksCell({ item }: { item: PodmanPod })}
	<span class="text-muted-foreground">{item.networks?.join(', ') || '-'}</span>
{/snippet}

{#snippet CreatedCell({ item }: { item: PodmanPod })}
	<span class="text-muted-foreground">{formatDateTime(item.created) || item.created}</span>
{/snippet}

{#snippet PodMobileCard({ item, mobileFieldVisibility }: { item: PodmanPod; mobileFieldVisibility: MobileFieldVisibility })}
	<UniversalMobileCard
		{item}
		icon={{ component: BoxIcon, variant: 'blue' }}
		title={(item: PodmanPod) => item.name}
		badges={[(item: PodmanPod) => ({ variant: podmanStateVariant(item.status), text: item.status })]}
		fields={[
			{
				label: m.containers(),
				getValue: (item: PodmanPod) => String(item.containers?.length ?? 0),
				icon: ContainersIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['containers'] ?? true
			},
			{
				label: m.common_created(),
				getValue: (item: PodmanPod) => formatDateTime(item.created) || item.created,
				icon: BoxIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility['created'] ?? false
			}
		]}
		rowActions={RowActions}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: PodmanPod })}
	<RowActionsMenu>
		<DropdownMenu.Item onclick={() => goto(`/podman/pods/${encodeURIComponent(item.id)}`)}>
			{m.common_inspect()}
		</DropdownMenu.Item>
		<IfPermitted perm="podman:pods">
			<DropdownMenu.Separator />
			{#each lifecycleActions as action (action)}
				<DropdownMenu.Item onclick={() => runPodAction(item, action, refresh)}>
					{podActionLabels[action]()}
				</DropdownMenu.Item>
			{/each}
			<DropdownMenu.Separator />
			<DropdownMenu.Item variant="destructive" onclick={() => confirmRemovePod(item, refresh)}>
				<TrashIcon class="size-4" />
				{m.common_remove()}
			</DropdownMenu.Item>
		</IfPermitted>
	</RowActionsMenu>
{/snippet}

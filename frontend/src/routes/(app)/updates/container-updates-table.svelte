<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { UniversalMobileCard, type ColumnSpec, type MobileFieldVisibility } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import type { SearchPaginationSortRequest, Paginated } from '$lib/types/shared';
	import type { ContainerSummaryDto } from '$lib/types/docker';
	import type { ImageUpdateInfoDto } from '$lib/types/docker';
	import type { ContainersPaginatedResponse, ContainerListRequestOptions } from '$lib/services/container-service';
	import { ContainersIcon, UpdateIcon } from '$lib/icons';
	import { getContainerDisplayName } from '../containers/container-table.helpers';
	import IfPermitted from '$lib/components/if-permitted.svelte';
	import { confirmAndUpdateContainer } from '$lib/utils/container-actions';
	import { formatImageUpdateCheckedAt, formatImageUpdateValue } from '$lib/utils/image-updates';

	type ContainerUpdateRow = {
		id: string;
		containerId: string;
		name: string;
		imageRef: string;
		currentValue: string;
		latestValue: string;
		checkedAt: string;
		updateInfo?: ImageUpdateInfoDto;
		container: ContainerSummaryDto;
	};

	interface Props {
		containers: ContainersPaginatedResponse;
		requestOptions: SearchPaginationSortRequest;
		onRefreshData: (options: ContainerListRequestOptions) => Promise<ContainersPaginatedResponse>;
	}

	let { containers = $bindable(), requestOptions = $bindable(), onRefreshData }: Props = $props();

	let selectedIds = $state<string[]>([]);
	let mobileFieldVisibility = $state<MobileFieldVisibility>({});
	let updatingContainerIds = $state<Record<string, boolean>>({});

	function mapContainerRow(container: ContainerSummaryDto): ContainerUpdateRow {
		return {
			id: container.id,
			containerId: container.id,
			name: getContainerDisplayName(container),
			imageRef: container.image,
			currentValue: formatImageUpdateValue(container.updateInfo, 'current'),
			latestValue: formatImageUpdateValue(container.updateInfo, 'latest'),
			checkedAt: container.updateInfo?.checkTime ?? '',
			updateInfo: container.updateInfo,
			container
		};
	}

	const tableItems = $derived<Paginated<ContainerUpdateRow, ContainersPaginatedResponse['counts']>>({
		...containers,
		data: (containers.data ?? []).map(mapContainerRow)
	});

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'imageRef', title: m.common_image(), sortable: true, cell: ImageCell },
		{ accessorKey: 'currentValue', title: m.image_update_current_label(), sortable: false, cell: DigestCell },
		{ accessorKey: 'latestValue', title: m.image_update_latest_digest_label(), sortable: false, cell: DigestCell },
		{ accessorKey: 'checkedAt', title: m.common_updated(), sortable: false, cell: CheckedAtCell },
		{ id: 'actions', title: m.common_actions(), sortable: false, cell: ActionsCell }
	] satisfies ColumnSpec<ContainerUpdateRow>[];

	const mobileFields = [
		{ id: 'imageRef', label: m.common_image(), defaultVisible: true },
		{ id: 'currentValue', label: m.image_update_current_label(), defaultVisible: true },
		{ id: 'latestValue', label: m.image_update_latest_digest_label(), defaultVisible: true },
		{ id: 'checkedAt', label: m.common_updated(), defaultVisible: true },
		{ id: 'actions', label: m.common_actions(), defaultVisible: true }
	];

	async function handleUpdateContainer(container: ContainerSummaryDto) {
		const containerName = getContainerDisplayName(container);

		confirmAndUpdateContainer({
			containerId: container.id,
			containerName,
			showPullingToast: true,
			setLoading: (loading) => {
				updatingContainerIds = { ...updatingContainerIds, [container.id]: loading };
			},
			onRefresh: async () => {
				const next = await onRefreshData(requestOptions as ContainerListRequestOptions);
				containers = next;
			}
		});
	}
</script>

{#snippet NameCell({ item }: { item: ContainerUpdateRow })}
	<a class="font-medium hover:underline" href={`/containers/${item.containerId}`}>
		{item.name}
	</a>
{/snippet}

{#snippet ImageCell({ item }: { item: ContainerUpdateRow })}
	<code class="text-xs">{item.imageRef}</code>
{/snippet}

{#snippet DigestCell({ value }: { value: unknown })}
	{@const text = typeof value === 'string' ? value : '-'}
	<span class="font-mono text-xs break-all whitespace-normal" title={text !== '-' ? text : undefined}>
		{text}
	</span>
{/snippet}

{#snippet CheckedAtCell({ value }: { value: unknown })}
	<span class="text-sm">{formatImageUpdateCheckedAt(typeof value === 'string' ? value : '')}</span>
{/snippet}

{#snippet ActionsCell({ item }: { item: ContainerUpdateRow })}
	<IfPermitted perm="containers:autoupdate">
		<ArcaneButton
			action="update"
			size="sm"
			customLabel={m.containers_update_container()}
			onclick={() => handleUpdateContainer(item.container)}
			loading={!!updatingContainerIds[item.containerId]}
			disabled={!!updatingContainerIds[item.containerId]}
			icon={UpdateIcon}
		/>
	</IfPermitted>
{/snippet}

{#snippet ContainerUpdatesMobileCard({ item }: { item: ContainerUpdateRow })}
	<UniversalMobileCard
		{item}
		icon={() => ({
			component: ContainersIcon,
			variant: 'blue' as const
		})}
		title={(item: ContainerUpdateRow) => item.name}
		subtitle={(item: ContainerUpdateRow) => item.imageRef}
		fields={[
			{
				label: m.image_update_current_label(),
				getValue: (item: ContainerUpdateRow) => item.currentValue
			},
			{
				label: m.image_update_latest_digest_label(),
				getValue: (item: ContainerUpdateRow) => item.latestValue
			},
			{
				label: m.common_updated(),
				getValue: (item: ContainerUpdateRow) => formatImageUpdateCheckedAt(item.checkedAt)
			},
			{
				label: m.common_actions(),
				getValue: () => m.containers_update_container()
			}
		]}
		onclick={(item: ContainerUpdateRow) => {
			window.location.href = `/containers/${item.containerId}`;
		}}
	/>
{/snippet}

<ArcaneTable
	persistKey="arcane-updates-container-table"
	items={tableItems}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRefresh={async (options) => {
		requestOptions = options;
		const next = await onRefreshData(options as ContainerListRequestOptions);
		containers = next;
		return {
			...next,
			data: (next.data ?? []).map(mapContainerRow)
		};
	}}
	{columns}
	{mobileFields}
	mobileCard={ContainerUpdatesMobileCard}
	withoutFilters
	selectionDisabled
/>

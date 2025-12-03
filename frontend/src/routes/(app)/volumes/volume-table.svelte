<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import ScanSearchIcon from '@lucide/svelte/icons/scan-search';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import { format } from 'date-fns';
	import { truncateString } from '$lib/utils/string.utils';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { VolumeSummaryDto } from '$lib/types/volume.type';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import DatabaseIcon from '@lucide/svelte/icons/database';
	import CalendarIcon from '@lucide/svelte/icons/calendar';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import bytes from 'bytes';

	let {
		volumes = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable()
	}: {
		volumes: Paginated<VolumeSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
	} = $props();

	let isLoading = $state({
		removing: false
	});

	async function handleRemoveVolumeConfirm(name: string) {
		const safeName = name?.trim() || m.common_unknown();
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_volume() }),
			message: m.common_remove_confirm({ resource: `${m.resource_volume()} "${safeName}"` }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					handleApiResultWithCallbacks({
						result: await tryCatch(volumeService.deleteVolume(safeName)),
						message: m.common_remove_failed({ resource: `${m.resource_volume()} "${safeName}"` }),
						setLoadingState: (value) => (isLoading.removing = value),
						onSuccess: async () => {
							toast.success(m.common_remove_success({ resource: `${m.resource_volume()} "${safeName}"` }));
							volumes = await volumeService.getVolumes(requestOptions);
						}
					});
				}
			}
		});
	}

	async function handleDeleteSelected(ids: string[]) {
		if (!ids?.length) return;
		isLoading.removing = true;
		let successCount = 0;
		let failureCount = 0;

		const idToName = new Map(volumes.data.map((v) => [v.id, v.name] as const));

		for (const id of ids) {
			const name = idToName.get(id);
			const safeName = name?.trim() || m.common_unknown();
			const result = await tryCatch(volumeService.deleteVolume(safeName));
			handleApiResultWithCallbacks({
				result,
				message: m.common_remove_failed({ resource: `${m.resource_volume()} "${safeName}"` }),
				setLoadingState: () => {},
				onSuccess: (_data) => {
					successCount += 1;
				}
			});
			if (result.error) failureCount += 1;
		}

		isLoading.removing = false;
		if (successCount > 0) {
			const successMsg = m.common_bulk_remove_success({ count: successCount, resource: m.volumes_title() });
			toast.success(successMsg);
			volumes = await volumeService.getVolumes(requestOptions);
		}
		if (failureCount > 0) {
			const failureMsg = m.common_bulk_remove_failed({ count: failureCount, resource: m.volumes_title() });
			toast.error(failureMsg);
		}
		selectedIds = [];
	}

	const columns = [
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			accessorKey: 'inUse',
			title: m.common_status(),
			sortable: true,
			cell: StatusCell
		},
		{
			accessorKey: 'size',
			title: m.common_size(),
			sortable: true,
			cell: SizeCell
		},
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true }
	] satisfies ColumnSpec<VolumeSummaryDto>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: true },
		{ id: 'status', label: m.common_status(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'createdAt', label: m.common_created(), defaultVisible: true },
		{ id: 'driver', label: m.common_driver(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet NameCell({ item }: { item: VolumeSummaryDto })}
	<a class="font-medium hover:underline" href="/volumes/{item.id}" title={item.name}>
		{truncateString(item.name, 40)}
	</a>
{/snippet}

{#snippet StatusCell({ item }: { item: VolumeSummaryDto })}
	{#if item.inUse}
		<StatusBadge text={m.common_in_use()} variant="green" />
	{:else}
		<StatusBadge text={m.common_unused()} variant="amber" />
	{/if}
{/snippet}

{#snippet SizeCell({ item }: { item: VolumeSummaryDto })}
	{#if item.size >= 0}
		<span class="text-sm tabular-nums">{bytes.format(item.size)}</span>
	{:else}
		<span class="text-muted-foreground text-sm">-</span>
	{/if}
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{format(new Date(String(value)), 'PP p')}
{/snippet}

{#snippet VolumeMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: VolumeSummaryDto;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => ({
			component: DatabaseIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		badges={[
			(item) =>
				(mobileFieldVisibility.status ?? true)
					? item.inUse
						? { variant: 'green' as const, text: m.common_in_use() }
						: { variant: 'amber' as const, text: m.common_unused() }
					: null
		]}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: VolumeSummaryDto) => item.driver,
				icon: DatabaseIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.driver ?? true
			},
			{
				label: m.common_size(),
				getValue: (item: VolumeSummaryDto) =>
					item.usageData && item.usageData.size >= 0 ? bytes.format(item.usageData.size) : '-',
				icon: HardDriveIcon,
				iconVariant: 'gray' as const,
				show: (mobileFieldVisibility.size ?? true) && !!item.usageData
			}
		]}
		footer={(mobileFieldVisibility.createdAt ?? true)
			? {
					label: m.common_created(),
					getValue: (item) => format(new Date(String(item.createdAt)), 'PP p'),
					icon: CalendarIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={() => goto(`/volumes/${item.id}`)}
	/>
{/snippet}

{#snippet RowActions({ item }: { item: VolumeSummaryDto })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon" class="relative size-8 p-0">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/volumes/${item.id}`)}>
					<ScanSearchIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Item variant="destructive" onclick={() => handleRemoveVolumeConfirm(item.name)} disabled={item.inUse}>
					<Trash2Icon class="size-4" />
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-volumes-table"
	items={volumes}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected(ids)}
	onRefresh={async (options) => (volumes = await volumeService.getVolumes(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={VolumeMobileCardSnippet}
/>

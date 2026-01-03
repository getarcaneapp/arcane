<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
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
	import type { VolumeSummaryDto, VolumeSizeInfo } from '$lib/types/volume.type';
	import type { ColumnSpec, MobileFieldVisibility } from '$lib/components/arcane-table';
	import { UniversalMobileCard } from '$lib/components/arcane-table/index.js';
	import { m } from '$lib/paraglide/messages';
	import { volumeService } from '$lib/services/volume-service';
	import bytes from 'bytes';
	import { TrashIcon, EllipsisIcon, InspectIcon, VolumesIcon, CalendarIcon } from '$lib/icons';
	import { Spinner } from '$lib/components/ui/spinner';

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

	// Lazy load volume sizes - this is a slow operation
	let volumeSizesPromise = $state<Promise<Map<string, VolumeSizeInfo>> | null>(null);

	// Start loading sizes when component mounts or volumes change
	$effect(() => {
		if (volumes.data.length > 0) {
			volumeSizesPromise = loadVolumeSizes();
		}
	});

	async function loadVolumeSizes(): Promise<Map<string, VolumeSizeInfo>> {
		const sizes = await volumeService.getVolumeSizes();
		return new Map(sizes.map((s) => [s.name, s]));
	}

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

		openConfirmDialog({
			title: m.volumes_remove_selected_title({ count: ids.length }),
			message: m.volumes_remove_selected_message({ count: ids.length }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
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
			}
		});
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'name', title: m.common_name(), sortable: true, cell: NameCell },
		{ accessorKey: 'inUse', title: m.common_status(), sortable: true, cell: StatusCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'createdAt', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'driver', title: m.common_driver(), sortable: true }
	] satisfies ColumnSpec<VolumeSummaryDto>[];

	const mobileFields = [
		{ id: 'inUse', label: m.common_status(), defaultVisible: true },
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
	{#if volumeSizesPromise}
		{#await volumeSizesPromise}
			<span class="text-muted-foreground flex items-center gap-1 text-sm">
				<Spinner class="size-4" />
			</span>
		{:then sizesMap}
			{@const sizeInfo = sizesMap.get(item.name)}
			{#if sizeInfo && sizeInfo.size >= 0}
				<span class="text-sm tabular-nums">{bytes.format(sizeInfo.size)}</span>
			{:else}
				<span class="text-muted-foreground text-sm">-</span>
			{/if}
		{:catch}
			<span class="text-muted-foreground text-sm">-</span>
		{/await}
	{:else}
		<span class="text-muted-foreground text-sm">-</span>
	{/if}
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{format(new Date(String(value)), 'PP p')}
{/snippet}

{#snippet VolumeMobileCardSnippet({
	item,
	mobileFieldVisibility
}: {
	item: VolumeSummaryDto;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => ({
			component: VolumesIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item) => item.name}
		subtitle={(item) => ((mobileFieldVisibility.id ?? true) ? item.id : null)}
		badges={[
			(item) =>
				(mobileFieldVisibility.inUse ?? true)
					? item.inUse
						? { variant: 'green' as const, text: m.common_in_use() }
						: { variant: 'amber' as const, text: m.common_unused() }
					: null
		]}
		fields={[
			{
				label: m.common_driver(),
				getValue: (item: VolumeSummaryDto) => item.driver,
				icon: VolumesIcon,
				iconVariant: 'gray' as const,
				show: mobileFieldVisibility.driver ?? true
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
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					size="icon"
					class="relative size-8 p-0"
					icon={EllipsisIcon}
					showLabel={false}
					customLabel={m.common_open_menu()}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/volumes/${item.id}`)}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Item variant="destructive" onclick={() => handleRemoveVolumeConfirm(item.name)} disabled={item.inUse}>
					<TrashIcon class="size-4" />
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

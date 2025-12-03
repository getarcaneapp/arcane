<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import ScanSearchIcon from '@lucide/svelte/icons/scan-search';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto } from '$app/navigation';
	import { toast } from 'svelte-sonner';
	import bytes from 'bytes';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import ImageUpdateItem from '$lib/components/image-update-item.svelte';
	import UniversalMobileCard from '$lib/components/arcane-table/cards/universal-mobile-card.svelte';
	import ImageIcon from '@lucide/svelte/icons/image';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ImageSummaryDto, ImageUpdateInfoDto } from '$lib/types/image.type';
	import { format } from 'date-fns';
	import type { ColumnSpec } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';

	let {
		images = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onImageUpdated
	}: {
		images: Paginated<ImageSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onImageUpdated?: () => Promise<void>;
	} = $props();

	let isLoading = $state({
		removing: false,
		checking: false
	});

	let isPullingInline = $state<Record<string, boolean>>({});

	async function handleDeleteSelected(ids: string[]) {
		if (!ids || ids.length === 0) return;

		openConfirmDialog({
			title: m.images_remove_selected_title({ count: ids.length }),
			message: m.images_remove_selected_message({ count: ids.length }),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;
					let successCount = 0;
					let failureCount = 0;

					for (const id of ids) {
						const result = await tryCatch(imageService.deleteImage(id));
						handleApiResultWithCallbacks({
							result,
							message: m.images_remove_failed(),
							setLoadingState: () => {},
							onSuccess: () => {
								successCount++;
							}
						});
						if (result.error) failureCount++;
					}

					isLoading.removing = false;

					if (successCount > 0) {
						const msg =
							successCount === 1 ? m.images_remove_success_one() : m.images_remove_success_many({ count: successCount });
						toast.success(msg);
						images = await imageService.getImages(requestOptions);
					}
					if (failureCount > 0) {
						const msg = failureCount === 1 ? m.images_remove_failed_one() : m.images_remove_failed_many({ count: failureCount });
						toast.error(msg);
					}

					selectedIds = [];
				}
			}
		});
	}

	async function deleteImage(id: string) {
		openConfirmDialog({
			title: m.common_remove_title({ resource: m.resource_image() }),
			message: m.images_remove_message(),
			confirm: {
				label: m.common_remove(),
				destructive: true,
				action: async () => {
					isLoading.removing = true;

					const result = await tryCatch(imageService.deleteImage(id));
					handleApiResultWithCallbacks({
						result,
						message: m.images_remove_failed(),
						setLoadingState: () => {},
						onSuccess: async () => {
							toast.success(m.images_remove_success());
							images = await imageService.getImages(requestOptions);
						}
					});

					isLoading.removing = false;
				}
			}
		});
	}
	async function handleInlineImagePull(imageId: string, repoTag: string) {
		if (!repoTag || repoTag === '<none>:<none>') {
			toast.error(m.images_pull_no_tag());
			return;
		}

		isPullingInline[imageId] = true;

		const result = await tryCatch(imageService.pullImage(repoTag));
		handleApiResultWithCallbacks({
			result,
			message: m.images_pull_failed(),
			setLoadingState: () => {},
			onSuccess: async () => {
				toast.success(m.images_pull_success({ repoTag }));
				images = await imageService.getImages(requestOptions);
			}
		});

		isPullingInline[imageId] = false;
	}

	async function handleUpdateInfoChanged(imageId: string, newUpdateInfo: ImageUpdateInfoDto) {
		const imageIndex = images.data.findIndex((img) => img.id === imageId);
		if (imageIndex !== -1) {
			images.data[imageIndex].updateInfo = newUpdateInfo;
			images = { ...images, data: [...images.data] };
		}
		await onImageUpdated?.();
	}

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{
			id: 'updates',
			accessorFn: (row) => row.updateInfo?.hasUpdate ?? false,
			title: m.images_updates(),
			cell: UpdatesCell
		},
		{
			accessorKey: 'inUse',
			title: m.common_status(),
			sortable: true,
			cell: StatusCell
		},
		{ accessorKey: 'created', title: m.common_created(), sortable: true, cell: CreatedCell },
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'repo', title: m.images_repository(), sortable: true, cell: RepoCell },
		{ accessorKey: 'repoTags', title: m.common_tags(), cell: TagCell }
	] satisfies ColumnSpec<ImageSummaryDto>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: true },
		{ id: 'updates', label: m.images_updates(), defaultVisible: true },
		{ id: 'inUse', label: m.common_in_use(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: true },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'repo', label: m.images_repository(), defaultVisible: true },
		{ id: 'repoTags', label: m.common_tags(), defaultVisible: true }
	];

	let mobileFieldVisibility = $state<Record<string, boolean>>({});
</script>

{#snippet RepoCell({ item }: { item: ImageSummaryDto })}
	{#if item.repo && item.repo !== '<none>'}
		<a class="font-medium hover:underline" href="/images/{item.id}">{item.repo}</a>
	{:else}
		<span class="text-muted-foreground italic">{m.images_untagged()}</span>
	{/if}
{/snippet}

{#snippet TagCell({ item }: { item: ImageSummaryDto })}
	{#if item.repoTags && item.repoTags.length > 0 && item.repoTags[0] !== '<none>:<none>'}
		<div class="flex flex-wrap gap-1.5">
			{#each item.repoTags.slice(0, 2) as repoTag}
				{@const tag = repoTag.split(':').pop() || repoTag}
				<Badge variant="outline" class="font-mono text-xs">{tag}</Badge>
			{/each}
			{#if item.repoTags.length > 2}
				<Badge variant="outline" class="text-xs">+{item.repoTags.length - 2}</Badge>
			{/if}
		</div>
	{:else if item.tag && item.tag !== '<none>'}
		<Badge variant="outline" class="font-mono text-xs">{item.tag}</Badge>
	{:else}
		<span class="text-muted-foreground italic">{m.images_untagged()}</span>
	{/if}
{/snippet}

{#snippet SizeCell({ value }: { value: unknown })}
	{bytes.format(Number(value ?? 0))}
{/snippet}

{#snippet CreatedCell({ value }: { value: unknown })}
	{format(new Date(Number(value || 0) * 1000), 'PP p')}
{/snippet}

{#snippet StatusCell({ item }: { item: ImageSummaryDto })}
	{#if item.inUse}
		<StatusBadge text={m.common_in_use()} variant="green" />
	{:else}
		<StatusBadge text={m.common_unused()} variant="amber" />
	{/if}
{/snippet}

{#snippet UpdatesCell({ item }: { item: ImageSummaryDto })}
	<ImageUpdateItem
		updateInfo={item.updateInfo}
		imageId={item.id}
		repo={item.repo}
		tag={item.tag}
		onUpdated={(newInfo) => handleUpdateInfoChanged(item.id, newInfo)}
	/>
{/snippet}

{#snippet ImageMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: ImageSummaryDto;
	mobileFieldVisibility: Record<string, boolean>;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => ({
			component: ImageIcon,
			variant: item.inUse ? 'emerald' : 'amber'
		})}
		title={(item) => {
			if (item.repo && item.repo !== '<none>') return item.repo;
			return m.images_untagged();
		}}
		subtitle={(item) => ((mobileFieldVisibility.id ?? false) ? item.id : null)}
		badges={[
			(item: ImageSummaryDto) =>
				(mobileFieldVisibility.inUse ?? true)
					? item.inUse
						? { variant: 'green' as const, text: m.common_in_use() }
						: { variant: 'amber' as const, text: m.common_unused() }
					: null
		]}
		fields={[
			{
				label: m.common_size(),
				getValue: (item: ImageSummaryDto) => bytes.format(Number(item.size ?? 0)),
				icon: HardDriveIcon,
				iconVariant: 'blue' as const,
				show: mobileFieldVisibility.size ?? true
			},
			{
				label: m.images_repository(),
				getValue: (item: ImageSummaryDto) => (item.repo && item.repo !== '<none>' ? item.repo : m.images_untagged()),
				icon: ImageIcon,
				iconVariant: 'purple' as const,
				show: mobileFieldVisibility.repo ?? true
			},
			{
				label: m.common_tags(),
				getValue: (item: ImageSummaryDto) => {
					if (item.repoTags && item.repoTags.length > 0 && item.repoTags[0] !== '<none>:<none>') {
						return item.repoTags.map((rt) => rt.split(':').pop() || rt).join(', ');
					}
					return item.tag && item.tag !== '<none>' ? item.tag : m.images_untagged();
				},
				icon: ImageIcon,
				iconVariant: 'purple' as const,
				show: mobileFieldVisibility.repoTags ?? true
			}
		]}
		footer={(mobileFieldVisibility.created ?? true)
			? {
					label: m.common_created(),
					getValue: (item) => format(new Date(Number(item.created || 0) * 1000), 'PP p'),
					icon: ClockIcon
				}
			: undefined}
		rowActions={RowActions}
		onclick={(item: ImageSummaryDto) => goto(`/images/${item.id}`)}
	>
		{#if (mobileFieldVisibility.updates ?? true) && item.updateInfo !== undefined}
			<div class="flex flex-wrap gap-x-4 gap-y-3 border-t pt-3">
				<div class="flex min-w-0 flex-1 basis-[180px] flex-col">
					<div class="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
						{m.images_updates()}
					</div>
					<div class="mt-0.5">
						<ImageUpdateItem
							updateInfo={item.updateInfo}
							imageId={item.id}
							repo={item.repo}
							tag={item.tag}
							onUpdated={(newInfo) => handleUpdateInfoChanged(item.id, newInfo)}
						/>
					</div>
				</div>
			</div>
		{/if}
	</UniversalMobileCard>
{/snippet}

{#snippet RowActions({ item }: { item: ImageSummaryDto })}
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
				<DropdownMenu.Item onclick={() => goto(`/images/${item.id}`)}>
					<ScanSearchIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>
				<DropdownMenu.Item
					onclick={() => handleInlineImagePull(item.id, item.repoTags?.[0] || '')}
					disabled={isPullingInline[item.id] || !item.repoTags?.[0]}
				>
					{#if isPullingInline[item.id]}
						<Spinner class="size-4" />
						{m.common_action_pulling()}
					{:else}
						<DownloadIcon class="size-4" />
						{m.images_pull()}
					{/if}
				</DropdownMenu.Item>
				<DropdownMenu.Separator />
				<DropdownMenu.Item variant="destructive" onclick={() => deleteImage(item.id)} disabled={isLoading.removing}>
					{#if isLoading.removing}
						<Spinner class="size-4" />
					{:else}
						<Trash2Icon class="size-4" />
					{/if}
					{m.common_remove()}
				</DropdownMenu.Item>
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
{/snippet}

<ArcaneTable
	persistKey="arcane-image-table"
	items={images}
	bind:requestOptions
	bind:selectedIds
	bind:mobileFieldVisibility
	onRemoveSelected={(ids) => handleDeleteSelected(ids)}
	onRefresh={async (options) => (images = await imageService.getImages(options))}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ImageMobileCardSnippet}
/>

<script lang="ts">
	import ArcaneTable from '$lib/components/arcane-table/arcane-table.svelte';
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { goto } from '$app/navigation';
	import { onDestroy } from 'svelte';
	import { toast } from 'svelte-sonner';
	import bytes from '$lib/utils/bytes';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import StatusBadge from '$lib/components/badges/status-badge.svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import ImageUpdateItem from '$lib/components/image-update-item.svelte';
	import VulnerabilityScanItem from '$lib/components/vulnerability/vulnerability-scan-item.svelte';
	import UniversalMobileCard from '$lib/components/arcane-table/cards/universal-mobile-card.svelte';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import type { Paginated, SearchPaginationSortRequest } from '$lib/types/pagination.type';
	import type { ImageSummaryDto, ImageUpdateInfoDto } from '$lib/types/image.type';
	import type { VulnerabilityScanSummary } from '$lib/types/vulnerability.type';
	import { format } from 'date-fns';
	import type { ColumnSpec, MobileFieldVisibility, BulkAction } from '$lib/components/arcane-table';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { vulnerabilityService } from '$lib/services/vulnerability-service';
	import { isLikelyStaleFailedSummary, isVulnerabilityScanInProgress } from '$lib/utils/vulnerability-scan.util';

	import {
		DownloadIcon,
		TrashIcon,
		InspectIcon,
		ImagesIcon,
		VolumesIcon,
		ClockIcon,
		EllipsisIcon,
		ScanIcon,
		ProjectsIcon,
		ContainersIcon
	} from '$lib/icons';

	let {
		images = $bindable(),
		selectedIds = $bindable(),
		requestOptions = $bindable(),
		onImageUpdated,
		onRefreshData
	}: {
		images: Paginated<ImageSummaryDto>;
		selectedIds: string[];
		requestOptions: SearchPaginationSortRequest;
		onImageUpdated?: () => Promise<void>;
		onRefreshData?: (options: SearchPaginationSortRequest) => Promise<void>;
	} = $props();

	let isLoading = $state({
		removing: false,
		checking: false
	});

	let isPullingInline = $state<Record<string, boolean>>({});
	let isScanningInline = $state<Record<string, boolean>>({});
	let scanRequestedAtByImage = $state<Record<string, string>>({});
	let scanPollTimeout: ReturnType<typeof setTimeout> | null = null;
	let scanPollInFlight = false;
	const SCAN_POLL_INTERVAL_MS = 4000;

	async function refreshImages(options: SearchPaginationSortRequest = requestOptions) {
		if (onRefreshData) {
			await onRefreshData(options);
			return;
		}
		images = await imageService.getImages(options);
	}

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
						await refreshImages();
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
							await refreshImages();
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
				await refreshImages();
			}
		});

		isPullingInline[imageId] = false;
	}

	async function handleInlineVulnerabilityScan(imageId: string) {
		isScanningInline[imageId] = true;

		const result = await tryCatch(vulnerabilityService.scanImage(imageId));
		handleApiResultWithCallbacks({
			result,
			message: m.vuln_scan_failed(),
			setLoadingState: () => {},
			onSuccess: async (data) => {
				scanRequestedAtByImage[imageId] = data.scanTime || new Date().toISOString();
				const summary: VulnerabilityScanSummary = {
					imageId: data.imageId,
					scanTime: data.scanTime,
					status: data.status,
					scanPhase: data.scanPhase,
					summary: data.summary,
					error: data.error
				};
				await handleVulnerabilityScanChanged(imageId, summary);

				if (data.status === 'completed') {
					toast.success(m.vuln_scan_completed());
					return;
				}
				if (data.status === 'failed') {
					toast.error(data.error || m.vuln_scan_failed());
					return;
				}

				startBatchScanPolling();
			}
		});

		isScanningInline[imageId] = false;
	}

	async function handleUpdateInfoChanged(imageId: string, newUpdateInfo: ImageUpdateInfoDto) {
		const imageIndex = images.data.findIndex((img) => img.id === imageId);
		if (imageIndex !== -1) {
			images.data[imageIndex].updateInfo = newUpdateInfo;
			images = { ...images, data: [...images.data] };
		}
		await onImageUpdated?.();
	}

	async function handleVulnerabilityScanChanged(imageId: string, newScanSummary: VulnerabilityScanSummary) {
		const imageIndex = images.data.findIndex((img) => img.id === imageId);
		if (imageIndex !== -1) {
			images.data[imageIndex].vulnerabilityScan = newScanSummary;
			images = { ...images, data: [...images.data] };
		}
		if (newScanSummary.status === 'completed' || newScanSummary.status === 'failed') {
			if (scanRequestedAtByImage[imageId]) {
				delete scanRequestedAtByImage[imageId];
				scanRequestedAtByImage = { ...scanRequestedAtByImage };
			}
		}
		if (newScanSummary.status === 'completed' || newScanSummary.status === 'failed') {
			await onImageUpdated?.();
		} else if (isVulnerabilityScanInProgress(newScanSummary.status)) {
			startBatchScanPolling();
		}
	}

	function stopBatchScanPolling() {
		if (scanPollTimeout) {
			clearTimeout(scanPollTimeout);
			scanPollTimeout = null;
		}
	}

	function getScanningImageIds(): string[] {
		return (images.data ?? [])
			.filter((item) => isVulnerabilityScanInProgress(item.vulnerabilityScan?.status))
			.map((item) => item.id);
	}

	async function pollBatchScanSummaries() {
		const imageIds = getScanningImageIds();
		if (imageIds.length === 0) {
			stopBatchScanPolling();
			return;
		}

		if (scanPollInFlight) {
			scheduleBatchScanPolling();
			return;
		}

		scanPollInFlight = true;
		try {
			const response = await vulnerabilityService.getScanSummaries(imageIds);
			const summaries = response?.summaries ?? {};

			if (Object.keys(summaries).length > 0 && images.data?.length) {
				let changed = false;
				let completed = false;
				const nextScanRequestedAtByImage = { ...scanRequestedAtByImage };
				const nextData = images.data.map((img) => {
					const summary = summaries[img.id];
					if (!summary) return img;

					if (
						summary.status === 'failed' &&
						isLikelyStaleFailedSummary(summary, nextScanRequestedAtByImage[img.id]) &&
						isVulnerabilityScanInProgress(img.vulnerabilityScan?.status)
					) {
						return img;
					}

					changed = true;
					if (summary.status === 'completed' || summary.status === 'failed') {
						completed = true;
						delete nextScanRequestedAtByImage[img.id];
					}
					return { ...img, vulnerabilityScan: summary };
				});
				if (changed) {
					images = { ...images, data: nextData };
					scanRequestedAtByImage = nextScanRequestedAtByImage;
				}
				if (completed) {
					await onImageUpdated?.();
				}
			}
		} catch (error) {
			console.error('Failed to poll vulnerability summaries:', error);
		} finally {
			scanPollInFlight = false;
			if (getScanningImageIds().length > 0) {
				scheduleBatchScanPolling();
			}
		}
	}

	function scheduleBatchScanPolling() {
		stopBatchScanPolling();
		scanPollTimeout = setTimeout(() => void pollBatchScanSummaries(), SCAN_POLL_INTERVAL_MS);
	}

	function startBatchScanPolling() {
		if (scanPollTimeout) return;
		void pollBatchScanSummaries();
	}

	$effect(() => {
		if (getScanningImageIds().length > 0) {
			startBatchScanPolling();
		} else {
			stopBatchScanPolling();
		}
	});

	onDestroy(() => {
		stopBatchScanPolling();
	});

	const columns = [
		{ accessorKey: 'id', title: m.common_id(), hidden: true },
		{ accessorKey: 'repo', title: m.images_repository(), sortable: true, cell: RepoCell },
		{ accessorKey: 'repoTags', title: m.common_tags(), cell: TagCell },
		{
			accessorKey: 'inUse',
			title: m.common_status(),
			sortable: true,
			cell: StatusCell
		},
		{
			id: 'usedBy',
			title: m.images_used_by(),
			cell: UsedByCell
		},
		{
			id: 'updates',
			accessorFn: (row) => {
				if (row.updateInfo?.hasUpdate) return 'has_update';
				if (row.updateInfo?.error) return 'error';
				if (row.updateInfo) return 'up_to_date';
				return 'unknown';
			},
			title: m.images_updates(),
			cell: UpdatesCell,
			align: 'center',
			class: 'text-center'
		},
		{
			id: 'vulnerabilities',
			accessorFn: (row) => {
				if (!row.vulnerabilityScan) return 'not_scanned';
				if (row.vulnerabilityScan.status === 'failed') return 'error';
				if (row.vulnerabilityScan.status === 'completed') {
					const total = row.vulnerabilityScan.summary?.total ?? 0;
					const critical = row.vulnerabilityScan.summary?.critical ?? 0;
					const high = row.vulnerabilityScan.summary?.high ?? 0;
					if (critical > 0) return 'critical';
					if (high > 0) return 'high';
					if (total > 0) return 'has_vulnerabilities';
					return 'clean';
				}
				return 'scanning';
			},
			title: m.vuln_title(),
			cell: VulnerabilitiesCell,
			align: 'center',
			class: 'text-center'
		},
		{ accessorKey: 'size', title: m.common_size(), sortable: true, cell: SizeCell },
		{ accessorKey: 'created', title: m.common_created(), sortable: true, cell: CreatedCell }
	] satisfies ColumnSpec<ImageSummaryDto>[];

	const mobileFields = [
		{ id: 'id', label: m.common_id(), defaultVisible: false },
		{ id: 'repoTags', label: m.common_tags(), defaultVisible: true },
		{ id: 'inUse', label: m.common_status(), defaultVisible: true },
		{ id: 'usedBy', label: m.images_used_by(), defaultVisible: false },
		{ id: 'updates', label: m.images_updates(), defaultVisible: false },
		{ id: 'vulnerabilities', label: m.vuln_title(), defaultVisible: false },
		{ id: 'size', label: m.common_size(), defaultVisible: true },
		{ id: 'created', label: m.common_created(), defaultVisible: true }
	];

	const bulkActions = $derived.by<BulkAction[]>(() => [
		{
			id: 'remove',
			label: m.common_remove_selected_count({ count: selectedIds?.length ?? 0 }),
			action: 'remove',
			onClick: handleDeleteSelected,
			loading: isLoading.removing,
			disabled: isLoading.removing,
			icon: TrashIcon
		}
	]);

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

{#snippet UsedByCell({ item }: { item: ImageSummaryDto })}
	{#if item.usedBy && item.usedBy.length > 0}
		{@const maxVisible = 3}
		{@const hasOverflow = item.usedBy.length > maxVisible}
		{@const visibleUsage = hasOverflow ? item.usedBy.slice(0, maxVisible) : item.usedBy}
		{@const overflowUsage = hasOverflow ? item.usedBy.slice(maxVisible) : []}
		<div class="flex flex-wrap gap-1.5">
			{#each visibleUsage as usage}
				{#if usage.type === 'project'}
					{#if usage.id}
						<a class="inline-flex" href={`/projects/${encodeURIComponent(usage.id)}`}>
							<Badge
								variant="outline"
								class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
							>
								<ProjectsIcon class="size-3" />
								<span>{usage.name}</span>
							</Badge>
						</a>
					{:else}
						<Badge
							variant="outline"
							class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
						>
							<ProjectsIcon class="size-3" />
							<span>{usage.name}</span>
						</Badge>
					{/if}
				{:else if usage.id}
					<a class="inline-flex" href={`/containers/${encodeURIComponent(usage.id)}`}>
						<Badge
							variant="outline"
							class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
						>
							<ContainersIcon class="size-3" />
							<span>{usage.name}</span>
						</Badge>
					</a>
				{:else}
					<Badge
						variant="outline"
						class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
					>
						<ContainersIcon class="size-3" />
						<span>{usage.name}</span>
					</Badge>
				{/if}
			{/each}
			{#if hasOverflow}
				<Tooltip.Provider>
					<Tooltip.Root>
						<Tooltip.Trigger>
							<Badge
								variant="outline"
								class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center rounded-md text-xs transition-colors focus-visible:ring-2"
							>
								+{overflowUsage.length}
							</Badge>
						</Tooltip.Trigger>
						<Tooltip.Content class="pointer-events-auto">
							<div class="flex max-w-xs flex-wrap gap-1.5">
								{#each overflowUsage as usage}
									{#if usage.type === 'project'}
										{#if usage.id}
											<a class="inline-flex" href={`/projects/${encodeURIComponent(usage.id)}`}>
												<Badge
													variant="outline"
													class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
												>
													<ProjectsIcon class="size-3" />
													<span>{usage.name}</span>
												</Badge>
											</a>
										{:else}
											<Badge
												variant="outline"
												class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
											>
												<ProjectsIcon class="size-3" />
												<span>{usage.name}</span>
											</Badge>
										{/if}
									{:else if usage.id}
										<a class="inline-flex" href={`/containers/${encodeURIComponent(usage.id)}`}>
											<Badge
												variant="outline"
												class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
											>
												<ContainersIcon class="size-3" />
												<span>{usage.name}</span>
											</Badge>
										</a>
									{:else}
										<Badge
											variant="outline"
											class="hover:bg-accent/40 focus-visible:ring-primary/40 bg-background/80 inline-flex items-center gap-1 rounded-md text-xs transition-colors focus-visible:ring-2"
										>
											<ContainersIcon class="size-3" />
											<span>{usage.name}</span>
										</Badge>
									{/if}
								{/each}
							</div>
						</Tooltip.Content>
					</Tooltip.Root>
				</Tooltip.Provider>
			{/if}
		</div>
	{:else}
		<span class="text-muted-foreground">—</span>
	{/if}
{/snippet}

{#snippet UpdatesCell({ item }: { item: ImageSummaryDto })}
	<div class="flex items-center justify-center">
		<ImageUpdateItem
			updateInfo={item.updateInfo}
			imageId={item.id}
			repo={item.repo}
			tag={item.tag}
			onUpdated={(newInfo) => handleUpdateInfoChanged(item.id, newInfo)}
		/>
	</div>
{/snippet}

{#snippet VulnerabilitiesCell({ item }: { item: ImageSummaryDto })}
	<div class="flex items-center justify-center">
		<VulnerabilityScanItem
			scanSummary={item.vulnerabilityScan}
			imageId={item.id}
			pollingEnabled={false}
			onScanned={(newSummary) => handleVulnerabilityScanChanged(item.id, newSummary)}
		/>
	</div>
{/snippet}

{#snippet ImageMobileCardSnippet({
	row,
	item,
	mobileFieldVisibility
}: {
	row: any;
	item: ImageSummaryDto;
	mobileFieldVisibility: MobileFieldVisibility;
})}
	<UniversalMobileCard
		{item}
		icon={(item) => ({
			component: ImagesIcon,
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
					: null,
			(item: ImageSummaryDto) => {
				if (!(mobileFieldVisibility.updates ?? false)) return null;
				if (item.updateInfo?.hasUpdate) return { variant: 'blue' as const, text: m.images_has_updates() };
				if (item.updateInfo?.error) return { variant: 'red' as const, text: m.common_error() };
				if (item.updateInfo) return { variant: 'green' as const, text: m.images_no_updates() };
				return { variant: 'gray' as const, text: m.common_unknown() };
			}
		]}
		fields={[
			{
				label: m.common_size(),
				getValue: (item: ImageSummaryDto) => bytes.format(Number(item.size ?? 0)),
				icon: VolumesIcon,
				iconVariant: 'blue' as const,
				show: mobileFieldVisibility.size ?? true
			},
			{
				label: m.common_tags(),
				getValue: (item: ImageSummaryDto) => {
					if (item.repoTags && item.repoTags.length > 0 && item.repoTags[0] !== '<none>:<none>') {
						return item.repoTags.map((rt) => rt.split(':').pop() || rt).join(', ');
					}
					return item.tag && item.tag !== '<none>' ? item.tag : m.images_untagged();
				},
				icon: ImagesIcon,
				iconVariant: 'purple' as const,
				show: mobileFieldVisibility.repoTags ?? true
			},
			{
				label: m.images_used_by(),
				getValue: (item: ImageSummaryDto) => item.usedBy?.map((usage) => usage.name).join(', ') || '—',
				icon: ImagesIcon,
				iconVariant: 'purple' as const,
				show: mobileFieldVisibility.usedBy ?? false
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
	/>
{/snippet}

{#snippet RowActions({ item }: { item: ImageSummaryDto })}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton {...props} action="base" tone="ghost" size="icon" class="size-8">
					<span class="sr-only">{m.common_open_menu()}</span>
					<EllipsisIcon class="size-4" />
				</ArcaneButton>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Group>
				<DropdownMenu.Item onclick={() => goto(`/images/${item.id}`)}>
					<InspectIcon class="size-4" />
					{m.common_inspect()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				<DropdownMenu.Item
					onclick={() => handleInlineImagePull(item.id, item.repoTags?.[0] || '')}
					disabled={isPullingInline[item.id] || !item.repoTags?.[0]}
				>
					{#if isPullingInline[item.id]}
						<Spinner class="size-4" />
					{:else}
						<DownloadIcon class="size-4" />
					{/if}
					{m.images_pull()}
				</DropdownMenu.Item>

				<DropdownMenu.Item onclick={() => handleInlineVulnerabilityScan(item.id)} disabled={isScanningInline[item.id]}>
					{#if isScanningInline[item.id]}
						<Spinner class="size-4" />
					{:else}
						<ScanIcon class="size-4" />
					{/if}
					{m.vuln_scan()}
				</DropdownMenu.Item>

				<DropdownMenu.Separator />

				<DropdownMenu.Item variant="destructive" onclick={() => deleteImage(item.id)} disabled={isLoading.removing}>
					{#if isLoading.removing}
						<Spinner class="size-4" />
					{:else}
						<TrashIcon class="size-4" />
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
	{bulkActions}
	onRefresh={async (options) => {
		requestOptions = options;
		await refreshImages(options);
		return images;
	}}
	{columns}
	{mobileFields}
	rowActions={RowActions}
	mobileCard={ImageMobileCardSnippet}
/>

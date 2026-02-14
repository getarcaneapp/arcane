<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { toast } from 'svelte-sonner';
	import ImagePullSheet from '$lib/components/sheets/image-pull-sheet.svelte';
	import bytes from '$lib/utils/bytes';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { displaySize, FileDropZone, MEGABYTE, type FileDropZoneProps } from '$lib/components/ui/file-drop-zone';
	import ImageTable from './image-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { queryKeys } from '$lib/query/query-keys';
	import type { ImageUsageCounts } from '$lib/types/image.type';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import { CloseIcon, VolumesIcon, LocalFolderComputerIcon } from '$lib/icons';

	let { data } = $props();

	let images = $state(untrack(() => data.images));
	let requestOptions = $state(untrack(() => data.imageRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isPullDialogOpen = $state(false);
	let isUploadDialogOpen = $state(false);
	let isConfirmPruneDialogOpen = $state(false);
	let uploadedFiles = $state<File[]>([]);
	const envId = $derived(environmentStore.selected?.id || '0');
	const imageUsageFallback: ImageUsageCounts = {
		imagesInuse: 0,
		imagesUnused: 0,
		totalImages: 0,
		totalImageSize: 0
	};

	const maxUploadSizeMB = $derived(parseInt(String(data.settings?.maxImageUploadSize || '500'), 10));
	const shouldPruneDangling = $derived(data.settings?.dockerPruneMode === 'dangling');

	const imagesQuery = createQuery(() => ({
		queryKey: queryKeys.images.list(envId, requestOptions),
		queryFn: () => imageService.getImagesForEnvironment(envId, requestOptions),
		initialData: data.images
	}));

	const imageUsageCountsQuery = createQuery(() => ({
		queryKey: queryKeys.images.usageCounts(envId),
		queryFn: () => imageService.getImageUsageCountsForEnvironment(envId),
		initialData: data.imageUsageCounts
	}));

	const pruneImagesMutation = createMutation(() => ({
		mutationKey: ['images', 'prune', envId],
		mutationFn: () => imageService.pruneImages(shouldPruneDangling),
		onSuccess: async () => {
			toast.success(m.images_pruned_success());
			await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
			isConfirmPruneDialogOpen = false;
		},
		onError: () => {
			toast.error(m.images_prune_failed());
		}
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['images', 'check-updates', envId],
		mutationFn: () => imageService.checkAllImages(),
		onSuccess: async () => {
			toast.success(m.images_update_check_completed());
			await imagesQuery.refetch();
		},
		onError: () => {
			toast.error(m.images_update_check_failed());
		}
	}));

	const uploadImagesMutation = createMutation(() => ({
		mutationKey: ['images', 'upload', envId],
		mutationFn: async (files: File[]) => {
			for (const file of files) {
				try {
					await imageService.uploadImage(file);
					toast.success(m.images_upload_success());
				} catch {
					toast.error(m.images_upload_failed());
				}
			}
		},
		onSuccess: async () => {
			await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
			uploadedFiles = [];
			isUploadDialogOpen = false;
		}
	}));

	$effect(() => {
		if (imagesQuery.data) {
			images = imagesQuery.data;
		}
	});

	const imageUsageCounts = $derived(imageUsageCountsQuery.data ?? imageUsageFallback);

	const isRefreshing = $derived(
		(imagesQuery.isFetching && !imagesQuery.isPending) || (imageUsageCountsQuery.isFetching && !imageUsageCountsQuery.isPending)
	);
	const isUploading = $derived(uploadImagesMutation.isPending);
	const isPruning = $derived(pruneImagesMutation.isPending);
	const isChecking = $derived(checkUpdatesMutation.isPending);

	const onUpload: FileDropZoneProps['onUpload'] = async (files) => {
		uploadedFiles = [...uploadedFiles, ...files];
	};

	const onFileRejected: FileDropZoneProps['onFileRejected'] = async ({ reason, file }) => {
		toast.error(`${file.name} failed to upload!`, { description: reason });
	};

	async function handleUploadImages() {
		if (uploadedFiles.length === 0) {
			toast.error(m.images_upload_file_required());
			return;
		}
		await uploadImagesMutation.mutateAsync(uploadedFiles);
	}

	async function handleTriggerBulkUpdateCheck() {
		await checkUpdatesMutation.mutateAsync();
	}

	async function handlePruneImages() {
		await pruneImagesMutation.mutateAsync();
	}

	async function refresh() {
		await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
	}

	const actionButtons: ActionButton[] = $derived([
		{ id: 'pull', action: 'pull', label: m.images_pull_image(), onclick: () => (isPullDialogOpen = true) },
		{ id: 'upload', action: 'create', label: m.images_upload_image(), onclick: () => (isUploadDialogOpen = true) },
		{
			id: 'check-updates',
			action: 'inspect',
			label: m.images_check_updates(),
			loadingLabel: m.common_action_checking(),
			onclick: handleTriggerBulkUpdateCheck,
			loading: isChecking,
			disabled: isChecking
		},
		{
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		},
		{
			id: 'prune',
			action: 'remove',
			label: m.images_prune_unused(),
			loadingLabel: m.common_action_pruning(),
			onclick: () => (isConfirmPruneDialogOpen = true),
			loading: isPruning,
			disabled: isPruning
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{
			title: m.images_total(),
			value: imageUsageCounts.totalImages,
			icon: VolumesIcon,
			iconColor: 'text-blue-500'
		},
		{
			title: m.images_total_size(),
			value: String(bytes.format(imageUsageCounts.totalImageSize)),
			icon: LocalFolderComputerIcon,
			iconColor: 'text-amber-500'
		}
	]);
</script>

<ResourcePageLayout title={m.images_title()} subtitle={m.images_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		<ImageTable
			bind:images
			bind:selectedIds
			bind:requestOptions
			onRefreshData={async (options) => {
				requestOptions = options;
				await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
			}}
			onImageUpdated={async () => {
				await imagesQuery.refetch();
			}}
		/>
	{/snippet}

	{#snippet additionalContent()}
		<ImagePullSheet
			bind:open={isPullDialogOpen}
			onPullFinished={async () => {
				await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
			}}
		/>

		<Dialog.Root bind:open={isUploadDialogOpen}>
			<Dialog.Content class="max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{m.images_upload_image()}</Dialog.Title>
					<Dialog.Description>{m.images_upload_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-4 py-4">
					<FileDropZone
						{onUpload}
						{onFileRejected}
						maxFileSize={maxUploadSizeMB * MEGABYTE}
						accept=".tar,.tar.gz,.tgz,.tar.xz"
						maxFiles={10}
						fileCount={uploadedFiles.length}
						disabled={isUploading}
					/>
					{#if uploadedFiles.length > 0}
						<div class="flex flex-col gap-2">
							{#each uploadedFiles as file, i (file.name)}
								<div class="border-border bg-muted/50 flex items-center justify-between gap-2 rounded-lg border p-3">
									<div class="flex flex-col">
										<span class="text-sm font-medium">{file.name}</span>
										<span class="text-muted-foreground text-xs">{displaySize(file.size)}</span>
									</div>
									<ArcaneButton
										action="base"
										tone="ghost"
										size="icon"
										onclick={() => (uploadedFiles = [...uploadedFiles.slice(0, i), ...uploadedFiles.slice(i + 1)])}
										disabled={isUploading}
										icon={CloseIcon}
									/>
								</div>
							{/each}
						</div>
					{/if}
					{#if isUploading}
						<div class="text-muted-foreground flex items-center gap-2 text-sm">
							<Spinner class="size-4" />{m.images_uploading()}
						</div>
					{/if}
				</div>
				<div class="flex justify-end gap-3">
					<ArcaneButton
						action="cancel"
						onclick={() => {
							isUploadDialogOpen = false;
							uploadedFiles = [];
						}}
						disabled={isUploading}
					/>
					<ArcaneButton
						action="create"
						onclick={handleUploadImages}
						disabled={isUploading || uploadedFiles.length === 0}
						loading={isUploading}
						customLabel={m.images_upload_image()}
					/>
				</div>
			</Dialog.Content>
		</Dialog.Root>

		<Dialog.Root bind:open={isConfirmPruneDialogOpen}>
			<Dialog.Content>
				<Dialog.Header>
					<Dialog.Title>{m.images_prune_confirm_title()}</Dialog.Title>
					<Dialog.Description
						>{m.images_prune_confirm_description({ mode: String(data.settings.dockerPruneMode) })}</Dialog.Description
					>
				</Dialog.Header>
				<div class="flex justify-end gap-3 pt-6">
					<ArcaneButton action="cancel" onclick={() => (isConfirmPruneDialogOpen = false)} disabled={isPruning} />
					<ArcaneButton
						action="remove"
						onclick={handlePruneImages}
						disabled={isPruning}
						loading={isPruning}
						customLabel={m.images_prune_action()}
						loadingLabel={m.common_action_pruning()}
					/>
				</div>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</ResourcePageLayout>

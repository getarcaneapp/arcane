<script lang="ts">
	import { ArcaneButton } from '$lib/components/arcane-button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { toast } from 'svelte-sonner';
	import ImagePullSheet from '$lib/components/sheets/image-pull-sheet.svelte';
	import ImageRegistrySearchDialog from '$lib/components/dialogs/image-registry-search-dialog.svelte';
	import { bytes } from '$lib/utils/formatting';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { displaySize, FileDropZone, MEGABYTE, type FileDropZoneProps } from '$lib/components/ui/file-drop-zone';
	import ImageTable from './image-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import { hasPermission } from '$lib/utils/auth';
	import { queryKeys } from '$lib/query/query-keys';
	import type { ImageUsageCounts } from '$lib/types/docker';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { CloseIcon, VolumesIcon, LocalFolderComputerIcon, SearchIcon } from '$lib/icons';
	import { createMutation, createQuery } from '@tanstack/svelte-query';
	import PruneModeCard from '$lib/components/prune/prune-mode-card.svelte';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';

	let { data } = $props();

	let images = $state(untrack(() => data.images));
	let requestOptions = $state(untrack(() => data.imageRequestOptions));
	let selectedIds = $state<string[]>([]);
	let isPullDialogOpen = $state(false);
	let isRegistrySearchDialogOpen = $state(false);
	let isUploadDialogOpen = $state(false);
	let isConfirmPruneDialogOpen = $state(false);
	let isRefreshing = $state(false);
	let uploadedFiles = $state<File[]>([]);
	let imagePruneMode = $state<'dangling' | 'all' | 'olderThan'>('dangling');
	let imagePruneUntil = $state('');
	const envId = $derived(environmentStore.selected?.id || '0');
	let previousEnvId = untrack(() => envId);
	const imageUsageFallback: ImageUsageCounts = {
		imagesInuse: 0,
		imagesUnused: 0,
		totalImages: 0,
		totalImageSize: 0
	};

	const maxUploadSizeMB = $derived(
		parseInt(String((data.envId === envId ? data.settings?.maxImageUploadSize : undefined) || '500'), 10)
	);

	const imagesQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.images.list(queryEnvId, requestOptions),
			queryFn: () => imageService.getImagesForEnvironment(queryEnvId, requestOptions),
			initialData: data.envId === queryEnvId ? data.images : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});

	const imageUsageCountsQuery = createQuery(() => {
		const queryEnvId = envId;
		return {
			queryKey: queryKeys.images.usageCounts(queryEnvId),
			queryFn: () => imageService.getImageUsageCountsForEnvironment(queryEnvId),
			initialData: data.envId === queryEnvId ? data.imageUsageCounts : undefined,
			select: (value) => ({ envId: queryEnvId, value })
		};
	});
	const resourcesReady = $derived(imagesQuery.data?.envId === envId);

	const pruneImagesMutation = createMutation(() => ({
		mutationKey: ['images', 'prune', envId],
		mutationFn: (requestedEnvId: string) =>
			imageService.pruneImages(
				{
					mode: imagePruneMode,
					...(imagePruneMode === 'olderThan' ? { until: imagePruneUntil } : {})
				},
				requestedEnvId
			),
		onSuccess: async (data, requestedEnvId) => {
			toast.success(m.images_pruned_success(), activityToastOptions(extractActivityId(data)));
			if (requestedEnvId === envId) {
				await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
				isConfirmPruneDialogOpen = false;
			}
		},
		onError: () => {
			toast.error(m.images_prune_failed());
		}
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['images', 'check-updates', envId],
		mutationFn: (requestedEnvId: string) => imageService.checkAllImages(requestedEnvId),
		onSuccess: async (data, requestedEnvId) => {
			toast.success(m.images_update_check_completed(), activityToastOptions(extractActivityId(data)));
			if (requestedEnvId === envId) {
				await imagesQuery.refetch();
			}
		},
		onError: () => {
			toast.error(m.images_update_check_failed());
		}
	}));

	const uploadImagesMutation = createMutation(() => ({
		mutationKey: ['images', 'upload', envId],
		mutationFn: async ({ files, requestedEnvId }: { files: File[]; requestedEnvId: string }) => {
			for (const file of files) {
				try {
					await imageService.uploadImage(file, requestedEnvId);
					toast.success(m.images_upload_success());
				} catch {
					toast.error(m.images_upload_failed());
				}
			}
		},
		onSuccess: async (_data, { requestedEnvId }) => {
			if (requestedEnvId === envId) {
				await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
				uploadedFiles = [];
				isUploadDialogOpen = false;
			}
		}
	}));

	$effect(() => {
		if (imagesQuery.data?.envId === envId) {
			images = imagesQuery.data.value;
		}
	});

	$effect(() => {
		if (envId === previousEnvId) return;
		previousEnvId = envId;
		selectedIds = [];
		isPullDialogOpen = false;
		isRegistrySearchDialogOpen = false;
		isUploadDialogOpen = false;
		isConfirmPruneDialogOpen = false;
		uploadedFiles = [];
		isRefreshing = false;
	});

	const imageUsageCounts = $derived(
		imageUsageCountsQuery.data?.envId === envId ? imageUsageCountsQuery.data.value : imageUsageFallback
	);

	// Intentionally a manual flag, not $derived(query.isFetching): these queries use
	// aggressive background refetching (staleTime: 0 + refetchOnMount/WindowFocus:
	// 'always'), so deriving from isFetching leaves the manual refresh button spinning
	// constantly. Mirrors the containers page — reflects only user-initiated refreshes.
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
		await uploadImagesMutation.mutateAsync({ files: uploadedFiles, requestedEnvId: envId });
	}

	async function handleTriggerBulkUpdateCheck() {
		await checkUpdatesMutation.mutateAsync(envId);
	}

	async function handlePruneImages() {
		await pruneImagesMutation.mutateAsync(envId);
	}

	const imagePruneModes = [
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.prune_images_mode_all() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];

	async function refresh() {
		const requestedEnvId = envId;
		isRefreshing = true;
		try {
			await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
		} finally {
			if (requestedEnvId === envId) {
				isRefreshing = false;
			}
		}
	}

	const canPullImage = $derived(hasPermission('images:pull', envId));
	const canUploadImage = $derived(hasPermission('images:upload', envId));
	const canPruneImage = $derived(hasPermission('images:prune', envId));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canPullImage) {
			buttons.push({
				id: 'pull',
				action: 'pull',
				label: m.images_pull_image(),
				onclick: () => (isPullDialogOpen = true),
				disabled: !resourcesReady
			});
			buttons.push({
				id: 'registry-search',
				action: 'inspect',
				label: m.images_search_registry(),
				icon: SearchIcon,
				onclick: () => (isRegistrySearchDialogOpen = true),
				disabled: !resourcesReady
			});
		}
		if (canUploadImage) {
			buttons.push({
				id: 'upload',
				action: 'create',
				label: m.images_upload_image(),
				onclick: () => (isUploadDialogOpen = true),
				disabled: !resourcesReady
			});
		}
		if (canPullImage) {
			buttons.push({
				id: 'check-updates',
				action: 'inspect',
				label: m.images_check_updates(),
				loadingLabel: m.common_action_checking(),
				onclick: handleTriggerBulkUpdateCheck,
				loading: isChecking,
				disabled: !resourcesReady || isChecking
			});
		}
		buttons.push({
			id: 'refresh',
			action: 'restart',
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		});
		if (canPruneImage) {
			buttons.push({
				id: 'prune',
				action: 'remove',
				label: m.images_prune_unused(),
				loadingLabel: m.common_action_pruning(),
				onclick: () => (isConfirmPruneDialogOpen = true),
				loading: isPruning,
				disabled: !resourcesReady || isPruning
			});
		}
		return buttons;
	});

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
		{#if resourcesReady}
			<ImageTable
				bind:images
				bind:selectedIds
				bind:requestOptions
				loading={imagesQuery.isLoading}
				onRefreshData={async (options) => {
					const requestedEnvId = envId;
					requestOptions = options;
					await imageUsageCountsQuery.refetch();
					if (requestedEnvId !== envId) {
						selectedIds = [];
					}
				}}
				onImageUpdated={async () => {
					await imagesQuery.refetch();
				}}
			/>
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<ImagePullSheet
			bind:open={isPullDialogOpen}
			onPullFinished={async () => {
				await Promise.all([imagesQuery.refetch(), imageUsageCountsQuery.refetch()]);
			}}
		/>

		<ImageRegistrySearchDialog
			bind:open={isRegistrySearchDialogOpen}
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
					<Dialog.Description>{m.images_prune_confirm_description({ mode: imagePruneMode })}</Dialog.Description>
				</Dialog.Header>
				<div class="py-4">
					<PruneModeCard
						title={m.prune_images_label()}
						description={m.prune_images_dialog_description()}
						modeOptions={imagePruneModes}
						bind:value={imagePruneMode}
						bind:untilValue={imagePruneUntil}
						disabled={isPruning}
					/>
				</div>
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

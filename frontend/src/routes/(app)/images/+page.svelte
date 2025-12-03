<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import HardDriveIcon from '@lucide/svelte/icons/hard-drive';
	import PackageIcon from '@lucide/svelte/icons/package';
	import XIcon from '@lucide/svelte/icons/x';
	import { toast } from 'svelte-sonner';
	import ImagePullSheet from '$lib/components/sheets/image-pull-sheet.svelte';
	import bytes from 'bytes';
	import * as Dialog from '$lib/components/ui/dialog/index.js';
	import { displaySize, FileDropZone, MEGABYTE, type FileDropZoneProps } from '$lib/components/ui/file-drop-zone';
	import { handleApiResultWithCallbacks } from '$lib/utils/api.util';
	import { tryCatch } from '$lib/utils/try-catch';
	import ImageTable from './image-table.svelte';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { untrack } from 'svelte';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '$lib/layouts/index.js';
	import { useEnvironmentRefresh } from '$lib/hooks/use-environment-refresh.svelte';
	import { parallelRefresh } from '$lib/utils/refresh.util';

	let { data } = $props();

	let images = $state(untrack(() => data.images));
	let imageUsageCounts = $state(untrack(() => data.imageUsageCounts));
	let requestOptions = $state(untrack(() => data.imageRequestOptions));
	let selectedIds = $state<string[]>([]);

	let isLoading = $state({ pulling: false, uploading: false, refreshing: false, pruning: false, checking: false });
	let isPullDialogOpen = $state(false);
	let isUploadDialogOpen = $state(false);
	let isConfirmPruneDialogOpen = $state(false);
	let uploadedFiles = $state<File[]>([]);

	const maxUploadSizeMB = $derived(parseInt(String(data.settings?.maxImageUploadSize || '500'), 10));

	async function refresh() {
		await parallelRefresh(
			{
				images: {
					fetch: () => imageService.getImages(requestOptions),
					onSuccess: (data) => (images = data),
					errorMessage: m.common_refresh_failed({ resource: m.images_title() })
				},
				counts: {
					fetch: () => imageService.getImageUsageCounts(),
					onSuccess: (data) => (imageUsageCounts = data),
					errorMessage: m.common_refresh_failed({ resource: m.images_title() })
				}
			},
			(v) => (isLoading.refreshing = v)
		);
	}

	useEnvironmentRefresh(refresh);

	async function handlePruneImages() {
		isLoading.pruning = true;
		const dangling = data.settings?.dockerPruneMode === 'dangling';
		handleApiResultWithCallbacks({
			result: await tryCatch(imageService.pruneImages(dangling)),
			message: m.images_prune_failed(),
			setLoadingState: (v) => (isLoading.pruning = v),
			onSuccess: async () => {
				toast.success(m.images_pruned_success());
				images = await imageService.getImages(requestOptions);
				isConfirmPruneDialogOpen = false;
			}
		});
	}

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
		isLoading.uploading = true;
		for (const file of uploadedFiles) {
			handleApiResultWithCallbacks({
				result: await tryCatch(imageService.uploadImage(file)),
				message: m.images_upload_failed(),
				setLoadingState: (v) => { isLoading.uploading = v; },
				onSuccess: () => { toast.success(m.images_upload_success()); }
			});
		}
		images = await imageService.getImages(requestOptions);
		uploadedFiles = [];
		isUploadDialogOpen = false;
		isLoading.uploading = false;
	}

	async function handleTriggerBulkUpdateCheck() {
		isLoading.checking = true;
		try {
			await imageService.checkAllImages();
			toast.success(m.images_update_check_completed());
			images = await imageService.getImages(requestOptions);
		} catch {
			toast.error(m.images_update_check_failed());
		} finally {
			isLoading.checking = false;
		}
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
			loading: isLoading.checking,
			disabled: isLoading.checking
		},
		{ id: 'refresh', action: 'restart', label: m.common_refresh(), onclick: refresh, loading: isLoading.refreshing, disabled: isLoading.refreshing },
		{
			id: 'prune',
			action: 'remove',
			label: m.images_prune_unused(),
			loadingLabel: m.common_action_pruning(),
			onclick: () => (isConfirmPruneDialogOpen = true),
			loading: isLoading.pruning,
			disabled: isLoading.pruning
		}
	]);

	const statCards: StatCardConfig[] = $derived([
		{ title: m.images_total(), value: imageUsageCounts.totalImages, icon: HardDriveIcon, iconColor: 'text-blue-500', class: 'border-l-4 border-l-blue-500' },
		{ title: m.images_total_size(), value: String(bytes.format(imageUsageCounts.totalImageSize)), icon: PackageIcon, iconColor: 'text-amber-500', class: 'border-l-4 border-l-amber-500' }
	]);
</script>

<ResourcePageLayout title={m.images_title()} subtitle={m.images_subtitle()} {actionButtons} {statCards} statCardsColumns={2}>
	{#snippet mainContent()}
		<ImageTable bind:images bind:selectedIds bind:requestOptions onImageUpdated={async () => { images = await imageService.getImages(requestOptions); }} />
	{/snippet}

	{#snippet additionalContent()}
		<ImagePullSheet bind:open={isPullDialogOpen} onPullFinished={async () => { images = await imageService.getImages(requestOptions); }} />

		<Dialog.Root bind:open={isUploadDialogOpen}>
			<Dialog.Content class="max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{m.images_upload_image()}</Dialog.Title>
					<Dialog.Description>{m.images_upload_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-4 py-4">
					<FileDropZone {onUpload} {onFileRejected} maxFileSize={maxUploadSizeMB * MEGABYTE} accept=".tar,.tar.gz,.tgz,.tar.xz" maxFiles={10} fileCount={uploadedFiles.length} disabled={isLoading.uploading} />
					{#if uploadedFiles.length > 0}
						<div class="flex flex-col gap-2">
							{#each uploadedFiles as file, i (file.name)}
								<div class="border-border bg-muted/50 flex items-center justify-between gap-2 rounded-lg border p-3">
									<div class="flex flex-col">
										<span class="text-sm font-medium">{file.name}</span>
										<span class="text-muted-foreground text-xs">{displaySize(file.size)}</span>
									</div>
									<Button variant="ghost" size="icon" onclick={() => (uploadedFiles = [...uploadedFiles.slice(0, i), ...uploadedFiles.slice(i + 1)])} disabled={isLoading.uploading}>
										<XIcon class="size-4" />
									</Button>
								</div>
							{/each}
						</div>
					{/if}
					{#if isLoading.uploading}
						<div class="text-muted-foreground flex items-center gap-2 text-sm"><Spinner class="size-4" />{m.images_uploading()}</div>
					{/if}
				</div>
				<div class="flex justify-end gap-3">
					<Button variant="outline" onclick={() => { isUploadDialogOpen = false; uploadedFiles = []; }} disabled={isLoading.uploading}>{m.common_cancel()}</Button>
					<Button onclick={handleUploadImages} disabled={isLoading.uploading || uploadedFiles.length === 0}>
						{#if isLoading.uploading}<Spinner class="mr-2 size-4" />{/if}
						{m.images_upload_image()}
					</Button>
				</div>
			</Dialog.Content>
		</Dialog.Root>

		<Dialog.Root bind:open={isConfirmPruneDialogOpen}>
			<Dialog.Content>
				<Dialog.Header>
					<Dialog.Title>{m.images_prune_confirm_title()}</Dialog.Title>
					<Dialog.Description>{m.images_prune_confirm_description({ mode: String(data.settings.dockerPruneMode) })}</Dialog.Description>
				</Dialog.Header>
				<div class="flex justify-end gap-3 pt-6">
					<Button variant="outline" onclick={() => (isConfirmPruneDialogOpen = false)} disabled={isLoading.pruning}>{m.common_cancel()}</Button>
					<Button variant="destructive" onclick={handlePruneImages} disabled={isLoading.pruning}>
						{#if isLoading.pruning}<Spinner class="mr-2 size-4" /> {m.common_action_pruning()}{:else}{m.images_prune_action()}{/if}
					</Button>
				</div>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</ResourcePageLayout>

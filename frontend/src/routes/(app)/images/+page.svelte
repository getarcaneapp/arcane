<script lang="ts">
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import ImageRegistrySearchDialog from '#lib/components/dialogs/image-registry-search-dialog.svelte';
	import EnvironmentDestinationSelect from '#lib/components/form/environment-destination-select.svelte';
	import PruneModePicker from '#lib/components/prune/prune-mode-picker.svelte';
	import SettingsRow from '#lib/components/settings/settings-row.svelte';
	import SettingsSection from '#lib/components/settings/settings-section.svelte';
	import ImagePullSheet from '#lib/components/sheets/image-pull-sheet.svelte';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import { displaySize, FileDropZone, MEGABYTE, type FileDropZoneProps } from '#lib/components/ui/file-drop-zone/index.js';
	import { Progress } from '#lib/components/ui/progress/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { CloseIcon, VolumesIcon, LocalFolderComputerIcon, SearchIcon } from '#lib/icons/index.js';
	import { ResourcePageLayout, type ActionButton, type StatCardConfig } from '#lib/layouts/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { imageListQuery, imageUsageCountsQuery } from '#lib/query/images.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import type { ChunkedUploadProgress } from '#lib/services/upload-service.js';
	import { environmentStore, type EnvironmentRef } from '#lib/stores/environment.store.svelte.js';
	import type { ImageUsageCounts } from '#lib/types/docker.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { pinViewAction } from '#lib/utils/dashboard-layout.js';
	import {
		acrossEnvironmentsNote,
		actionEnvironments,
		destinationEnvironmentId,
		hasShownPermission,
		listScopes,
		readableEnvironments,
		settleAcrossEnvironments,
		withFailedEnvironments
	} from '#lib/utils/environments.js';
	import { bytes } from '#lib/utils/formatting.js';
	import { ResourceListPageState } from '#lib/utils/resource-list-page.svelte.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	import ImageTable from './components/image-table.svelte';

	let { data } = $props();
	const queryClient = useQueryClient();

	const pageState = new ResourceListPageState(untrack(() => data.imageRequestOptions));
	let isPullDialogOpen = $state(false);
	let isRegistrySearchDialogOpen = $state(false);
	let isUploadDialogOpen = $state(false);
	let isConfirmPruneDialogOpen = $state(false);
	let uploadedFiles = $state<File[]>([]);
	let uploadProgress = $state<ChunkedUploadProgress | null>(null);
	let imagePruneMode = $state<'dangling' | 'all' | 'olderThan'>('dangling');
	let imagePruneUntil = $state('');
	const imageUsageFallback: ImageUsageCounts = {
		imagesInuse: 0,
		imagesUnused: 0,
		totalImages: 0,
		totalImageSize: 0
	};

	const imagesQuery = createQuery(() => {
		const scope = pageState.scope;
		return {
			...imageListQuery(scope, readableEnvironments('images:list'), pageState.requestOptions),
			placeholderData: (previous, query) => (query?.queryKey[1] === scope ? previous : undefined),
			select: (value) => ({ envId: scope, value })
		};
	});

	const usageCountsQuery = createQuery(() => {
		const scope = pageState.scope;
		return {
			...imageUsageCountsQuery(scope, readableEnvironments('images:list')),
			select: (value) => ({ envId: scope, value })
		};
	});
	const images = $derived(imagesQuery.data?.value ?? data.images);
	const countFailures = $derived(
		usageCountsQuery.data?.envId === pageState.scope ? usageCountsQuery.data.value.failedEnvironments : []
	);

	const resourcesReady = $derived(imagesQuery.data?.envId === pageState.scope);

	function invalidateEnvironmentImages(results: { environment: EnvironmentRef }[]) {
		return Promise.all(
			listScopes(results.map(({ environment }) => environment.id)).map((listScope) =>
				queryClient.invalidateQueries({ queryKey: queryKeys.images.listPrefix(listScope) })
			)
		);
	}

	const pruneImagesMutation = createMutation(() => ({
		mutationKey: ['images', 'prune', pageState.scope],
		mutationFn: (_requestedScope: string) =>
			settleAcrossEnvironments(actionEnvironments('images:prune'), (id) =>
				imageService.pruneImages(
					{
						mode: imagePruneMode,
						...(imagePruneMode === 'olderThan' ? { until: imagePruneUntil } : {})
					},
					id
				)
			),
		onSuccess: async (results, requestedScope) => {
			toast.success(m.images_pruned_success(), activityToastOptions(extractActivityId(results[0]?.value)));
			await invalidateEnvironmentImages(results);
			if (requestedScope === pageState.scope) isConfirmPruneDialogOpen = false;
		},
		onError: (error) => {
			toast.error(m.images_prune_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	const checkUpdatesMutation = createMutation(() => ({
		mutationKey: ['images', 'check-updates', pageState.scope],
		mutationFn: () =>
			settleAcrossEnvironments(actionEnvironments('image-updates:check'), (id) => imageService.checkAllImages(id)),
		onSuccess: async (results) => {
			toast.success(m.images_update_check_completed(), activityToastOptions(extractActivityId(results[0]?.value)));
			await invalidateEnvironmentImages(results);
		},
		onError: (error) => {
			toast.error(m.images_update_check_failed(), { description: extractApiErrorMessage(error) });
		}
	}));

	let pickedUploadEnvironmentId = $state('');
	const uploadEnvironmentId = $derived(destinationEnvironmentId(pickedUploadEnvironmentId, 'images:upload'));
	// Merged lists carry one environment's upload limit, so the destination's own is read while uploading.
	// If it cannot be read, the browser sets no limit and the destination enforces its own.
	const uploadLimitQuery = createQuery(() => ({
		queryKey: queryKeys.images.uploadLimit(uploadEnvironmentId),
		queryFn: async () =>
			(await imageService.getImagesForEnvironment(uploadEnvironmentId, { pagination: { page: 1, limit: 1 } })).maxImageUploadSize,
		enabled: isUploadDialogOpen,
		retry: false
	}));
	const maxUploadSizeMB = $derived(uploadLimitQuery.data);

	const uploadImagesMutation = createMutation(() => ({
		mutationKey: ['images', 'upload', uploadEnvironmentId],
		mutationFn: async ({ files, requestedEnvId }: { files: File[]; requestedEnvId: string }) => {
			for (const file of files) {
				try {
					const operationResult = await tryCatch(
						(async () => {
							await imageService.uploadImage(file, requestedEnvId, (progress) => (uploadProgress = progress));
							toast.success(m.images_upload_success());
						})()
					);
					if (operationResult.error !== null) {
						toast.error(m.images_upload_failed());
					}
				} finally {
					uploadProgress = null;
				}
			}
		},
		onSuccess: async (_data, { requestedEnvId }) => {
			if (environmentStore.shown.some((env) => env.id === requestedEnvId)) {
				await queryClient.invalidateQueries({ queryKey: queryKeys.images.listPrefix(pageState.scope) });
				uploadedFiles = [];
				pickedUploadEnvironmentId = '';
				isUploadDialogOpen = false;
			}
		}
	}));

	const imageUsageCounts = $derived(
		usageCountsQuery.data?.envId === pageState.scope ? usageCountsQuery.data.value : imageUsageFallback
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
		await uploadImagesMutation.mutateAsync({ files: uploadedFiles, requestedEnvId: uploadEnvironmentId });
	}

	async function handleTriggerBulkUpdateCheck() {
		await checkUpdatesMutation.mutateAsync();
	}

	async function handlePruneImages() {
		await pruneImagesMutation.mutateAsync(pageState.scope);
	}

	const imagePruneModes = [
		{ value: 'dangling', label: m.prune_images_mode_dangling() },
		{ value: 'all', label: m.all_unused() },
		{ value: 'olderThan', label: m.prune_mode_older_than() }
	];

	function refresh() {
		return Promise.all([imagesQuery.refetch(), usageCountsQuery.refetch()]);
	}

	const isRefreshing = $derived((imagesQuery.isFetching && !imagesQuery.isPending) || usageCountsQuery.isFetching);

	const canPullImage = $derived(hasShownPermission('images:pull'));
	const canUploadImage = $derived(hasShownPermission('images:upload'));
	const canPruneImage = $derived(hasShownPermission('images:prune'));

	const actionButtons: ActionButton[] = $derived.by(() => {
		const buttons: ActionButton[] = [];
		if (canPullImage) {
			buttons.push({
				id: 'pull',
				action: 'pull',
				placement: 'primary',
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
		buttons.push(pinViewAction('images', pageState.requestOptions, readableEnvironments('images:list')));
		buttons.push({
			id: 'refresh',
			action: 'refresh',
			placement: 'secondary',
			iconOnly: true,
			label: m.common_refresh(),
			onclick: refresh,
			loading: isRefreshing,
			disabled: isRefreshing
		});
		if (canPruneImage) {
			buttons.push({
				id: 'prune',
				action: 'remove',
				group: 'danger',
				destructive: true,
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
			iconColor: 'text-info'
		},
		{
			title: m.images_total_size(),
			value: String(bytes.format(imageUsageCounts.totalImageSize)),
			icon: LocalFolderComputerIcon,
			iconColor: 'text-warning'
		}
	]);
</script>

<ResourcePageLayout title={m.images()} subtitle={m.images_subtitle()} {actionButtons} {statCards}>
	{#snippet mainContent()}
		{#if resourcesReady}
			{#key pageState.scope}
				<ImageTable
					images={withFailedEnvironments(images, countFailures)}
					bind:selectedIds={pageState.selectedIds}
					bind:requestOptions={pageState.requestOptions}
					onRetry={refresh}
				/>
			{/key}
		{/if}
	{/snippet}

	{#snippet additionalContent()}
		<ImagePullSheet
			bind:open={isPullDialogOpen}
			onPullFinished={() => queryClient.invalidateQueries({ queryKey: queryKeys.images.listPrefix(pageState.scope) })}
		/>

		<ImageRegistrySearchDialog
			bind:open={isRegistrySearchDialogOpen}
			onPullFinished={() => queryClient.invalidateQueries({ queryKey: queryKeys.images.listPrefix(pageState.scope) })}
		/>

		<Dialog.Root bind:open={isUploadDialogOpen}>
			<Dialog.Content class="max-w-2xl">
				<Dialog.Header>
					<Dialog.Title>{m.images_upload_image()}</Dialog.Title>
					<Dialog.Description>{m.images_upload_description()}</Dialog.Description>
				</Dialog.Header>
				<div class="space-y-4 py-4">
					<EnvironmentDestinationSelect
						bind:value={() => uploadEnvironmentId, (value) => (pickedUploadEnvironmentId = value)}
						permission="images:upload"
						variant="field"
					/>
					<FileDropZone
						{onUpload}
						{onFileRejected}
						maxFileSize={maxUploadSizeMB === undefined ? undefined : maxUploadSizeMB * MEGABYTE}
						accept=".tar,.tar.gz,.tgz,.tar.xz"
						maxFiles={10}
						fileCount={uploadedFiles.length}
						disabled={isUploading}
					/>
					{#if uploadedFiles.length > 0}
						<div class="flex flex-col gap-2">
							{#each uploadedFiles as file, i (file.name)}
								<div class="flex items-center justify-between gap-2 rounded-lg border border-border bg-muted/50 p-3">
									<div class="flex flex-col">
										<span class="text-sm font-medium">{file.name}</span>
										<span class="text-xs text-muted-foreground">{displaySize(file.size)}</span>
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
						<div class="flex flex-col gap-2">
							<div class="flex items-center gap-2 text-sm text-muted-foreground">
								<Spinner class="size-4" />{m.images_uploading()}
								{#if uploadProgress}
									<span class="ml-auto tabular-nums"
										>{Math.round((uploadProgress.bytesDone / uploadProgress.totalBytes) * 100)}%</span
									>
								{/if}
							</div>
							{#if uploadProgress}
								<Progress value={(uploadProgress.bytesDone / uploadProgress.totalBytes) * 100} class="h-1.5" />
							{/if}
						</div>
					{/if}
				</div>
				<div class="flex justify-end gap-3">
					<ArcaneButton
						action="cancel"
						onclick={() => {
							isUploadDialogOpen = false;
							uploadedFiles = [];
							pickedUploadEnvironmentId = '';
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
					<Dialog.Description>
						{m.images_prune_confirm_description({ mode: imagePruneMode })}
						{acrossEnvironmentsNote(actionEnvironments('images:prune'))}
					</Dialog.Description>
				</Dialog.Header>
				<SettingsSection class="py-4">
					<SettingsRow
						label={m.images()}
						description={m.prune_images_dialog_description()}
						layout="wide"
						contentClass="flex justify-center"
					>
						<PruneModePicker
							modeOptions={imagePruneModes}
							bind:value={imagePruneMode}
							bind:untilValue={imagePruneUntil}
							disabled={isPruning}
						/>
					</SettingsRow>
				</SettingsSection>
				<div class="flex justify-end gap-3 pt-6">
					<ArcaneButton action="cancel" onclick={() => (isConfirmPruneDialogOpen = false)} disabled={isPruning} />
					<ArcaneButton
						action="remove"
						onclick={handlePruneImages}
						disabled={isPruning}
						loading={isPruning}
						customLabel={m.prune_images()}
						loadingLabel={m.common_action_pruning()}
					/>
				</div>
			</Dialog.Content>
		</Dialog.Root>
	{/snippet}
</ResourcePageLayout>

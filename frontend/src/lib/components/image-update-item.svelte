<script lang="ts">
	import { createQuery } from '@tanstack/svelte-query';
	import { mergeProps } from 'bits-ui';
	import type { Component } from 'svelte';
	import { toast } from 'svelte-sonner';

	import { CopyButton } from '#lib/components/ui/copy-button/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import UncheckedRingIcon from '#lib/components/unchecked-ring-icon.svelte';
	import UpdateStatusBanner from '#lib/components/update-status-banner.svelte';
	import UpdateStatusPopover from '#lib/components/update-status-popover.svelte';
	import {
		ArrowRightIcon,
		RefreshIcon,
		AlertIcon,
		VerifiedCheckIcon,
		ApiKeyIcon,
		CircleArrowUpIcon,
		BoxIcon,
		ClockIcon,
		DownloadIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import type { ImageUpdateData, ImageUpdateInfoDto } from '#lib/types/docker.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { formatRelativeTime, instantEpochMilliseconds, nowInstantString } from '#lib/utils/formatting.js';
	import { tryCatch } from '#lib/utils/try-catch.js';

	interface Props {
		updateInfo?: ImageUpdateData;
		isLoadingInBackground?: boolean;
		imageId?: string;
		containerId?: string;
		environmentId: string;
		tagUpdates?: boolean;
		/** Image reference for ref-scoped checks when no local image ID exists (e.g. not-pulled refs) */
		imageRef?: string;
		repo?: string;
		tag?: string;
		isLocal?: boolean;
		onUpdated?: (data: ImageUpdateData) => void;
		/** Callback when user clicks "Update Container" button */
		onUpdateContainer?: () => void;
		/** Debug: force hasUpdate to true for testing */
		debugHasUpdate?: boolean;
	}

	let {
		updateInfo,
		isLoadingInBackground = false,
		imageId,
		containerId,
		environmentId,
		tagUpdates = false,
		imageRef,
		repo,
		tag,
		isLocal = false,
		onUpdated,
		onUpdateContainer,
		debugHasUpdate
	}: Props = $props();

	function getCheckTimeValue(info?: ImageUpdateData): number {
		if (!info?.checkTime) return 0;
		return instantEpochMilliseconds(info.checkTime) ?? 0;
	}

	const imageUpdateQuery = createQuery<ImageUpdateData>(() => {
		const target = { imageId, containerId, imageRef, tagUpdates };
		let key = target.imageId || target.imageRef || '';
		if (target.containerId) key = `container:${target.containerId}`;
		return {
			queryKey: queryKeys.images.updateCheck(environmentId, key),
			queryFn: async () => {
				let result: ImageUpdateInfoDto | undefined;
				if (target.imageId && !(target.containerId && target.imageRef)) {
					result = await imageService.checkImageUpdateByID(target.imageId, environmentId);
				} else {
					const ref = target.imageRef ?? '';
					result = (await imageService.checkMultipleImages([ref], environmentId))[ref];
				}
				if (!result) throw new Error(m.images_update_check_failed());
				if (target.containerId) {
					if (target.tagUpdates) {
						const scopedResult = result.containerUpdates?.[target.containerId];
						if (!scopedResult) throw new Error(result.error || m.images_update_check_failed());
						return { ...scopedResult, activityId: scopedResult.activityId ?? result.activityId };
					}
					return result.imageUpdate ?? result;
				}
				return result;
			},
			enabled: false,
			retry: false
		};
	});

	const errorFromQuery = $derived.by((): ImageUpdateData | undefined => {
		if (!imageUpdateQuery.error) return undefined;
		return {
			hasUpdate: false,
			updateType: 'error',
			currentVersion: tag || '',
			currentDigest: '',
			latestVersion: '',
			latestDigest: '',
			checkTime: nowInstantString(),
			responseTimeMs: 0,
			error: imageUpdateQuery.error instanceof Error ? imageUpdateQuery.error.message : m.images_update_check_failed()
		};
	});

	const resolvedUpdateInfo = $derived.by((): ImageUpdateData | undefined => {
		const queryInfo = imageUpdateQuery.data;
		const propInfo = updateInfo;

		if (!queryInfo) return propInfo ?? errorFromQuery;
		if (!propInfo) return queryInfo;

		return getCheckTimeValue(propInfo) > getCheckTimeValue(queryInfo) ? propInfo : queryInfo;
	});

	// If debug is enabled, override hasUpdate to true
	const effectiveUpdateInfo = $derived.by((): ImageUpdateData | undefined => {
		if (!resolvedUpdateInfo) return undefined;
		if (debugHasUpdate) {
			return { ...resolvedUpdateInfo, hasUpdate: true, updateType: resolvedUpdateInfo.updateType || 'tag' };
		}
		return resolvedUpdateInfo;
	});

	const isChecking = $derived(imageUpdateQuery.isFetching);
	let isOpen = $state(false);

	const isLocalImage = $derived(!!isLocal || effectiveUpdateInfo?.updateType === 'local');
	const canCheckUpdate = $derived(
		!!(repo && tag && repo !== '<none>' && tag !== '<none>') && !!(imageId || imageRef) && !isLocalImage
	);
	const hasError = $derived(!!effectiveUpdateInfo?.error && effectiveUpdateInfo.error.trim() !== '');

	const authLabel = $derived.by((): string | null => {
		const mth = effectiveUpdateInfo?.authMethod;
		if (mth === 'credential') {
			const user = effectiveUpdateInfo?.authUsername;
			return user ? m.image_update_auth_credential_with_user({ username: user }) : m.image_update_auth_credential();
		}
		if (mth === 'anonymous') return m.image_update_auth_anonymous();
		if (mth === 'none') return m.image_update_auth_none();
		return null;
	});

	const checkedLabel = $derived(formatRelativeTime(effectiveUpdateInfo?.checkTime));

	const currentVersion = $derived(
		effectiveUpdateInfo?.currentVersion && effectiveUpdateInfo.currentVersion.trim() !== ''
			? effectiveUpdateInfo.currentVersion
			: tag || m.common_unknown()
	);

	const currentDigest = $derived(effectiveUpdateInfo?.currentDigest?.trim() ?? '');
	const latestDigest = $derived(effectiveUpdateInfo?.latestDigest?.trim() ?? '');

	const latestVersion = $derived.by((): string | null => {
		if (hasError) return null;
		if (effectiveUpdateInfo?.latestVersion && effectiveUpdateInfo.latestVersion.trim() !== '') {
			return effectiveUpdateInfo.latestVersion;
		}
		if ((effectiveUpdateInfo?.updateType === 'digest' || effectiveUpdateInfo?.updateType === 'not_pulled') && latestDigest) {
			return currentVersion;
		}
		return null;
	});

	async function checkImageUpdate() {
		if (!canCheckUpdate || imageUpdateQuery.isFetching) return;

		const requestResult1 = await tryCatch(
			(async () => {
				const result = await imageUpdateQuery.refetch();
				if (result.data) {
					onUpdated?.(result.data);
					const toastOptions = activityToastOptions(extractActivityId(result.data));

					if (result.data.error) {
						toast.error(result.data.error || m.images_update_check_failed(), toastOptions);
					} else {
						toast.success(m.images_update_check_completed(), toastOptions);
					}
					return;
				}

				if (result.error) {
					const message = result.error instanceof Error ? result.error.message : m.images_update_check_failed();
					onUpdated?.({
						hasUpdate: false,
						updateType: 'error',
						currentVersion: tag || '',
						currentDigest: '',
						latestVersion: '',
						latestDigest: '',
						checkTime: nowInstantString(),
						responseTimeMs: 0,
						error: message
					});
					toast.error(message);
					return;
				}

				toast.error(m.images_update_check_failed());
			})()
		);
		if (requestResult1.error !== null) {
			const error = requestResult1.error;

			console.error('Error checking update:', error);
			const errorInfo: ImageUpdateData = {
				hasUpdate: false,
				updateType: 'error',
				currentVersion: tag || '',
				currentDigest: '',
				latestVersion: '',
				latestDigest: '',
				checkTime: nowInstantString(),
				responseTimeMs: 0,
				error: (error as Error)?.message || m.images_update_check_failed()
			};
			onUpdated?.(errorInfo);
			toast.error(errorInfo.error);
		}
	}

	function handleUpdateContainer() {
		isOpen = false;
		onUpdateContainer?.();
	}
</script>

{#snippet diffRow(Icon: Component, label: string, version: string, digest: string, accentClass: string)}
	<div class={['rounded-lg px-2.5 py-2', accentClass && 'bg-sidebar-accent']}>
		<div class="flex items-center justify-between gap-2">
			<div class="flex items-center gap-2 text-xs font-medium">
				<Icon class="size-4 shrink-0" />
				<span>{label}</span>
			</div>
			<span class={['rounded-md px-1.5 py-0.5 font-mono text-xs font-medium', accentClass || 'bg-foreground/8']}>
				{version}
			</span>
		</div>
		{#if digest}
			<div class="mt-1 flex items-start gap-1.5 pl-6">
				<span
					class={[
						'min-w-0 flex-1 font-mono text-2xs break-all',
						accentClass ? 'text-sidebar-foreground' : 'text-muted-foreground'
					]}
				>
					{digest}
				</span>
				<CopyButton text={digest} size="icon" class="size-5 shrink-0" />
			</div>
		{/if}
	</div>
{/snippet}

{#snippet metaLine()}
	{#if authLabel || checkedLabel}
		<div class="flex flex-wrap items-center gap-x-3 gap-y-1 px-2.5 pt-2 text-2xs text-muted-foreground">
			{#if authLabel}
				<span class="inline-flex items-center gap-1.5">
					<ApiKeyIcon class="size-3" />
					{authLabel}
				</span>
			{/if}
			{#if checkedLabel}
				<span class="inline-flex items-center gap-1.5">
					<ClockIcon class="size-3" />
					{m.image_update_checked_at({ time: checkedLabel })}
				</span>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet footer()}
	{#if canCheckUpdate}
		<div class="px-2 pb-2">
			{#if effectiveUpdateInfo?.hasUpdate && onUpdateContainer}
				<button
					onclick={handleUpdateContainer}
					class="flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-sidebar-primary text-xs font-medium text-sidebar-primary-foreground transition-colors hover:bg-sidebar-primary/90"
				>
					<CircleArrowUpIcon class="size-3.5" />
					{m.update_container()}
				</button>
			{:else}
				<button
					onclick={checkImageUpdate}
					disabled={isChecking}
					class="group flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-sidebar-accent text-xs font-medium text-sidebar-accent-foreground transition-colors hover:bg-sidebar-accent/80 disabled:cursor-not-allowed disabled:opacity-50"
				>
					{#if isChecking}
						<Spinner class="size-3.5" />
						{m.common_action_checking()}
					{:else}
						<RefreshIcon class="size-3.5 transition-transform group-hover:rotate-45" />
						{m.image_update_recheck_button()}
					{/if}
				</button>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet errorState()}
	<UpdateStatusBanner
		icon={AlertIcon}
		tileClass="text-destructive"
		titleClass="text-destructive"
		title={m.image_update_check_failed_title()}
		description={m.image_update_could_not_query_registry()}
	/>
	<div class="p-2">
		<div
			class="rounded-lg border border-destructive/25 bg-destructive/10 px-2.5 py-2 font-mono text-2xs wrap-break-word text-destructive"
		>
			{effectiveUpdateInfo?.error}
		</div>
		{#if repo && tag}
			<div class="px-2.5 pt-2 font-mono text-2xs break-all text-muted-foreground">{repo}:{tag}</div>
		{/if}
		{@render metaLine()}
	</div>
	{@render footer()}
{/snippet}

{#snippet successState()}
	<UpdateStatusBanner
		icon={VerifiedCheckIcon}
		tileClass="text-success"
		titleClass="text-success"
		title={m.image_update_up_to_date_title()}
		description={m.image_update_up_to_date_desc()}
	/>
	<div class="p-2">
		{@render diffRow(BoxIcon, m.common_running(), currentVersion, currentDigest, '')}
		{@render metaLine()}
	</div>
	{@render footer()}
{/snippet}

{#snippet updateState(Icon: Component, title: string, description: string, toneClass: string, accentClass: string)}
	<UpdateStatusBanner icon={Icon} tileClass={toneClass} titleClass={toneClass} {title} {description} />
	<div class="flex flex-col gap-0.5 p-2">
		{@render diffRow(
			BoxIcon,
			m.common_current(),
			currentVersion,
			effectiveUpdateInfo?.updateType === 'not_pulled' ? '' : currentDigest,
			''
		)}
		{#if latestVersion}
			{@render diffRow(ArrowRightIcon, m.image_update_latest_label(), latestVersion, latestDigest, accentClass)}
		{/if}
		{@render metaLine()}
	</div>
	{@render footer()}
{/snippet}

{#snippet localState()}
	<UpdateStatusBanner
		icon={BoxIcon}
		tileClass="text-muted-foreground"
		titleClass="text-sidebar-foreground"
		title={m.image_update_local_title()}
		description={m.image_update_local_desc()}
	/>
{/snippet}

{#snippet loadingState()}
	<UpdateStatusBanner
		icon={Spinner}
		tileClass="text-info"
		titleClass="text-info"
		title={m.image_update_checking_title()}
		description={m.image_update_querying_registry()}
	/>
{/snippet}

{#snippet unknownState()}
	<UpdateStatusBanner
		icon={AlertIcon}
		tileClass="text-muted-foreground"
		titleClass="text-sidebar-foreground"
		title={m.image_update_status_unknown()}
		description={canCheckUpdate ? m.image_update_click_to_check() : m.image_update_unable_check_tags()}
	/>
{/snippet}

{#if isLocalImage}
	<UpdateStatusPopover bind:open={isOpen}>
		{#snippet trigger({ props })}
			<span
				{...props}
				class="mr-2 inline-flex size-4 items-center justify-center align-middle"
				data-testid="image-update-trigger"
			>
				<BoxIcon class="size-4 text-muted-foreground" />
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
				{@render localState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else if effectiveUpdateInfo}
	<UpdateStatusPopover bind:open={isOpen} contentWidth="xl">
		{#snippet trigger({ props })}
			<span
				{...props}
				class="mr-2 inline-flex size-4 items-center justify-center align-middle"
				data-testid="image-update-trigger"
			>
				{#if hasError}
					<AlertIcon class="size-4 text-destructive" />
				{:else if effectiveUpdateInfo?.updateType === 'not_pulled'}
					<DownloadIcon class="size-4 text-info" />
				{:else if !effectiveUpdateInfo?.hasUpdate}
					<VerifiedCheckIcon class="size-4 text-success" />
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					<CircleArrowUpIcon class="size-4 text-info" />
				{:else}
					<CircleArrowUpIcon class="size-4 text-warning" />
				{/if}
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
				{#if hasError}
					{@render errorState()}
				{:else if effectiveUpdateInfo?.updateType === 'not_pulled'}
					{@render updateState(
						DownloadIcon,
						m.image_update_not_pulled_title(),
						m.image_update_not_pulled_desc(),
						'text-info',
						'bg-info/15 text-info'
					)}
				{:else if !effectiveUpdateInfo?.hasUpdate}
					{@render successState()}
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					{@render updateState(
						CircleArrowUpIcon,
						m.image_update_digest_title(),
						m.image_update_digest_desc(),
						'text-info',
						'bg-info/15 text-info'
					)}
				{:else}
					{@render updateState(
						CircleArrowUpIcon,
						m.image_update_version_title(),
						latestVersion ? m.image_update_tag_description_new({ version: latestVersion }) : m.image_update_version_desc(),
						'text-warning',
						'bg-warning/15 text-warning'
					)}
				{/if}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else if isLoadingInBackground || isChecking}
	<UpdateStatusPopover contentWidth="sm">
		{#snippet trigger({ props })}
			<span {...props} class="mr-2 inline-flex size-4 items-center justify-center" data-testid="image-update-trigger">
				<Spinner tone="info" class="size-4" />
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
				{@render loadingState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else}
	<UpdateStatusPopover interactive directTrigger={canCheckUpdate} contentWidth="md">
		{#snippet trigger({ props })}
			{#if canCheckUpdate}
				{@const triggerProps = mergeProps(props, {
					onclick: checkImageUpdate,
					class:
						'mr-2 inline-flex size-4 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-info disabled:cursor-not-allowed'
				})}
				<button {...triggerProps} disabled={isChecking} data-testid="image-update-trigger">
					{#if isChecking}
						<Spinner tone="info" class="size-3" />
					{:else}
						<UncheckedRingIcon />
					{/if}
				</button>
			{:else}
				<span {...props} class="mr-2 inline-flex size-4 items-center justify-center" data-testid="image-update-trigger">
					<div class="flex size-4 items-center justify-center text-muted-foreground opacity-30">
						<UncheckedRingIcon />
					</div>
				</span>
			{/if}
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
				{@render unknownState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{/if}

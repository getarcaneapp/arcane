<script lang="ts">
	import { tryCatch } from '#lib/utils/try-catch.js';

	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { Badge, type BadgeVariant } from '#lib/components/ui/badge/index.js';
	import { toast } from 'svelte-sonner';
	import type { ImageUpdateData, ImageUpdateInfoDto } from '#lib/types/docker.js';
	import { m } from '#lib/paraglide/messages.js';
	import { imageService } from '#lib/services/image-service.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { environmentStore } from '#lib/stores/environment.store.svelte.js';
	import type { Component } from 'svelte';
	import {
		ArrowRightIcon,
		RefreshIcon,
		AlertIcon,
		VerifiedCheckIcon,
		ApiKeyIcon,
		CircleArrowUpIcon,
		BoxIcon,
		DownloadIcon
	} from '#lib/icons/index.js';
	import { createQuery } from '@tanstack/svelte-query';
	import UpdateStatusPopover from '#lib/components/update-status-popover.svelte';
	import UpdateStatusBanner from '#lib/components/update-status-banner.svelte';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import UncheckedRingIcon from '#lib/components/unchecked-ring-icon.svelte';
	import { mergeProps } from 'bits-ui';
	import { instantEpochMilliseconds, nowInstantString } from '#lib/utils/formatting.js';

	interface Props {
		updateInfo?: ImageUpdateData;
		isLoadingInBackground?: boolean;
		imageId?: string;
		containerId?: string;
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
		const environmentId = environmentStore.selected?.id || '0';
		const target = { imageId, containerId, imageRef, tagUpdates };
		let key = target.imageId || target.imageRef || '';
		if (target.containerId) key = `container:${target.containerId}`;
		return {
			queryKey: queryKeys.images.updateCheck(environmentId, key),
			queryFn: async () => {
				let result: ImageUpdateInfoDto | undefined;
				if (target.imageId && !(target.containerId && target.imageRef)) {
					result = await imageService.checkImageUpdateByID(target.imageId);
				} else {
					const ref = target.imageRef ?? '';
					result = (await imageService.checkMultipleImages([ref]))[ref];
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

	type AuthBadge = { label: string; variant: BadgeVariant };

	const authBadge = $derived.by((): AuthBadge | null => {
		const mth = effectiveUpdateInfo?.authMethod;
		if (!mth) return null;

		if (mth === 'credential') {
			const user = effectiveUpdateInfo?.authUsername;
			return {
				label: user ? m.image_update_auth_credential_with_user({ username: user }) : m.image_update_auth_credential(),
				variant: 'amber'
			};
		}
		if (mth === 'anonymous') {
			return {
				label: m.image_update_auth_anonymous(),
				variant: 'gray'
			};
		}
		if (mth === 'none') {
			return {
				label: m.image_update_auth_none(),
				variant: 'gray'
			};
		}
		return null;
	});

	const currentVersion = $derived(
		effectiveUpdateInfo?.currentVersion && effectiveUpdateInfo.currentVersion.trim() !== ''
			? effectiveUpdateInfo.currentVersion
			: tag || m.common_unknown()
	);

	const latestVersion = $derived.by((): string | null => {
		if (hasError) return null;
		if (effectiveUpdateInfo?.latestVersion && effectiveUpdateInfo.latestVersion.trim() !== '') {
			return effectiveUpdateInfo.latestVersion;
		}
		if (
			(effectiveUpdateInfo?.updateType === 'digest' || effectiveUpdateInfo?.updateType === 'not_pulled') &&
			effectiveUpdateInfo?.latestDigest
		) {
			return effectiveUpdateInfo.latestDigest.slice(7, 19) + '...';
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

	const updatePriority = $derived.by(() => {
		if (!effectiveUpdateInfo) return null;
		if (effectiveUpdateInfo.error)
			return { level: 'Error', color: 'text-destructive', description: m.image_update_could_not_query_registry() };
		if (effectiveUpdateInfo.updateType === 'local')
			return { level: m.image_update_local_title(), color: 'text-muted-foreground', description: m.image_update_local_desc() };
		if (effectiveUpdateInfo.updateType === 'not_pulled')
			return {
				level: m.image_update_not_pulled_title(),
				color: 'text-info',
				description: m.image_update_not_pulled_desc()
			};
		if (!effectiveUpdateInfo.hasUpdate)
			return { level: 'None', color: 'text-success', description: m.image_update_up_to_date_desc() };
		if (effectiveUpdateInfo.updateType === 'digest')
			return {
				level: m.image_update_digest_title(),
				color: 'text-info',
				description: m.image_update_digest_desc()
			};
		if (effectiveUpdateInfo.updateType === 'tag') {
			const desc = effectiveUpdateInfo.latestVersion
				? m.image_update_tag_description_new({ version: effectiveUpdateInfo.latestVersion })
				: m.image_update_tag_description();
			return { level: m.image_update_version_title(), color: 'text-warning', description: desc };
		}
		return { level: m.common_unknown(), color: 'text-muted-foreground', description: m.image_update_unknown_type() };
	});
</script>

{#snippet iconCircle(Icon: Component, gradientFrom: string, gradientTo: string, shadowColor: string)}
	<div
		class="flex h-10 w-10 items-center justify-center rounded-full bg-linear-to-br {gradientFrom} {gradientTo} shadow-lg {shadowColor}"
	>
		<Icon class="size-5 text-white" />
	</div>
{/snippet}

{#snippet authBadgeDisplay()}
	{#if authBadge}
		<div class="mt-2">
			<Badge variant={authBadge.variant} size="sm">
				<ApiKeyIcon class="opacity-80" />
				<span>{m.image_update_auth({ label: authBadge.label })}</span>
			</Badge>
		</div>
	{/if}
{/snippet}

{#snippet versionDisplay(label: string, version: string, bgClass: string, textClass: string = '')}
	<div class="flex items-center justify-between">
		<div class="flex items-center gap-1.5 text-muted-foreground">
			{#if label === m.common_current()}
				<BoxIcon class="size-3" />
			{:else}
				<ArrowRightIcon class="size-3" />
			{/if}
			<span>{label}</span>
		</div>
		<span class="rounded {bgClass} px-1.5 py-0.5 font-mono font-medium {textClass}">
			{version}
		</span>
	</div>
{/snippet}

{#snippet recheckButton()}
	{#if canCheckUpdate}
		<div class="border-t border-border/50 bg-muted/50 p-3">
			{#if effectiveUpdateInfo?.hasUpdate && onUpdateContainer}
				<button
					onclick={handleUpdateContainer}
					class="group flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 py-2 text-xs font-medium text-primary-foreground shadow-sm transition-all hover:bg-primary/90 hover:shadow-md"
				>
					<CircleArrowUpIcon class="size-3" />
					{m.update_container()}
				</button>
			{:else}
				<button
					onclick={checkImageUpdate}
					disabled={isChecking}
					class="group flex w-full items-center justify-center gap-2 rounded-lg bg-secondary/80 px-3 py-2 text-xs font-medium text-secondary-foreground shadow-sm transition-all hover:bg-secondary hover:shadow-md disabled:cursor-not-allowed disabled:opacity-50"
				>
					{#if isChecking}
						<Spinner class="size-3" />
						{m.common_action_checking()}
					{:else}
						<RefreshIcon class="size-3 transition-transform group-hover:rotate-45" />
						{m.image_update_recheck_button()}
					{/if}
				</button>
			{/if}
		</div>
	{/if}
{/snippet}

{#snippet errorState()}
	<div class="bg-linear-to-br from-destructive/10 to-destructive/5 p-4">
		<div class="flex items-start gap-3">
			{@render iconCircle(AlertIcon, 'from-rose', 'to-destructive', 'shadow-destructive/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-destructive">{m.image_update_check_failed_title()}</div>
				<div class="text-xs text-destructive/80">{m.image_update_could_not_query_registry()}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	<div class="bg-transparent p-4">
		<div class="space-y-3">
			<div class="text-xs text-muted-foreground">
				<span class="font-medium">{m.image_update_error_label()}</span>
				<span class="ml-1 wrap-break-word">{effectiveUpdateInfo?.error}</span>
			</div>
			{#if repo && tag}
				<div class="text-xs text-muted-foreground">
					{m.image_update_image_label()} <span class="font-mono">{repo}:{tag}</span>
				</div>
			{/if}
		</div>
	</div>
	{@render recheckButton()}
{/snippet}

{#snippet successState()}
	<div class="bg-linear-to-br from-success/10 to-success/5 p-4">
		<div class="flex items-start gap-3">
			{@render iconCircle(VerifiedCheckIcon, 'from-success', 'to-success', 'shadow-success/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-success">
					{m.image_update_up_to_date_title()}
				</div>
				<div class="text-xs text-success/80">{m.image_update_up_to_date_desc()}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	<div class="bg-transparent p-4">
		<div class="text-center">
			<div class="mb-2 text-xs text-muted-foreground">
				{m.common_running()}
				<span class="rounded bg-muted px-1.5 py-0.5 font-mono text-xs font-medium">{currentVersion}</span>
			</div>
			<div class="text-xs leading-relaxed text-muted-foreground">
				{m.image_update_up_to_date_desc()}
			</div>
		</div>
	</div>
	{@render recheckButton()}
{/snippet}

{#snippet updateDetails(latestLabel: string, latestBg: string, latestText: string, boxBg: string, boxText: string)}
	<div class="bg-transparent p-4">
		<div class="space-y-3">
			<div class="space-y-2 text-xs">
				{@render versionDisplay(m.common_current(), currentVersion, 'bg-muted', '')}
				{#if latestVersion}
					{@render versionDisplay(latestLabel, latestVersion, latestBg, latestText)}
				{/if}
			</div>
			{#if updatePriority}
				<div class="rounded-lg {boxBg} p-3">
					<div class="text-center text-xs leading-relaxed font-medium {boxText}">
						{updatePriority.description}
					</div>
				</div>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet digestState(title: string, description: string, icon: Component)}
	<div class="bg-linear-to-br from-info/10 to-info/5 p-4">
		<div class="flex items-start gap-3">
			{@render iconCircle(icon, 'from-info', 'to-cyan', 'shadow-info/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-info">{title}</div>
				<div class="text-xs text-info/80">{description}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	{@render updateDetails(m.image_update_latest_digest_label(), 'bg-info/10', 'text-info', 'bg-info/10', 'text-info')}
	{@render recheckButton()}
{/snippet}

{#snippet versionUpdateState()}
	<div class="bg-linear-to-br from-warning/10 to-warning/5 p-4">
		<div class="flex items-start gap-3">
			{@render iconCircle(CircleArrowUpIcon, 'from-warning', 'to-warning', 'shadow-warning/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-warning">{m.image_update_version_title()}</div>
				<div class="text-xs text-warning/80">{m.image_update_version_desc()}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	{@render updateDetails(m.image_update_latest_label(), 'bg-warning/10', 'text-warning', 'bg-warning/10', 'text-warning')}
	{@render recheckButton()}
{/snippet}

{#snippet localState()}
	<div class="bg-linear-to-br from-muted/50 to-muted/20 p-4">
		<div class="flex items-start gap-3">
			{@render iconCircle(BoxIcon, 'from-muted-foreground', 'to-muted-foreground', 'shadow-muted-foreground/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-foreground">{m.image_update_local_title()}</div>
				<div class="text-xs text-muted-foreground">{m.image_update_local_desc()}</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet loadingState()}
	<UpdateStatusBanner
		icon={Spinner}
		wrapperClass="bg-linear-to-br from-info/10 to-info/5 p-4"
		gradientFrom="from-info"
		gradientTo="to-cyan"
		shadowColor="shadow-info/25"
		titleClass="text-info"
		descriptionClass="text-info/80"
		title={m.image_update_checking_title()}
		description={m.image_update_querying_registry()}
	/>
{/snippet}

{#snippet unknownState()}
	<div class="bg-linear-to-br from-muted/50 to-muted/20 p-4">
		<div class="flex items-center gap-3">
			{@render iconCircle(AlertIcon, 'from-muted-foreground', 'to-muted-foreground', 'shadow-muted-foreground/25')}
			<div>
				<div class="text-sm font-semibold text-foreground">{m.image_update_status_unknown()}</div>
				<div class="text-xs text-muted-foreground">
					{#if canCheckUpdate}
						{m.image_update_click_to_check()}
					{:else}
						{m.image_update_unable_check_tags()}
					{/if}
				</div>
			</div>
		</div>
	</div>
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
			<div class="overflow-hidden rounded-xl">
				{@render localState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else if effectiveUpdateInfo}
	<UpdateStatusPopover bind:open={isOpen}>
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
			<div class="overflow-hidden rounded-xl">
				{#if hasError}
					{@render errorState()}
				{:else if effectiveUpdateInfo?.updateType === 'not_pulled'}
					{@render digestState(m.image_update_not_pulled_title(), m.image_update_not_pulled_desc(), DownloadIcon)}
				{:else if !effectiveUpdateInfo?.hasUpdate}
					{@render successState()}
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					{@render digestState(m.image_update_digest_title(), m.image_update_digest_desc(), CircleArrowUpIcon)}
				{:else}
					{@render versionUpdateState()}
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
			<div class="overflow-hidden rounded-xl">
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
			<div class="overflow-hidden rounded-xl">
				{@render unknownState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{/if}

<script lang="ts">
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { Badge, type BadgeVariant } from '$lib/components/ui/badge';
	import { toast } from 'svelte-sonner';
	import type { ImageUpdateData } from '$lib/types/docker';
	import { m } from '$lib/paraglide/messages';
	import { imageService } from '$lib/services/image-service';
	import { queryKeys } from '$lib/query/query-keys';
	import { environmentStore } from '$lib/stores/environment.store.svelte';
	import type { Component } from 'svelte';
	import { ArrowRightIcon, RefreshIcon, AlertIcon, VerifiedCheckIcon, ApiKeyIcon, CircleArrowUpIcon, BoxIcon } from '$lib/icons';
	import { createQuery } from '@tanstack/svelte-query';
	import UpdateStatusPopover from '$lib/components/update-status-popover.svelte';
	import UpdateStatusBanner from '$lib/components/update-status-banner.svelte';
	import { activityToastOptions, extractActivityId } from '$lib/utils/activity-toast';
	import UncheckedRingIcon from '$lib/components/unchecked-ring-icon.svelte';
	import { mergeProps } from 'bits-ui';

	interface Props {
		updateInfo?: ImageUpdateData;
		isLoadingInBackground?: boolean;
		imageId: string;
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
		repo,
		tag,
		isLocal = false,
		onUpdated,
		onUpdateContainer,
		debugHasUpdate
	}: Props = $props();

	function getCheckTimeValue(info?: ImageUpdateData): number {
		if (!info?.checkTime) return 0;
		const parsed = Date.parse(info.checkTime);
		return Number.isNaN(parsed) ? 0 : parsed;
	}

	const imageUpdateQuery = createQuery<ImageUpdateData>(() => ({
		queryKey: queryKeys.images.updateCheck(environmentStore.selected?.id || '0', imageId),
		queryFn: () => imageService.checkImageUpdateByID(imageId),
		enabled: false,
		retry: false
	}));

	const errorFromQuery = $derived.by((): ImageUpdateData | undefined => {
		if (!imageUpdateQuery.error) return undefined;
		return {
			hasUpdate: false,
			updateType: 'error',
			currentVersion: tag || '',
			currentDigest: '',
			latestVersion: '',
			latestDigest: '',
			checkTime: new Date().toISOString(),
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
	const canCheckUpdate = $derived(!!(repo && tag && repo !== '<none>' && tag !== '<none>') && !isLocalImage);
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
		if (effectiveUpdateInfo?.updateType === 'digest' && effectiveUpdateInfo?.latestDigest) {
			return effectiveUpdateInfo.latestDigest.slice(7, 19) + '...';
		}
		return null;
	});

	async function checkImageUpdate() {
		if (!canCheckUpdate || imageUpdateQuery.isFetching) return;

		try {
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
					checkTime: new Date().toISOString(),
					responseTimeMs: 0,
					error: message
				});
				toast.error(message);
				return;
			}

			toast.error(m.images_update_check_failed());
		} catch (error) {
			console.error('Error checking update:', error);
			const errorInfo: ImageUpdateData = {
				hasUpdate: false,
				updateType: 'error',
				currentVersion: tag || '',
				currentDigest: '',
				latestVersion: '',
				latestDigest: '',
				checkTime: new Date().toISOString(),
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
			return { level: 'Error', color: 'text-red-500', description: m.image_update_could_not_query_registry() };
		if (effectiveUpdateInfo.updateType === 'local')
			return { level: m.image_update_local_title(), color: 'text-slate-500', description: m.image_update_local_desc() };
		if (!effectiveUpdateInfo.hasUpdate)
			return { level: 'None', color: 'text-green-500', description: m.image_update_up_to_date_desc() };
		if (effectiveUpdateInfo.updateType === 'digest')
			return {
				level: m.image_update_digest_title(),
				color: 'text-blue-500',
				description: m.image_update_digest_desc()
			};
		if (effectiveUpdateInfo.updateType === 'tag') {
			const desc = effectiveUpdateInfo.latestVersion
				? m.image_update_tag_description_new({ version: effectiveUpdateInfo.latestVersion })
				: m.image_update_tag_description();
			return { level: m.image_update_version_title(), color: 'text-yellow-500', description: desc };
		}
		return { level: m.common_unknown(), color: 'text-gray-500', description: m.image_update_unknown_type() };
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
		<div class="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
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
	<div class="bg-linear-to-br from-rose-50 to-red-50/40 p-4 dark:from-rose-950/20 dark:to-red-950/10">
		<div class="flex items-start gap-3">
			{@render iconCircle(AlertIcon, 'from-rose-500', 'to-red-500', 'shadow-red-500/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-red-950 dark:text-red-100">{m.image_update_check_failed_title()}</div>
				<div class="text-xs text-red-900/80 dark:text-red-300/80">{m.image_update_could_not_query_registry()}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	<div class="bg-transparent p-4">
		<div class="space-y-3">
			<div class="text-xs text-gray-600 dark:text-gray-300">
				<span class="font-medium">{m.image_update_error_label()}</span>
				<span class="ml-1 wrap-break-word">{effectiveUpdateInfo?.error}</span>
			</div>
			{#if repo && tag}
				<div class="text-xs text-gray-500 dark:text-gray-400">
					{m.image_update_image_label()} <span class="font-mono">{repo}:{tag}</span>
				</div>
			{/if}
		</div>
	</div>
	{@render recheckButton()}
{/snippet}

{#snippet successState()}
	<div class="bg-linear-to-br from-emerald-50 to-green-50/30 p-4 dark:from-emerald-950/20 dark:to-green-950/10">
		<div class="flex items-start gap-3">
			{@render iconCircle(VerifiedCheckIcon, 'from-emerald-500', 'to-green-500', 'shadow-emerald-500/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-emerald-950 dark:text-emerald-100">
					{m.image_update_up_to_date_title()}
				</div>
				<div class="text-xs text-emerald-900/80 dark:text-emerald-300/80">{m.image_update_up_to_date_desc()}</div>
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

{#snippet digestUpdateState()}
	<div class="bg-linear-to-br from-blue-50 to-cyan-50/30 p-4 dark:from-blue-950/20 dark:to-cyan-950/10">
		<div class="flex items-start gap-3">
			{@render iconCircle(CircleArrowUpIcon, 'from-blue-500', 'to-cyan-500', 'shadow-blue-500/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-blue-950 dark:text-blue-100">{m.image_update_digest_title()}</div>
				<div class="text-xs text-blue-900/80 dark:text-blue-300/80">{m.image_update_digest_desc()}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	{@render updateDetails(
		m.image_update_latest_digest_label(),
		'bg-blue-100 dark:bg-blue-900/30',
		'text-blue-800 dark:text-blue-300',
		'bg-blue-50 dark:bg-blue-950/30',
		'text-blue-800 dark:text-blue-300'
	)}
	{@render recheckButton()}
{/snippet}

{#snippet versionUpdateState()}
	<div class="bg-linear-to-br from-amber-50 to-yellow-50/30 p-4 dark:from-amber-950/20 dark:to-yellow-950/10">
		<div class="flex items-start gap-3">
			{@render iconCircle(CircleArrowUpIcon, 'from-amber-500', 'to-yellow-500', 'shadow-amber-500/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-amber-950 dark:text-amber-100">{m.image_update_version_title()}</div>
				<div class="text-xs text-amber-900/80 dark:text-amber-300/80">{m.image_update_version_desc()}</div>
				{@render authBadgeDisplay()}
			</div>
		</div>
	</div>
	{@render updateDetails(
		m.image_update_latest_label(),
		'bg-amber-100 dark:bg-amber-900/30',
		'text-amber-800 dark:text-amber-300',
		'bg-amber-50 dark:bg-amber-950/30',
		'text-amber-800 dark:text-amber-300'
	)}
	{@render recheckButton()}
{/snippet}

{#snippet localState()}
	<div class="bg-linear-to-br from-slate-50 to-slate-100/40 p-4 dark:from-slate-950/20 dark:to-slate-900/20">
		<div class="flex items-start gap-3">
			{@render iconCircle(BoxIcon, 'from-slate-500', 'to-gray-500', 'shadow-slate-500/25')}
			<div class="flex-1">
				<div class="text-sm font-semibold text-slate-950 dark:text-slate-100">{m.image_update_local_title()}</div>
				<div class="text-xs text-slate-900/80 dark:text-slate-300/80">{m.image_update_local_desc()}</div>
			</div>
		</div>
	</div>
{/snippet}

{#snippet loadingState()}
	<UpdateStatusBanner
		icon={Spinner}
		wrapperClass="bg-linear-to-br from-blue-50 to-cyan-50/30 p-4 dark:from-blue-950/20 dark:to-cyan-950/10"
		gradientFrom="from-blue-500"
		gradientTo="to-cyan-500"
		shadowColor="shadow-blue-500/25"
		titleClass="text-blue-950 dark:text-blue-100"
		descriptionClass="text-blue-900/80 dark:text-blue-300/80"
		title={m.image_update_checking_title()}
		description={m.image_update_querying_registry()}
	/>
{/snippet}

{#snippet unknownState()}
	<div class="bg-linear-to-br from-gray-50 to-slate-50/30 p-4 dark:from-gray-900/20 dark:to-slate-900/10">
		<div class="flex items-center gap-3">
			{@render iconCircle(AlertIcon, 'from-gray-400', 'to-slate-500', 'shadow-gray-400/25')}
			<div>
				<div class="text-sm font-semibold text-gray-950 dark:text-gray-100">{m.image_update_status_unknown()}</div>
				<div class="text-xs text-gray-800 dark:text-gray-300/80">
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
	<UpdateStatusPopover bind:open={isOpen} contentClass="max-w-[280px] p-0">
		{#snippet trigger({ props })}
			<span
				{...props}
				class="mr-2 inline-flex size-4 items-center justify-center align-middle"
				data-testid="image-update-trigger"
			>
				<BoxIcon class="size-4 text-slate-500" />
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl">
				{@render localState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else if effectiveUpdateInfo}
	<UpdateStatusPopover bind:open={isOpen} contentClass="max-w-[280px] p-0">
		{#snippet trigger({ props })}
			<span
				{...props}
				class="mr-2 inline-flex size-4 items-center justify-center align-middle"
				data-testid="image-update-trigger"
			>
				{#if hasError}
					<AlertIcon class="size-4 text-red-500" />
				{:else if !effectiveUpdateInfo?.hasUpdate}
					<VerifiedCheckIcon class="size-4 text-green-500" />
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					<CircleArrowUpIcon class="size-4 text-blue-500" />
				{:else}
					<CircleArrowUpIcon class="size-4 text-yellow-500" />
				{/if}
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl">
				{#if hasError}
					{@render errorState()}
				{:else if !effectiveUpdateInfo?.hasUpdate}
					{@render successState()}
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					{@render digestUpdateState()}
				{:else}
					{@render versionUpdateState()}
				{/if}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else if isLoadingInBackground || isChecking}
	<UpdateStatusPopover contentClass="max-w-[220px] p-0">
		{#snippet trigger({ props })}
			<span {...props} class="mr-2 inline-flex size-4 items-center justify-center" data-testid="image-update-trigger">
				<Spinner class="size-4 text-blue-400" />
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl">
				{@render loadingState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else}
	<UpdateStatusPopover interactive directTrigger={canCheckUpdate} contentClass="max-w-[240px] p-0">
		{#snippet trigger({ props })}
			{#if canCheckUpdate}
				{@const triggerProps = mergeProps(props, {
					onclick: checkImageUpdate,
					class:
						'mr-2 inline-flex size-4 items-center justify-center rounded-full text-gray-400 transition-colors hover:text-blue-400 disabled:cursor-not-allowed'
				})}
				<button {...triggerProps} disabled={isChecking} data-testid="image-update-trigger">
					{#if isChecking}
						<Spinner class="size-3 text-blue-400" />
					{:else}
						<UncheckedRingIcon />
					{/if}
				</button>
			{:else}
				<span {...props} class="mr-2 inline-flex size-4 items-center justify-center" data-testid="image-update-trigger">
					<div class="flex size-4 items-center justify-center text-gray-400 opacity-30">
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

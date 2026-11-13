<script lang="ts">
	import { createMutation } from '@tanstack/svelte-query';
	import { mergeProps } from 'bits-ui';
	import type { Component } from 'svelte';
	import { toast } from 'svelte-sonner';

	import ImageReleaseNotesDialog from '#lib/components/dialogs/image-release-notes-dialog.svelte';
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
		ContainersIcon,
		ClockIcon,
		DownloadIcon,
		FileTextIcon,
		ShieldAlertIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { queryKeys } from '#lib/query/query-keys.js';
	import { imageService } from '#lib/services/image-service.js';
	import type { ImageUpdateResponse } from '#lib/types/imageupdate.js';
	import { activityToastOptions, extractActivityId } from '#lib/utils/activity-toast.js';
	import { extractApiErrorMessage } from '#lib/utils/api.js';
	import { hasPermission } from '#lib/utils/auth.js';
	import { formatDateTimeShort, formatRelativeTime, instantEpochMilliseconds, nowInstantString } from '#lib/utils/formatting.js';

	interface Props {
		updateInfo?: ImageUpdateResponse;
		isLoadingInBackground?: boolean;
		imageId?: string;
		containerId?: string;
		environmentId: string;
		tagUpdates?: boolean;
		imageRef?: string;
		repo?: string;
		tag?: string;
		isLocal?: boolean;
		onUpdated?: (data: ImageUpdateResponse) => void;
		onUpdateContainer?: () => void;
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

	function getCheckTimeValue(info?: ImageUpdateResponse): number {
		if (!info?.checkTime) return 0;
		return instantEpochMilliseconds(info.checkTime) ?? 0;
	}

	function errorUpdateInfo(message: string): ImageUpdateResponse {
		return {
			hasUpdate: false,
			updateType: 'error',
			currentVersion: tag || '',
			currentDigest: '',
			latestVersion: '',
			latestDigest: '',
			checkTime: nowInstantString(),
			responseTimeMs: 0,
			error: message
		};
	}

	const checkKey = $derived(containerId ? `container:${containerId}` : imageId || imageRef || '');

	const updateCheckMutation = createMutation(() => ({
		mutationKey: queryKeys.images.updateCheck(environmentId, checkKey),
		mutationFn: async (_key: string): Promise<ImageUpdateResponse> => {
			let result: ImageUpdateResponse | undefined;
			if (imageId && !(containerId && imageRef)) {
				result = await imageService.checkImageUpdateByID(imageId, environmentId);
			} else {
				const ref = imageRef ?? '';
				result = (await imageService.checkMultipleImages([ref], environmentId))[ref];
			}
			if (!result) throw new Error(m.images_update_check_failed());
			if (containerId) {
				if (tagUpdates) {
					const scopedResult = result.containerUpdates?.[containerId];
					if (!scopedResult) throw new Error(result.error || m.images_update_check_failed());
					return { ...scopedResult, activityId: scopedResult.activityId ?? result.activityId };
				}
				return result.imageUpdate ?? result;
			}
			return result;
		},
		onSuccess: (data) => {
			onUpdated?.(data);
			const toastOptions = activityToastOptions(extractActivityId(data));
			if (data.error) {
				toast.error(data.error, toastOptions);
				return;
			}
			toast.success(m.images_update_check_completed(), toastOptions);
		},
		onError: (error) => {
			const message = extractApiErrorMessage(error);
			onUpdated?.(errorUpdateInfo(message));
			toast.error(m.images_update_check_failed(), { description: message });
		}
	}));

	const checkMatches = $derived(updateCheckMutation.variables === checkKey);
	const errorFromCheck = $derived(
		checkMatches && updateCheckMutation.error ? errorUpdateInfo(extractApiErrorMessage(updateCheckMutation.error)) : undefined
	);

	const resolvedUpdateInfo = $derived.by((): ImageUpdateResponse | undefined => {
		const checkInfo = checkMatches ? updateCheckMutation.data : undefined;
		const propInfo = updateInfo;

		if (!checkInfo) return propInfo ?? errorFromCheck;
		if (!propInfo) return checkInfo;

		return getCheckTimeValue(propInfo) > getCheckTimeValue(checkInfo) ? propInfo : checkInfo;
	});

	const effectiveUpdateInfo = $derived.by((): ImageUpdateResponse | undefined => {
		if (!resolvedUpdateInfo) return undefined;
		if (debugHasUpdate) {
			return { ...resolvedUpdateInfo, hasUpdate: true, updateType: resolvedUpdateInfo.updateType || 'tag' };
		}
		return resolvedUpdateInfo;
	});

	const isChecking = $derived(updateCheckMutation.isPending);
	let isOpen = $state(false);
	let releaseNotesOpen = $state(false);
	let releaseNotesTarget = $state<{ imageRef: string; version?: string; digest?: string } | null>(null);
	const releaseNotesRef = $derived(imageRef || (repo && tag ? `${repo}:${tag}` : ''));
	const canShowReleaseNotes = $derived(
		!!effectiveUpdateInfo?.hasUpdate &&
			effectiveUpdateInfo.updateType !== 'not_pulled' &&
			!!releaseNotesRef &&
			!!environmentId &&
			hasPermission('image-updates:read', environmentId)
	);

	const isLocalImage = $derived(!!isLocal || effectiveUpdateInfo?.updateType === 'local');
	const canCheckUpdate = $derived(
		!!(repo && tag && repo !== '<none>' && tag !== '<none>') && !!(imageId || imageRef) && !isLocalImage
	);
	const hasError = $derived(!!effectiveUpdateInfo?.error && effectiveUpdateInfo.error.trim() !== '');
	const isBlocked = $derived(!hasError && !!effectiveUpdateInfo?.hasUpdate && !!effectiveUpdateInfo.blocked);

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

	const statusTriggerClass = 'mr-2 inline-flex h-5 min-w-4 items-center gap-1.5 text-left whitespace-nowrap';

	const checkedLabel = $derived(formatRelativeTime(effectiveUpdateInfo?.checkTime));

	const statusLabel = $derived.by((): string => {
		if (isLocalImage) return m.image_update_local_title();
		if (!effectiveUpdateInfo) {
			return isLoadingInBackground || isChecking ? m.image_update_checking_title() : m.image_update_status_unknown();
		}
		if (hasError) return m.image_update_check_failed_title();
		if (effectiveUpdateInfo.updateType === 'not_pulled') return m.image_update_not_pulled_title();
		if (!effectiveUpdateInfo.hasUpdate) return m.image_update_up_to_date_title();
		if (isBlocked) return m.blocked();
		if (effectiveUpdateInfo.updateType === 'digest' && !versionChanged) return m.image_update_digest_title();
		return m.image_update_version_title();
	});

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
		return null;
	});

	const versionChanged = $derived(!!latestVersion && latestVersion !== currentVersion);

	function checkImageUpdate() {
		if (!canCheckUpdate || updateCheckMutation.isPending) return;
		updateCheckMutation.mutate(checkKey);
	}

	function openReleaseNotes() {
		isOpen = false;
		releaseNotesTarget = {
			imageRef: releaseNotesRef,
			version: effectiveUpdateInfo?.updateType === 'tag' ? effectiveUpdateInfo.latestVersion : undefined,
			digest: effectiveUpdateInfo?.latestDigest
		};
		releaseNotesOpen = true;
	}

	function handleUpdateContainer() {
		isOpen = false;
		onUpdateContainer?.();
	}
</script>

{#snippet diffRow(Icon: Component, label: string, version: string, digest: string, created: string, accentClass: string)}
	<div class={['rounded-lg px-2.5 py-2', accentClass && 'bg-sidebar-accent']}>
		<div class="flex items-center justify-between gap-2">
			<div class="flex items-center gap-2 text-xs font-medium">
				<Icon class="size-4 shrink-0" />
				<span>{label}</span>
			</div>
			{#if version}
				<span class={['rounded-md px-1.5 py-0.5 font-mono text-xs font-medium', accentClass || 'bg-foreground/8']}>
					{version}
				</span>
			{/if}
		</div>
		{#if created && formatDateTimeShort(created)}
			<div class={['mt-1 pl-6 text-2xs', accentClass ? 'text-sidebar-foreground' : 'text-muted-foreground']}>
				{m.built_on({ date: formatDateTimeShort(created) })}
			</div>
		{/if}
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
		<div class="flex flex-col gap-1 px-2 pb-2">
			{#if canShowReleaseNotes}
				<button
					type="button"
					onclick={openReleaseNotes}
					class="flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-sidebar-accent text-xs font-medium text-sidebar-accent-foreground transition-colors hover:bg-sidebar-accent/80"
				>
					<FileTextIcon class="size-3.5" />
					{m.image_update_release_notes_button()}
				</button>
			{/if}
			{#if effectiveUpdateInfo?.hasUpdate && onUpdateContainer}
				<button
					type="button"
					onclick={handleUpdateContainer}
					class="flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-sidebar-primary text-xs font-medium text-sidebar-primary-foreground transition-colors hover:bg-sidebar-primary/90"
				>
					<CircleArrowUpIcon class="size-3.5" />
					{m.update_container()}
				</button>
			{:else}
				<button
					type="button"
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
		{@render diffRow(
			ContainersIcon,
			m.common_running(),
			currentVersion,
			currentDigest,
			effectiveUpdateInfo?.currentCreated ?? '',
			''
		)}
		{@render metaLine()}
	</div>
	{@render footer()}
{/snippet}

{#snippet updateState(Icon: Component, title: string, description: string, toneClass: string, accentClass: string)}
	<UpdateStatusBanner icon={Icon} tileClass={toneClass} titleClass={toneClass} {title} {description} />
	<div class="flex flex-col gap-0.5 p-2">
		{@render diffRow(
			ContainersIcon,
			m.common_current(),
			currentVersion,
			effectiveUpdateInfo?.updateType === 'not_pulled' ? '' : currentDigest,
			effectiveUpdateInfo?.currentCreated ?? '',
			''
		)}
		{#if latestVersion || latestDigest}
			{@render diffRow(
				ArrowRightIcon,
				m.image_update_latest_label(),
				latestVersion ?? '',
				latestDigest,
				effectiveUpdateInfo?.latestCreated ?? '',
				accentClass
			)}
		{/if}
		{@render metaLine()}
	</div>
	{@render footer()}
{/snippet}

{#snippet localState()}
	<UpdateStatusBanner
		icon={ContainersIcon}
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

{#snippet statusText()}
	<span
		class={[
			'text-xs-plus leading-5',
			hasError && 'text-destructive',
			isBlocked && 'text-warning',
			!hasError && !isBlocked && effectiveUpdateInfo?.hasUpdate && 'text-update',
			!hasError && !effectiveUpdateInfo?.hasUpdate && 'text-muted-foreground'
		]}
	>
		{statusLabel}
	</span>
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
	<UpdateStatusPopover bind:open={isOpen} label={statusLabel}>
		{#snippet trigger({ props })}
			<span {...props} class={statusTriggerClass} data-testid="image-update-trigger">
				<ContainersIcon class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
				{@render statusText()}
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
				{@render localState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else if effectiveUpdateInfo}
	<UpdateStatusPopover bind:open={isOpen} label={statusLabel} contentWidth="xl">
		{#snippet trigger({ props })}
			<span {...props} class={statusTriggerClass} data-testid="image-update-trigger">
				{#if hasError}
					<AlertIcon class="size-4 shrink-0 text-destructive" aria-hidden="true" />
				{:else if effectiveUpdateInfo?.updateType === 'not_pulled'}
					<DownloadIcon class="size-4 shrink-0 text-info" aria-hidden="true" />
				{:else if !effectiveUpdateInfo?.hasUpdate}
					<VerifiedCheckIcon class="size-4 shrink-0 text-success" aria-hidden="true" />
				{:else if isBlocked}
					<ShieldAlertIcon class="size-4 shrink-0 text-warning" aria-hidden="true" />
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					<CircleArrowUpIcon class="size-4 shrink-0 text-info" aria-hidden="true" />
				{:else}
					<CircleArrowUpIcon class="size-4 shrink-0 text-warning" aria-hidden="true" />
				{/if}
				{@render statusText()}
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
				{:else if isBlocked}
					{@render updateState(
						ShieldAlertIcon,
						m.update_blocked_by_vulnerability_limits(),
						effectiveUpdateInfo?.blockedReason || m.blocked(),
						'text-warning',
						'bg-warning/15 text-warning'
					)}
				{:else if effectiveUpdateInfo?.updateType === 'digest'}
					{@render updateState(
						CircleArrowUpIcon,
						versionChanged ? m.image_update_version_title() : m.image_update_digest_title(),
						versionChanged && latestVersion
							? m.image_update_tag_description_new({ version: latestVersion })
							: m.image_update_digest_desc(),
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
	<UpdateStatusPopover label={statusLabel} contentWidth="sm">
		{#snippet trigger({ props })}
			<span {...props} class={statusTriggerClass} data-testid="image-update-trigger">
				<Spinner tone="info" class="size-4 shrink-0" />
				{@render statusText()}
			</span>
		{/snippet}

		{#snippet content()}
			<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
				{@render loadingState()}
			</div>
		{/snippet}
	</UpdateStatusPopover>
{:else}
	<UpdateStatusPopover label={statusLabel} interactive directTrigger={canCheckUpdate} contentWidth="md">
		{#snippet trigger({ props })}
			{#if canCheckUpdate}
				{@const triggerProps = mergeProps(props, {
					onclick: checkImageUpdate,
					class:
						'relative touch-hit mr-2 inline-flex min-h-4 min-w-4 items-center gap-1.5 rounded-full text-left whitespace-nowrap text-muted-foreground transition-colors hover:text-info disabled:cursor-not-allowed'
				})}
				<button {...triggerProps} disabled={isChecking} data-testid="image-update-trigger">
					{#if isChecking}
						<Spinner tone="info" class="size-3 shrink-0" />
					{:else}
						<UncheckedRingIcon />
					{/if}
					{@render statusText()}
				</button>
			{:else}
				<span {...props} class={statusTriggerClass} data-testid="image-update-trigger">
					<div class="flex size-4 shrink-0 items-center justify-center text-muted-foreground opacity-30" aria-hidden="true">
						<UncheckedRingIcon />
					</div>
					{@render statusText()}
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

{#if releaseNotesTarget}
	<ImageReleaseNotesDialog
		bind:open={releaseNotesOpen}
		{environmentId}
		imageRef={releaseNotesTarget.imageRef}
		version={releaseNotesTarget.version}
		digest={releaseNotesTarget.digest}
	/>
{/if}

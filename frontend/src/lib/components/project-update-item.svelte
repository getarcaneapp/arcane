<script lang="ts">
	import { mergeProps } from 'bits-ui';
	import type { Component } from 'svelte';

	import ImageUpdateItem from '#lib/components/image-update-item.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import UncheckedRingIcon from '#lib/components/unchecked-ring-icon.svelte';
	import UpdateStatusBanner from '#lib/components/update-status-banner.svelte';
	import UpdateStatusPopover from '#lib/components/update-status-popover.svelte';
	import {
		AlertIcon,
		ArrowRightIcon,
		CircleArrowUpIcon,
		ClockIcon,
		DownloadIcon,
		ImagesIcon,
		RefreshIcon,
		VerifiedCheckIcon
	} from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { ProjectUpdateInfo } from '#lib/types/swarm.js';
	import { getProjectUpdateStatus, getProjectUpdateText, parseImageRef } from '#lib/utils/docker.js';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';

	interface Props {
		updateInfo?: ProjectUpdateInfo;
		environmentId: string;
		class?: string;
		onCheck?: () => void | Promise<void>;
		checking?: boolean;
		disabled?: boolean;
	}

	let { updateInfo, environmentId, class: className = '', onCheck, checking = false, disabled = false }: Props = $props();
	let isOpen = $state(false);

	const status = $derived(getProjectUpdateStatus(updateInfo));
	const indicatorLabel = $derived(checking ? m.common_action_checking() : getProjectUpdateText(updateInfo));
	const imageCount = $derived(updateInfo?.imageCount ?? 0);
	const checkedImageCount = $derived(updateInfo?.checkedImageCount ?? 0);
	const errorCount = $derived(updateInfo?.errorCount ?? 0);
	const errorMessage = $derived(updateInfo?.errorMessage?.trim() || '');
	const imageRefs = $derived(updateInfo?.imageRefs ?? []);
	const updatedImageRefs = $derived(updateInfo?.updatedImageRefs ?? []);
	const notPulledImageRefs = $derived(updateInfo?.notPulledImageRefs ?? []);
	const updateInfoByRef = $derived(updateInfo?.updateInfoByRef ?? {});
	const serviceUpdates = $derived(Object.entries(updateInfo?.serviceUpdates ?? {}));
	const fallbackUpdatedImageRefs = $derived(
		updatedImageRefs.filter((ref) => !serviceUpdates.some(([, service]) => service.imageRef === ref))
	);
	const canCheck = $derived(!!onCheck && !disabled && (imageRefs.length > 0 || serviceUpdates.length > 0));
	const directCheckFromTrigger = $derived(canCheck && (status === 'unknown' || status === 'error'));

	const summaryText = $derived.by(() => {
		if (imageCount <= 0) return null;
		return `${checkedImageCount} / ${imageCount} ${String(m.images()).toLowerCase()}`;
	});

	const updatedSummaryText = $derived.by(() => {
		if (imageCount <= 0) return null;
		return m.images_updates_count({ updated: updatedImageRefs.length, total: imageCount });
	});

	const notPulledSummaryText = $derived.by(() => {
		if (imageCount <= 0) return null;
		return m.images_not_pulled_count({ notPulled: notPulledImageRefs.length, total: imageCount });
	});

	const lastCheckedAtLabel = $derived.by(() => {
		if (!updateInfo?.lastCheckedAt) return null;
		return formatDateTimeShort(updateInfo.lastCheckedAt) || null;
	});

	const stateMeta = $derived.by(
		(): {
			icon: Component;
			tileClass: string;
			titleClass: string;
			title: string;
			description: string;
		} => {
			switch (status) {
				case 'has_update':
					return {
						icon: CircleArrowUpIcon,
						tileClass: 'text-info',
						titleClass: 'text-info',
						title: m.images_has_updates(),
						description: updatedSummaryText ?? m.images_has_updates()
					};
				case 'not_pulled':
					return {
						icon: DownloadIcon,
						tileClass: 'text-info',
						titleClass: 'text-info',
						title: m.image_update_not_pulled_title(),
						description: notPulledSummaryText ?? m.image_update_not_pulled_desc()
					};
				case 'up_to_date':
					return {
						icon: VerifiedCheckIcon,
						tileClass: 'text-success',
						titleClass: 'text-success',
						title: m.image_update_up_to_date_title(),
						description: m.image_update_up_to_date_desc()
					};
				case 'error':
					return {
						icon: AlertIcon,
						tileClass: 'text-destructive',
						titleClass: 'text-destructive',
						title: m.image_update_check_failed_title(),
						description: errorMessage || m.image_update_could_not_query_registry()
					};
				default:
					return {
						icon: AlertIcon,
						tileClass: 'text-muted-foreground',
						titleClass: 'text-sidebar-foreground',
						title: m.image_update_status_unknown(),
						description: canCheck ? m.image_update_click_to_check() : m.image_update_unable_check_tags()
					};
			}
		}
	);

	async function handleCheckClick(event?: MouseEvent) {
		event?.preventDefault();
		event?.stopPropagation();
		if (!canCheck || checking || disabled) {
			return;
		}

		isOpen = false;
		await onCheck?.();
	}
</script>

{#snippet sectionLabel(label: string)}
	<div class="px-2.5 pt-1 pb-1 text-xs text-muted-foreground">{label}</div>
{/snippet}

{#snippet refRow(imageRef: string, selected: boolean)}
	{@const parsed = parseImageRef(imageRef)}
	<div class={['flex items-center rounded-lg px-2.5 py-1.5', selected && 'bg-sidebar-accent']}>
		<ImageUpdateItem updateInfo={updateInfoByRef[imageRef]} {imageRef} repo={parsed.repo} tag={parsed.tag} {environmentId} />
		<span class="min-w-0 flex-1 font-mono text-xs break-all">{imageRef}</span>
	</div>
{/snippet}

{#snippet recheckButton()}
	{#if canCheck}
		<div class="px-2 pb-2">
			<button
				onclick={handleCheckClick}
				disabled={checking}
				class="group flex h-9 w-full items-center justify-center gap-2 rounded-lg bg-sidebar-accent text-xs font-medium text-sidebar-accent-foreground transition-colors hover:bg-sidebar-accent/80 disabled:cursor-not-allowed disabled:opacity-50"
			>
				{#if checking}
					<Spinner class="size-3.5" />
					{m.common_action_checking()}
				{:else}
					<RefreshIcon class="size-3.5 transition-transform group-hover:rotate-45" />
					{m.image_update_recheck_button()}
				{/if}
			</button>
		</div>
	{/if}
{/snippet}

<UpdateStatusPopover bind:open={isOpen} interactive={canCheck} directTrigger={directCheckFromTrigger} contentWidth="xl">
	{#snippet trigger({ props })}
		{#if checking}
			<span
				{...props}
				class="inline-flex size-4 items-center justify-center align-middle {className}"
				aria-label={indicatorLabel}
				data-testid="project-update-trigger"
			>
				<Spinner tone="info" class="size-4" />
			</span>
		{:else if directCheckFromTrigger}
			{@const triggerProps = mergeProps(props, {
				onclick: handleCheckClick,
				class: `group inline-flex size-4 items-center justify-center align-middle transition-colors disabled:cursor-not-allowed dark:hover:bg-info/10 ${className}`
			})}
			<button
				{...triggerProps}
				disabled={checking}
				aria-label={m.image_update_recheck_button()}
				title={m.image_update_recheck_button()}
				data-testid="project-update-trigger"
			>
				{#if status === 'error'}
					<AlertIcon class="size-4 text-destructive transition-colors group-hover:text-info" />
				{:else}
					<span class="flex size-4 items-center justify-center text-muted-foreground transition-colors group-hover:text-info">
						<UncheckedRingIcon />
					</span>
				{/if}
			</button>
		{:else}
			<span
				{...props}
				class="inline-flex size-4 items-center justify-center align-middle {className}"
				aria-label={indicatorLabel}
				data-testid="project-update-trigger"
			>
				{#if status === 'error'}
					<AlertIcon class="size-4 text-destructive" />
				{:else if status === 'up_to_date'}
					<VerifiedCheckIcon class="size-4 text-success" />
				{:else if status === 'has_update'}
					<CircleArrowUpIcon class="size-4 text-info" />
				{:else if status === 'not_pulled'}
					<DownloadIcon class="size-4 text-info" />
				{:else}
					<div class="flex size-4 items-center justify-center text-muted-foreground opacity-60">
						<UncheckedRingIcon />
					</div>
				{/if}
			</span>
		{/if}
	{/snippet}

	{#snippet content()}
		<div class="overflow-hidden rounded-xl bg-sidebar text-sidebar-foreground">
			{#if checking}
				<UpdateStatusBanner
					icon={Spinner}
					tileClass="text-info"
					titleClass="text-info"
					title={m.image_update_checking_title()}
					description={m.image_update_querying_registry()}
				/>
			{:else}
				<UpdateStatusBanner
					icon={stateMeta.icon}
					tileClass={stateMeta.tileClass}
					titleClass={stateMeta.titleClass}
					title={stateMeta.title}
					description={stateMeta.description}
				/>
				<div class="flex flex-col gap-0.5 p-2">
					{#if serviceUpdates.length > 0}
						{@render sectionLabel(m.services())}
						<div class="flex max-h-60 flex-col gap-0.5 overflow-auto">
							{#each serviceUpdates as [serviceName, service] (serviceName)}
								{@const parsed = parseImageRef(service.imageRef)}
								<div class={['rounded-lg px-2.5 py-2', service.updateInfo?.hasUpdate && 'bg-sidebar-accent']}>
									<div class="flex items-center">
										<ImageUpdateItem
											updateInfo={service.updateInfo ?? undefined}
											repo={parsed.repo}
											tag={parsed.tag}
											{environmentId}
										/>
										<span class="min-w-0 flex-1 text-xs font-medium break-all">{serviceName}</span>
									</div>
									<div class="mt-0.5 pl-6 font-mono text-2xs break-all text-muted-foreground">{service.imageRef}</div>
									{#if service.updateInfo?.hasUpdate && service.updateInfo.updateType === 'tag' && service.updateInfo.latestVersion}
										<div class="mt-0.5 flex flex-wrap items-center gap-1 pl-6 font-mono text-2xs break-all">
											<span>{parsed.tag}</span>
											<ArrowRightIcon class="size-3 shrink-0" />
											<span>{service.updateInfo.latestVersion}</span>
										</div>
									{/if}
								</div>
							{/each}
						</div>
					{/if}

					{#if status === 'has_update' && fallbackUpdatedImageRefs.length > 0}
						{@render sectionLabel(m.images_has_updates())}
						<div class="flex max-h-40 flex-col gap-0.5 overflow-auto">
							{#each fallbackUpdatedImageRefs as imageRef (imageRef)}
								{@render refRow(imageRef, true)}
							{/each}
						</div>
					{:else if status === 'not_pulled'}
						<div class="px-2.5 text-xs leading-relaxed text-muted-foreground">{m.image_update_not_pulled_desc()}</div>
					{/if}

					{#if notPulledImageRefs.length > 0 && (status === 'not_pulled' || status === 'has_update')}
						{@render sectionLabel(m.image_update_not_pulled_title())}
						<div class="flex max-h-40 flex-col gap-0.5 overflow-auto">
							{#each notPulledImageRefs as imageRef (imageRef)}
								{@render refRow(imageRef, false)}
							{/each}
						</div>
					{/if}

					{#if summaryText || (errorCount > 0 && status !== 'error') || lastCheckedAtLabel}
						<div class="flex flex-wrap items-center gap-x-3 gap-y-1 px-2.5 pt-1.5 text-2xs text-muted-foreground">
							{#if summaryText}
								<span class="inline-flex items-center gap-1.5">
									<ImagesIcon class="size-3" />
									{summaryText}
								</span>
							{/if}
							{#if errorCount > 0 && status !== 'error'}
								<span class="inline-flex items-center gap-1.5">
									<AlertIcon class="size-3 text-destructive" />
									{errorCount}
									{m.common_error()}
								</span>
							{/if}
							{#if lastCheckedAtLabel}
								<span class="inline-flex items-center gap-1.5">
									<ClockIcon class="size-3" />
									{lastCheckedAtLabel}
								</span>
							{/if}
						</div>
					{/if}
				</div>
				{@render recheckButton()}
			{/if}
		</div>
	{/snippet}
</UpdateStatusPopover>

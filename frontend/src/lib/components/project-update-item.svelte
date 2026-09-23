<script lang="ts">
	import type { ProjectUpdateInfo } from '#lib/types/swarm.js';
	import { getProjectUpdateStatus, getProjectUpdateText, parseImageRef } from '#lib/utils/docker.js';
	import { m } from '#lib/paraglide/messages.js';
	import UpdateStatusPopover from '#lib/components/update-status-popover.svelte';
	import UpdateStatusBanner from '#lib/components/update-status-banner.svelte';
	import ImageUpdateItem from '#lib/components/image-update-item.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
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
	import type { Component } from 'svelte';
	import { formatDateTimeShort } from '#lib/utils/formatting.js';
	import UncheckedRingIcon from '#lib/components/unchecked-ring-icon.svelte';
	import { mergeProps } from 'bits-ui';

	interface Props {
		updateInfo?: ProjectUpdateInfo;
		class?: string;
		onCheck?: () => void | Promise<void>;
		checking?: boolean;
		disabled?: boolean;
	}

	let { updateInfo, class: className = '', onCheck, checking = false, disabled = false }: Props = $props();
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
			gradientFrom: string;
			gradientTo: string;
			shadowColor: string;
			headerClass: string;
			titleClass: string;
			descriptionClass: string;
			title: string;
			description: string;
		} => {
			switch (status) {
				case 'has_update':
					return {
						icon: CircleArrowUpIcon,
						gradientFrom: 'from-info',
						gradientTo: 'to-cyan',
						shadowColor: 'shadow-info/25',
						headerClass: 'bg-linear-to-br from-info/10 to-cyan/5',
						titleClass: 'text-info',
						descriptionClass: 'text-info/80',
						title: m.images_has_updates(),
						description: updatedSummaryText ?? m.images_has_updates()
					};
				case 'not_pulled':
					return {
						icon: DownloadIcon,
						gradientFrom: 'from-info',
						gradientTo: 'to-cyan',
						shadowColor: 'shadow-info/25',
						headerClass: 'bg-linear-to-br from-info/10 to-cyan/5',
						titleClass: 'text-info',
						descriptionClass: 'text-info/80',
						title: m.image_update_not_pulled_title(),
						description: notPulledSummaryText ?? m.image_update_not_pulled_desc()
					};
				case 'up_to_date':
					return {
						icon: VerifiedCheckIcon,
						gradientFrom: 'from-success',
						gradientTo: 'to-success',
						shadowColor: 'shadow-success/25',
						headerClass: 'bg-linear-to-br from-success/10 to-success/5',
						titleClass: 'text-success',
						descriptionClass: 'text-success/80',
						title: m.image_update_up_to_date_title(),
						description: m.image_update_up_to_date_desc()
					};
				case 'error':
					return {
						icon: AlertIcon,
						gradientFrom: 'from-rose',
						gradientTo: 'to-destructive',
						shadowColor: 'shadow-destructive/25',
						headerClass: 'bg-linear-to-br from-rose/10 to-destructive/5',
						titleClass: 'text-destructive',
						descriptionClass: 'text-destructive/80',
						title: m.image_update_check_failed_title(),
						description: errorMessage || m.image_update_could_not_query_registry()
					};
				default:
					return {
						icon: AlertIcon,
						gradientFrom: 'from-muted-foreground',
						gradientTo: 'to-muted-foreground',
						shadowColor: 'shadow-muted-foreground/25',
						headerClass: 'bg-linear-to-br from-muted/50 to-muted/20',
						titleClass: 'text-foreground',
						descriptionClass: 'text-muted-foreground',
						title: m.image_update_status_unknown(),
						description: m.image_update_click_to_check()
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

{#snippet iconCircle(Icon: Component, gradientFrom: string, gradientTo: string, shadowColor: string)}
	<div
		class="flex h-10 w-10 items-center justify-center rounded-full bg-linear-to-br {gradientFrom} {gradientTo} shadow-lg {shadowColor}"
	>
		<Icon class="size-5 text-white" />
	</div>
{/snippet}

{#snippet refRow(imageRef: string)}
	{@const parsed = parseImageRef(imageRef)}
	<div class="flex items-center rounded-md bg-muted px-2 py-1">
		<ImageUpdateItem updateInfo={updateInfoByRef[imageRef]} {imageRef} repo={parsed.repo} tag={parsed.tag} />
		<span class="min-w-0 flex-1 font-mono text-xs break-all text-foreground">{imageRef}</span>
	</div>
{/snippet}

{#snippet recheckButton()}
	{#if canCheck}
		<div class="border-t border-border/50 bg-muted/50 p-3">
			<button
				onclick={handleCheckClick}
				disabled={checking}
				class="group flex w-full items-center justify-center gap-2 rounded-lg bg-secondary/80 px-3 py-2 text-xs font-medium text-secondary-foreground shadow-sm transition-all hover:bg-secondary hover:shadow-md disabled:cursor-not-allowed disabled:opacity-50"
			>
				{#if checking}
					<Spinner class="size-3" />
					{m.common_action_checking()}
				{:else}
					<RefreshIcon class="size-3 transition-transform group-hover:rotate-45" />
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
		<div class="overflow-hidden rounded-xl">
			{#if checking}
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
			{:else}
				<div class="p-4 {stateMeta.headerClass}">
					<div class="flex items-start gap-3">
						{@render iconCircle(stateMeta.icon, stateMeta.gradientFrom, stateMeta.gradientTo, stateMeta.shadowColor)}
						<div class="flex-1">
							<div class="text-sm font-semibold {stateMeta.titleClass}">{stateMeta.title}</div>
							<div class="text-xs {stateMeta.descriptionClass}">{stateMeta.description}</div>
						</div>
					</div>
				</div>
				<div class="bg-transparent p-4">
					<div class="space-y-3">
						{#if summaryText}
							<div class="flex items-center gap-2 text-xs text-muted-foreground">
								<ImagesIcon class="size-3.5" />
								<span>{summaryText}</span>
							</div>
						{/if}

						{#if serviceUpdates.length > 0}
							<div class="space-y-2">
								<div class="text-2xs font-medium tracking-wide text-foreground uppercase">{m.services()}</div>
								<div class="max-h-60 space-y-1 overflow-auto">
									{#each serviceUpdates as [serviceName, service] (serviceName)}
										{@const parsed = parseImageRef(service.imageRef)}
										<div class="rounded-md bg-muted px-2 py-1.5">
											<div class="flex items-center gap-2">
												<ImageUpdateItem updateInfo={service.updateInfo ?? undefined} repo={parsed.repo} tag={parsed.tag} />
												<span class="min-w-0 flex-1 text-xs font-medium break-all">{serviceName}</span>
											</div>
											<div class="mt-1 font-mono text-2xs break-all text-muted-foreground">{service.imageRef}</div>
											{#if service.updateInfo?.hasUpdate && service.updateInfo.updateType === 'tag' && service.updateInfo.latestVersion}
												<div class="mt-1 flex flex-wrap items-center gap-1 font-mono text-xs break-all">
													<span>{parsed.tag}</span>
													<ArrowRightIcon class="size-3 shrink-0" />
													<span>{service.updateInfo.latestVersion}</span>
												</div>
											{/if}
										</div>
									{/each}
								</div>
							</div>
						{/if}

						{#if status === 'has_update' && fallbackUpdatedImageRefs.length > 0}
							<div class="space-y-2">
								<div class="text-2xs font-medium tracking-wide text-foreground uppercase">{m.images_has_updates()}</div>
								<div class="max-h-40 space-y-1 overflow-auto">
									{#each fallbackUpdatedImageRefs as imageRef (imageRef)}
										{@render refRow(imageRef)}
									{/each}
								</div>
							</div>
						{:else if status === 'up_to_date'}
							<div class="text-xs leading-relaxed text-muted-foreground">{m.image_update_up_to_date_desc()}</div>
						{:else if status === 'not_pulled'}
							<div class="text-xs leading-relaxed text-muted-foreground">{m.image_update_not_pulled_desc()}</div>
						{:else if status === 'error'}
							<div class="text-xs leading-relaxed text-muted-foreground">
								{errorMessage || m.image_update_could_not_query_registry()}
							</div>
						{:else if status === 'unknown'}
							<div class="text-xs leading-relaxed text-muted-foreground">
								{#if canCheck}
									{m.image_update_click_to_check()}
								{:else}
									{m.image_update_unable_check_tags()}
								{/if}
							</div>
						{/if}

						{#if notPulledImageRefs.length > 0 && (status === 'not_pulled' || status === 'has_update')}
							<div class="space-y-2">
								<div class="text-2xs font-medium tracking-wide text-foreground uppercase">
									{m.image_update_not_pulled_title()}
								</div>
								<div class="max-h-40 space-y-1 overflow-auto">
									{#each notPulledImageRefs as imageRef (imageRef)}
										{@render refRow(imageRef)}
									{/each}
								</div>
							</div>
						{/if}

						{#if errorCount > 0 && status !== 'error'}
							<div class="flex items-center gap-2 text-xs text-muted-foreground">
								<AlertIcon class="size-3.5 text-destructive" />
								<span>{errorCount} {m.common_error()}</span>
							</div>
						{/if}

						{#if lastCheckedAtLabel}
							<div class="flex items-center gap-2 text-xs text-muted-foreground">
								<ClockIcon class="size-3.5" />
								<span>{lastCheckedAtLabel}</span>
							</div>
						{/if}
					</div>
				</div>
				{@render recheckButton()}
			{/if}
		</div>
	{/snippet}
</UpdateStatusPopover>

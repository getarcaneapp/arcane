<script lang="ts">
	import type { ProjectUpdateInfo } from '#lib/types/swarm';
	import { getProjectUpdateStatus, getProjectUpdateText, parseImageRef } from '#lib/utils/docker';
	import { m } from '#lib/paraglide/messages';
	import UpdateStatusPopover from '#lib/components/update-status-popover.svelte';
	import UpdateStatusBanner from '#lib/components/update-status-banner.svelte';
	import ImageUpdateItem from '#lib/components/image-update-item.svelte';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { AlertIcon, CircleArrowUpIcon, ClockIcon, DownloadIcon, ImagesIcon, RefreshIcon, VerifiedCheckIcon } from '#lib/icons';
	import type { Component } from 'svelte';
	import { formatDateTimeShort } from '#lib/utils/formatting';
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
	const canCheck = $derived(!!onCheck && !disabled && imageRefs.length > 0);
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
		const parsed = new Date(updateInfo.lastCheckedAt);
		if (Number.isNaN(parsed.getTime())) return null;
		return formatDateTimeShort(parsed);
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
						gradientFrom: 'from-blue-500',
						gradientTo: 'to-cyan-500',
						shadowColor: 'shadow-blue-500/25',
						headerClass: 'bg-linear-to-br from-blue-50 to-cyan-50/30 dark:from-blue-950/20 dark:to-cyan-950/10',
						titleClass: 'text-blue-950 dark:text-blue-100',
						descriptionClass: 'text-blue-900/80 dark:text-blue-300/80',
						title: m.images_has_updates(),
						description: updatedSummaryText ?? m.images_has_updates()
					};
				case 'not_pulled':
					return {
						icon: DownloadIcon,
						gradientFrom: 'from-blue-500',
						gradientTo: 'to-cyan-500',
						shadowColor: 'shadow-blue-500/25',
						headerClass: 'bg-linear-to-br from-blue-50 to-cyan-50/30 dark:from-blue-950/20 dark:to-cyan-950/10',
						titleClass: 'text-blue-950 dark:text-blue-100',
						descriptionClass: 'text-blue-900/80 dark:text-blue-300/80',
						title: m.image_update_not_pulled_title(),
						description: notPulledSummaryText ?? m.image_update_not_pulled_desc()
					};
				case 'up_to_date':
					return {
						icon: VerifiedCheckIcon,
						gradientFrom: 'from-emerald-500',
						gradientTo: 'to-green-500',
						shadowColor: 'shadow-emerald-500/25',
						headerClass: 'bg-linear-to-br from-emerald-50 to-green-50/30 dark:from-emerald-950/20 dark:to-green-950/10',
						titleClass: 'text-emerald-950 dark:text-emerald-100',
						descriptionClass: 'text-emerald-900/80 dark:text-emerald-300/80',
						title: m.image_update_up_to_date_title(),
						description: m.image_update_up_to_date_desc()
					};
				case 'error':
					return {
						icon: AlertIcon,
						gradientFrom: 'from-rose-500',
						gradientTo: 'to-red-500',
						shadowColor: 'shadow-red-500/25',
						headerClass: 'bg-linear-to-br from-rose-50 to-red-50/40 dark:from-rose-950/20 dark:to-red-950/10',
						titleClass: 'text-red-950 dark:text-red-100',
						descriptionClass: 'text-red-900/80 dark:text-red-300/80',
						title: m.image_update_check_failed_title(),
						description: errorMessage || m.image_update_could_not_query_registry()
					};
				default:
					return {
						icon: AlertIcon,
						gradientFrom: 'from-gray-400',
						gradientTo: 'to-slate-500',
						shadowColor: 'shadow-gray-400/25',
						headerClass: 'bg-linear-to-br from-gray-50 to-slate-50/30 dark:from-gray-900/20 dark:to-slate-900/10',
						titleClass: 'text-gray-950 dark:text-gray-100',
						descriptionClass: 'text-gray-800 dark:text-gray-300/80',
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

<UpdateStatusPopover
	bind:open={isOpen}
	interactive={canCheck}
	directTrigger={directCheckFromTrigger}
	contentClass="max-w-[320px] p-0"
>
	{#snippet trigger({ props })}
		{#if checking}
			<span
				{...props}
				class="inline-flex size-4 items-center justify-center align-middle {className}"
				aria-label={indicatorLabel}
				data-testid="project-update-trigger"
			>
				<Spinner class="size-4 text-blue-400" />
			</span>
		{:else if directCheckFromTrigger}
			{@const triggerProps = mergeProps(props, {
				onclick: handleCheckClick,
				class: `group inline-flex size-4 items-center justify-center align-middle transition-colors disabled:cursor-not-allowed dark:hover:bg-blue-950 ${className}`
			})}
			<button
				{...triggerProps}
				disabled={checking}
				aria-label={m.image_update_recheck_button()}
				title={m.image_update_recheck_button()}
				data-testid="project-update-trigger"
			>
				{#if status === 'error'}
					<AlertIcon class="size-4 text-red-500 transition-colors group-hover:text-blue-400" />
				{:else}
					<span class="flex size-4 items-center justify-center text-gray-400 transition-colors group-hover:text-blue-400">
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
					<AlertIcon class="size-4 text-red-500" />
				{:else if status === 'up_to_date'}
					<VerifiedCheckIcon class="size-4 text-green-500" />
				{:else if status === 'has_update'}
					<CircleArrowUpIcon class="size-4 text-blue-500" />
				{:else if status === 'not_pulled'}
					<DownloadIcon class="size-4 text-blue-500" />
				{:else}
					<div class="flex size-4 items-center justify-center text-gray-400 opacity-60">
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
					wrapperClass="bg-linear-to-br from-blue-50 to-cyan-50/30 p-4 dark:from-blue-950/20 dark:to-cyan-950/10"
					gradientFrom="from-blue-500"
					gradientTo="to-cyan-500"
					shadowColor="shadow-blue-500/25"
					titleClass="text-blue-950 dark:text-blue-100"
					descriptionClass="text-blue-900/80 dark:text-blue-300/80"
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

						{#if status === 'has_update' && updatedImageRefs.length > 0}
							<div class="space-y-2">
								<div class="text-[11px] font-medium tracking-wide text-foreground uppercase">{m.images_has_updates()}</div>
								<div class="max-h-40 space-y-1 overflow-auto">
									{#each updatedImageRefs as imageRef (imageRef)}
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
						{:else}
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
								<div class="text-[11px] font-medium tracking-wide text-foreground uppercase">
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
								<AlertIcon class="size-3.5 text-red-500" />
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

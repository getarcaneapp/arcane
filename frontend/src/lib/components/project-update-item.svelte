<script lang="ts">
	import type { ProjectUpdateInfo } from '$lib/types/project.type';
	import { getProjectUpdateStatus, getProjectUpdateText } from '$lib/utils/project-update.util';
	import { m } from '$lib/paraglide/messages';
	import UpdateStatusPopover from '$lib/components/update-status-popover.svelte';
	import { Spinner } from '$lib/components/ui/spinner/index.js';
	import { AlertIcon, CircleArrowUpIcon, ClockIcon, ImagesIcon, RefreshIcon, VerifiedCheckIcon } from '$lib/icons';
	import type { Component } from 'svelte';
	import { format } from 'date-fns';

	interface Props {
		updateInfo?: ProjectUpdateInfo;
		class?: string;
		onCheck?: () => void | Promise<void>;
		checking?: boolean;
	}

	let { updateInfo, class: className = '', onCheck, checking = false }: Props = $props();
	let isOpen = $state(false);

	const status = $derived(getProjectUpdateStatus(updateInfo));
	const indicatorLabel = $derived(checking ? m.common_action_checking() : getProjectUpdateText(updateInfo));
	const imageCount = $derived(updateInfo?.imageCount ?? 0);
	const checkedImageCount = $derived(updateInfo?.checkedImageCount ?? 0);
	const errorCount = $derived(updateInfo?.errorCount ?? 0);
	const errorMessage = $derived(updateInfo?.errorMessage?.trim() || '');
	const imageRefs = $derived(updateInfo?.imageRefs ?? []);
	const updatedImageRefs = $derived(updateInfo?.updatedImageRefs ?? []);
	const canCheck = $derived(!!onCheck && imageRefs.length > 0);
	const directCheckFromTrigger = $derived(canCheck && (status === 'unknown' || status === 'error'));

	const summaryText = $derived.by(() => {
		if (imageCount <= 0) return null;
		return `${checkedImageCount} / ${imageCount} ${String(m.images_title()).toLowerCase()}`;
	});

	const lastCheckedAtLabel = $derived.by(() => {
		if (!updateInfo?.lastCheckedAt) return null;
		const parsed = new Date(updateInfo.lastCheckedAt);
		if (Number.isNaN(parsed.getTime())) return null;
		return format(parsed, 'PP p');
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
						description: summaryText ?? m.images_has_updates()
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
		if (!canCheck || checking) {
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

{#snippet recheckButton()}
	{#if canCheck}
		<div class="border-border/50 bg-muted/50 border-t p-3">
			<button
				onclick={handleCheckClick}
				disabled={checking}
				class="group bg-secondary/80 text-secondary-foreground hover:bg-secondary flex w-full items-center justify-center gap-2 rounded-lg px-3 py-2 text-xs font-medium shadow-sm transition-all hover:shadow-md disabled:cursor-not-allowed disabled:opacity-50"
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

<UpdateStatusPopover bind:open={isOpen} interactive={canCheck} contentClass="max-w-[320px] p-0">
	{#snippet trigger()}
		{#if checking}
			<span
				class="inline-flex size-4 items-center justify-center align-middle {className}"
				aria-label={indicatorLabel}
				data-testid="project-update-trigger"
			>
				<Spinner class="size-4 text-blue-400" />
			</span>
		{:else if directCheckFromTrigger}
			<span
				class="inline-flex size-4 items-center justify-center align-middle {className}"
				aria-label={indicatorLabel}
				data-testid="project-update-trigger"
			>
				<button
					onclick={handleCheckClick}
					disabled={checking}
					aria-label={m.image_update_recheck_button()}
					title={m.image_update_recheck_button()}
					class="group flex h-4 w-4 items-center justify-center rounded-full transition-colors disabled:cursor-not-allowed dark:hover:bg-blue-950"
				>
					{#if status === 'error'}
						<AlertIcon class="size-4 text-red-500 transition-colors group-hover:text-blue-400" />
					{:else}
						<div
							class="flex h-4 w-4 items-center justify-center rounded-full border-2 border-dashed border-gray-400 transition-colors group-hover:border-blue-400 group-hover:bg-blue-50"
						>
							<div class="h-1.5 w-1.5 rounded-full bg-gray-400 transition-colors group-hover:bg-blue-400"></div>
						</div>
					{/if}
				</button>
			</span>
		{:else}
			<span
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
				{:else}
					<div class="flex h-4 w-4 items-center justify-center rounded-full border-2 border-dashed border-gray-400 opacity-60">
						<div class="h-1.5 w-1.5 rounded-full bg-gray-400"></div>
					</div>
				{/if}
			</span>
		{/if}
	{/snippet}

	{#snippet content()}
		<div class="overflow-hidden rounded-xl">
			{#if checking}
				<div class="bg-linear-to-br from-blue-50 to-cyan-50/30 p-4 dark:from-blue-950/20 dark:to-cyan-950/10">
					<div class="flex items-center gap-3">
						{@render iconCircle(Spinner, 'from-blue-500', 'to-cyan-500', 'shadow-blue-500/25')}
						<div>
							<div class="text-sm font-semibold text-blue-950 dark:text-blue-100">{m.image_update_checking_title()}</div>
							<div class="text-xs text-blue-900/80 dark:text-blue-300/80">{m.image_update_querying_registry()}</div>
						</div>
					</div>
				</div>
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
							<div class="text-muted-foreground flex items-center gap-2 text-xs">
								<ImagesIcon class="size-3.5" />
								<span>{summaryText}</span>
							</div>
						{/if}

						{#if status === 'has_update' && updatedImageRefs.length > 0}
							<div class="space-y-2">
								<div class="text-foreground text-[11px] font-medium tracking-wide uppercase">{m.images_has_updates()}</div>
								<div class="max-h-40 space-y-1 overflow-auto">
									{#each updatedImageRefs as imageRef}
										<div class="bg-muted text-foreground rounded-md px-2 py-1 font-mono text-xs break-all">
											{imageRef}
										</div>
									{/each}
								</div>
							</div>
						{:else if status === 'up_to_date'}
							<div class="text-muted-foreground text-xs leading-relaxed">{m.image_update_up_to_date_desc()}</div>
						{:else if status === 'error'}
							<div class="text-muted-foreground text-xs leading-relaxed">
								{errorMessage || m.image_update_could_not_query_registry()}
							</div>
						{:else}
							<div class="text-muted-foreground text-xs leading-relaxed">
								{#if canCheck}
									{m.image_update_click_to_check()}
								{:else}
									{m.image_update_unable_check_tags()}
								{/if}
							</div>
						{/if}

						{#if errorCount > 0 && status !== 'error'}
							<div class="text-muted-foreground flex items-center gap-2 text-xs">
								<AlertIcon class="size-3.5 text-red-500" />
								<span>{errorCount} {m.common_error()}</span>
							</div>
						{/if}

						{#if lastCheckedAtLabel}
							<div class="text-muted-foreground flex items-center gap-2 text-xs">
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

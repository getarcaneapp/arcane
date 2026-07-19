<script lang="ts">
	import { onMount } from 'svelte';
	import ResponsiveDialog from '$lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import ActivityListItem from './activity-list-item.svelte';
	import ActivityBatchItem from './activity-batch-item.svelte';
	import ActivityDetailPanel from './activity-detail-panel.svelte';
	import ActivityFilterPopover from './activity-filter-popover.svelte';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import type { Activity, ActivityGroup } from '$lib/types/activity.type';
	import { ActivityIcon, AlertTriangleIcon, CloseIcon, EllipsisIcon, RefreshIcon, SearchIcon, TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { confirmCancelActivity } from './activity-cancel';
	import { activityCompletionToastsEnabled } from './activity-completion-toasts';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	onMount(() => {
		void activityStore.start();
		return () => activityStore.stop();
	});

	function handleOpenChangeInternal(open: boolean) {
		activityStore.setOpen(open);
	}

	function clearHistoryInternal() {
		openConfirmDialog({
			title: m.activity_clear_history_title(),
			message: m.activity_clear_history_message(),
			confirm: {
				label: m.activity_clear_history_confirm(),
				destructive: true,
				action: async () => {
					try {
						const result = await activityStore.clearHistory();
						if (result.failed.length > 0) {
							toast.warning(
								m.activity_clear_history_partial({
									count: result.succeeded,
									environments: result.failed.map((failure) => failure.environmentName).join(', ')
								})
							);
							return;
						}
						toast.success(m.activity_clear_history_success());
					} catch (error) {
						console.error('Failed to clear activity history:', error);
						toast.error(m.activity_clear_history_failed());
					}
				}
			}
		});
	}

	function groupKeyInternal(group: ActivityGroup): string {
		return group.kind === 'batch' ? `batch:${group.batchId}` : group.activity.id;
	}

	const hasNoResults = $derived(
		activityStore.activities.length > 0 &&
			activityStore.hasActiveFilters &&
			activityStore.runningGroups.length === 0 &&
			activityStore.historyGroups.length === 0
	);
</script>

{#snippet activityRow(activity: Activity, child: boolean)}
	{@const expanded = activityStore.isExpanded(activity.id)}
	{@const cancelable = activity.status === 'running' || activity.status === 'queued'}
	<div class="group/activity relative">
		<Collapsible.Root open={expanded} onOpenChange={(open) => activityStore.setActivityExpanded(activity.id, open)}>
			<Collapsible.Trigger
				class="block w-full cursor-pointer appearance-none border-0 bg-transparent p-0 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden focus-visible:ring-inset"
				aria-label={m.activity_center_title()}
			>
				<ActivityListItem {activity} {expanded} {child} />
			</Collapsible.Trigger>
			<Collapsible.Content
				class="overflow-hidden data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-1 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:slide-in-from-top-1"
			>
				<ActivityDetailPanel {activity} />
			</Collapsible.Content>
		</Collapsible.Root>
		{#if cancelable}
			<IfPermitted perm="activities:cancel">
				<button
					type="button"
					onclick={() => confirmCancelActivity(activity.id)}
					disabled={activityStore.isCancelling(activity.id)}
					title={m.activity_cancel()}
					aria-label={m.activity_cancel()}
					class="absolute top-1/2 right-11 z-[var(--arcane-z-raised)] flex size-7 -translate-y-1/2 items-center justify-center rounded-md bg-background/70 text-muted-foreground opacity-0 backdrop-blur-sm transition group-hover/activity:opacity-100 hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden disabled:pointer-events-none disabled:opacity-40"
				>
					<CloseIcon class="size-4" aria-hidden="true" />
				</button>
			</IfPermitted>
		{/if}
	</div>
{/snippet}

{#snippet groupRow(group: ActivityGroup)}
	{#if group.kind === 'single'}
		{@render activityRow(group.activity, false)}
	{:else}
		{@const expanded = activityStore.isBatchExpanded(group.batchId)}
		<Collapsible.Root open={expanded} onOpenChange={(open) => activityStore.setBatchExpanded(group.batchId, open)}>
			<Collapsible.Trigger
				class="block w-full cursor-pointer appearance-none border-0 bg-transparent p-0 text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden focus-visible:ring-inset"
				aria-label={m.activity_center_title()}
			>
				<ActivityBatchItem {group} {expanded} />
			</Collapsible.Trigger>
			<Collapsible.Content
				class="overflow-hidden data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-1 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:slide-in-from-top-1"
			>
				<div class="ml-6 border-l border-border/40">
					{#each group.items as activity (activity.id)}
						{@render activityRow(activity, true)}
					{/each}
				</div>
			</Collapsible.Content>
		</Collapsible.Root>
	{/if}
{/snippet}

{#snippet sectionHeader(title: string, count: number)}
	<div class="flex items-center gap-2 border-b border-border/50 bg-muted/20 px-4 py-1.5">
		<span class="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{title}</span>
		<span class="text-[11px] text-muted-foreground/70 tabular-nums">{count}</span>
	</div>
{/snippet}

<ResponsiveDialog
	open={activityStore.open}
	onOpenChange={handleOpenChangeInternal}
	variant="sheet"
	title={m.activity_center_title()}
	contentClass="w-[min(94vw,760px)] sm:max-w-[760px]"
	class="flex min-h-0 flex-1 flex-col pt-3 pb-0"
>
	<div class="flex flex-wrap items-center gap-2 border-b border-border/60 px-4 py-3">
		<div class="relative min-w-40 flex-1">
			<SearchIcon
				class="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
				aria-hidden="true"
			/>
			<Input
				type="search"
				value={activityStore.searchTerm}
				oninput={(event) => activityStore.setSearchTerm(event.currentTarget.value)}
				placeholder={m.activity_search_placeholder()}
				class="h-8 pl-8 text-xs"
			/>
		</div>

		<div class="flex items-center gap-2">
			{#if activityStore.activeCount > 0}
				<span class="rounded-md bg-primary/10 px-2 py-1 text-xs font-semibold text-primary tabular-nums">
					{m.activity_active_count({ count: activityStore.activeCount })}
				</span>
			{/if}
			<ActivityFilterPopover />
			<button
				type="button"
				onclick={() => activityStore.refresh()}
				title={m.common_refresh()}
				aria-label={m.common_refresh()}
				class="flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden"
			>
				<RefreshIcon class={cn('size-4', activityStore.loading && 'animate-spin')} aria-hidden="true" />
			</button>
			<DropdownMenu.Root>
				<DropdownMenu.Trigger
					title={m.activity_more_options()}
					aria-label={m.activity_more_options()}
					class="flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden"
				>
					<EllipsisIcon class="size-4" aria-hidden="true" />
				</DropdownMenu.Trigger>
				<DropdownMenu.Content align="end" class="w-56">
					<DropdownMenu.CheckboxItem
						checked={activityCompletionToastsEnabled.current}
						onCheckedChange={(checked) => (activityCompletionToastsEnabled.current = checked)}
						closeOnSelect={false}
					>
						{m.activity_completion_toasts_label()}
					</DropdownMenu.CheckboxItem>
					<IfPermitted perm="activities:delete">
						<DropdownMenu.Separator />
						<DropdownMenu.Item onclick={clearHistoryInternal} class="text-destructive data-highlighted:text-destructive">
							<TrashIcon class="size-4" aria-hidden="true" />
							{m.activity_clear_history()}
						</DropdownMenu.Item>
					</IfPermitted>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		</div>
	</div>

	<div class="min-h-[68vh] flex-1 overflow-y-auto">
		{#if activityStore.environmentFailures.length > 0}
			<div class="border-b border-border/60 bg-muted/25 px-4 py-3">
				<div class="flex items-start gap-2 text-xs text-muted-foreground">
					<AlertTriangleIcon class="mt-0.5 size-4 shrink-0 text-amber-500" aria-hidden="true" />
					<div class="min-w-0 flex-1 space-y-1">
						{#each activityStore.environmentFailures as failure (failure.environmentId)}
							<p class="leading-relaxed">
								{m.activity_environment_load_failed({ environment: failure.environmentName })}
							</p>
						{/each}
					</div>
					<button
						type="button"
						onclick={() => activityStore.retryStream()}
						class="shrink-0 text-xs font-medium text-primary underline hover:text-primary/80"
					>
						{m.common_retry()}
					</button>
				</div>
			</div>
		{/if}

		{#if activityStore.loading && activityStore.activities.length === 0}
			<div class="flex min-h-96 items-center justify-center p-6 text-center">
				<div class="space-y-2">
					<ActivityIcon class="mx-auto size-8 animate-pulse text-muted-foreground" aria-hidden="true" />
					<p class="text-sm text-muted-foreground">{m.common_loading()}</p>
				</div>
			</div>
		{:else if activityStore.activities.length === 0}
			<div class="flex min-h-96 items-center justify-center p-6 text-center">
				<div class="max-w-56 space-y-2">
					<ActivityIcon class="mx-auto size-9 text-muted-foreground/50" aria-hidden="true" />
					<h3 class="text-sm font-semibold">{m.activity_empty_title()}</h3>
					<p class="text-xs leading-relaxed text-muted-foreground">{m.activity_empty_description()}</p>
				</div>
			</div>
		{:else if hasNoResults}
			<div class="flex min-h-96 items-center justify-center p-6 text-center">
				<div class="max-w-56 space-y-2">
					<SearchIcon class="mx-auto size-9 text-muted-foreground/50" aria-hidden="true" />
					<h3 class="text-sm font-semibold">{m.activity_no_results_title()}</h3>
					<p class="text-xs leading-relaxed text-muted-foreground">{m.activity_no_results_description()}</p>
					<button
						type="button"
						onclick={() => activityStore.clearFilters()}
						class="text-xs font-medium text-primary underline hover:text-primary/80"
					>
						{m.common_clear_filters()}
					</button>
				</div>
			</div>
		{:else}
			<div>
				{#if activityStore.runningGroups.length > 0}
					{@render sectionHeader(m.activity_section_running(), activityStore.runningGroups.length)}
					{#each activityStore.runningGroups as group (groupKeyInternal(group))}
						{@render groupRow(group)}
					{/each}
				{/if}
				{#if activityStore.historyGroups.length > 0}
					{@render sectionHeader(m.activity_section_history(), activityStore.historyGroups.length)}
					{#each activityStore.historyGroups as group (groupKeyInternal(group))}
						{@render groupRow(group)}
					{/each}
				{/if}
			</div>
		{/if}
	</div>
</ResponsiveDialog>

<script lang="ts">
	import { onMount } from 'svelte';
	import ResponsiveDialog from '$lib/components/ui/responsive-dialog/responsive-dialog.svelte';
	import * as Collapsible from '$lib/components/ui/collapsible/index.js';
	import ActivityListItem from './activity-list-item.svelte';
	import ActivityDetailPanel from './activity-detail-panel.svelte';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import type { ActivityFilter } from '$lib/types/activity.type';
	import { ActivityIcon, CloseIcon, RefreshIcon, TrashIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { activityFilterLabel } from './activity-labels';
	import { confirmCancelActivity } from './activity-cancel';
	import { openConfirmDialog } from '$lib/components/confirm-dialog';
	import { toast } from 'svelte-sonner';
	import IfPermitted from '$lib/components/if-permitted.svelte';

	const filters: ActivityFilter[] = ['running', 'failed', 'completed'];

	onMount(() => {
		void activityStore.start();
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
						await activityStore.clearHistory();
						toast.success(m.activity_clear_history_success());
					} catch (error) {
						console.error('Failed to clear activity history:', error);
						toast.error(m.activity_clear_history_failed());
					}
				}
			}
		});
	}
</script>

<ResponsiveDialog
	open={activityStore.open}
	onOpenChange={handleOpenChangeInternal}
	variant="sheet"
	title={m.activity_center_title()}
	contentClass="w-[min(94vw,760px)] sm:max-w-[760px]"
	class="flex min-h-0 flex-1 flex-col pt-3 pb-0"
>
	<div class="border-border/60 flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
		<div class="flex min-w-0 items-center gap-2">
			{#if activityStore.activeCount > 0}
				<span class="bg-primary/10 text-primary rounded-md px-2 py-1 text-xs font-semibold tabular-nums">
					{m.activity_active_count({ count: activityStore.activeCount })}
				</span>
			{/if}
		</div>

		<div class="flex items-center gap-2">
			<div class="bg-muted/40 inline-flex rounded-md p-0.5">
				{#each filters as filter (filter)}
					<button
						type="button"
						onclick={() => activityStore.setFilter(filter)}
						class={cn(
							'rounded-sm px-2.5 py-1 text-xs font-medium transition-colors',
							activityStore.filter === filter
								? 'bg-background text-foreground shadow-xs'
								: 'text-muted-foreground hover:text-foreground'
						)}
					>
						{activityFilterLabel(filter)}
					</button>
				{/each}
			</div>
			<button
				type="button"
				onclick={() => activityStore.refresh()}
				title={m.common_refresh()}
				aria-label={m.common_refresh()}
				class="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring flex size-8 items-center justify-center rounded-md transition-colors focus-visible:ring-2 focus-visible:outline-hidden"
			>
				<RefreshIcon class={cn('size-4', activityStore.loading && 'animate-spin')} aria-hidden="true" />
			</button>
			<IfPermitted perm="activities:delete">
				<button
					type="button"
					onclick={clearHistoryInternal}
					title={m.activity_clear_history()}
					aria-label={m.activity_clear_history()}
					class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive focus-visible:ring-ring flex size-8 items-center justify-center rounded-md transition-colors focus-visible:ring-2 focus-visible:outline-hidden"
				>
					<TrashIcon class="size-4" aria-hidden="true" />
				</button>
			</IfPermitted>
		</div>
	</div>

	<div class="min-h-[68vh] flex-1 overflow-y-auto">
		{#if activityStore.loading && activityStore.activities.length === 0}
			<div class="flex min-h-96 items-center justify-center p-6 text-center">
				<div class="space-y-2">
					<ActivityIcon class="text-muted-foreground mx-auto size-8 animate-pulse" aria-hidden="true" />
					<p class="text-muted-foreground text-sm">{m.common_loading()}</p>
				</div>
			</div>
		{:else if activityStore.filteredActivities.length === 0}
			<div class="flex min-h-96 items-center justify-center p-6 text-center">
				<div class="max-w-56 space-y-2">
					<ActivityIcon class="text-muted-foreground/50 mx-auto size-9" aria-hidden="true" />
					<h3 class="text-sm font-semibold">{m.activity_empty_title()}</h3>
					<p class="text-muted-foreground text-xs leading-relaxed">{m.activity_empty_description()}</p>
				</div>
			</div>
		{:else}
			<div>
				{#each activityStore.filteredActivities as activity (activity.id)}
					{@const expanded = activityStore.isExpanded(activity.id)}
					{@const cancelable = activity.status === 'running' || activity.status === 'queued'}
					<div class="group/activity relative">
						<Collapsible.Root open={expanded} onOpenChange={(open) => activityStore.setActivityExpanded(activity.id, open)}>
							<Collapsible.Trigger
								class="focus-visible:ring-ring block w-full cursor-pointer appearance-none border-0 bg-transparent p-0 text-left focus-visible:ring-2 focus-visible:outline-hidden focus-visible:ring-inset"
								aria-label={m.activity_center_title()}
							>
								<ActivityListItem {activity} {expanded} />
							</Collapsible.Trigger>
							<Collapsible.Content
								class="data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=closed]:slide-out-to-top-1 data-[state=open]:slide-in-from-top-1 overflow-hidden"
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
									class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive focus-visible:ring-ring bg-background/70 absolute top-1/2 right-11 z-10 flex size-7 -translate-y-1/2 items-center justify-center rounded-md opacity-0 backdrop-blur-sm transition focus-visible:opacity-100 focus-visible:ring-2 focus-visible:outline-hidden group-hover/activity:opacity-100 disabled:pointer-events-none disabled:opacity-40"
								>
									<CloseIcon class="size-4" aria-hidden="true" />
								</button>
							</IfPermitted>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>
</ResponsiveDialog>

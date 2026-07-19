<script lang="ts">
	import * as Popover from '$lib/components/ui/popover/index.js';
	import { activityStore } from '$lib/stores/activity.store.svelte';
	import type { ActivityStatus, ActivityType } from '$lib/types/activity.type';
	import { CheckIcon, FilterIcon } from '$lib/icons';
	import { m } from '$lib/paraglide/messages';
	import { cn } from '$lib/utils';
	import { activityStatusLabel, activityTypeLabel } from './activity-labels';

	const statuses: ActivityStatus[] = ['queued', 'running', 'success', 'failed', 'cancelled'];
	const types: ActivityType[] = [
		'image_pull',
		'image_build',
		'image_update_check',
		'project_pull',
		'project_build',
		'project_deploy',
		'project_redeploy',
		'project_down',
		'project_restart',
		'project_destroy',
		'container_start',
		'container_stop',
		'container_restart',
		'container_redeploy',
		'container_delete',
		'vulnerability_scan',
		'auto_update',
		'system_prune',
		'resource_action'
	];
</script>

{#snippet filterOption(label: string, selected: boolean, toggle: () => void)}
	<button
		type="button"
		onclick={toggle}
		class="flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs transition-colors hover:bg-muted/60"
	>
		<span
			class={cn(
				'flex size-3.5 shrink-0 items-center justify-center rounded-sm border border-primary',
				selected ? 'bg-primary text-primary-foreground' : 'opacity-50 [&_svg]:invisible'
			)}
		>
			<CheckIcon class="size-3" aria-hidden="true" />
		</span>
		<span class="truncate">{label}</span>
	</button>
{/snippet}

<Popover.Root>
	<Popover.Trigger
		title={m.activity_filters_label()}
		aria-label={m.activity_filters_label()}
		class={cn(
			'relative flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
			activityStore.activeFilterCount > 0 && 'bg-primary/10 text-primary'
		)}
	>
		<FilterIcon class="size-4" aria-hidden="true" />
		{#if activityStore.activeFilterCount > 0}
			<span
				class="absolute -top-1 -right-1 flex size-4 items-center justify-center rounded-full bg-primary text-[10px] font-semibold text-primary-foreground tabular-nums"
			>
				{activityStore.activeFilterCount}
			</span>
		{/if}
	</Popover.Trigger>
	<Popover.Content class="max-h-[min(60vh,480px)] w-56 overflow-y-auto p-2" align="end">
		<p class="px-2 pt-1 pb-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
			{m.activity_filter_status()}
		</p>
		{#each statuses as status (status)}
			{@render filterOption(activityStatusLabel(status), activityStore.statusFilters.includes(status), () =>
				activityStore.toggleStatusFilter(status)
			)}
		{/each}
		<p class="px-2 pt-3 pb-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
			{m.activity_filter_type()}
		</p>
		{#each types as type (type)}
			{@render filterOption(activityTypeLabel(type), activityStore.typeFilters.includes(type), () =>
				activityStore.toggleTypeFilter(type)
			)}
		{/each}
		{#if activityStore.activeFilterCount > 0}
			<div class="mt-2 border-t border-border/50 pt-2">
				<button
					type="button"
					onclick={() => activityStore.clearFilters()}
					class="w-full cursor-pointer rounded-sm px-2 py-1.5 text-center text-xs font-medium text-primary transition-colors hover:bg-muted/60"
				>
					{m.common_clear_filters()}
				</button>
			</div>
		{/if}
	</Popover.Content>
</Popover.Root>

<script lang="ts">
	import { ArrowLeftIcon, ExpandIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';

	import ActivityDetailView from './activity-detail-view.svelte';
	import { activityGroupKey, activityGroupStatus, isActiveActivityStatus } from './activity-groups';
	import ActivityIconButton from './activity-icon-button.svelte';
	import ActivityListStates from './activity-list-states.svelte';
	import ActivityOptionsMenu from './activity-options-menu.svelte';
	import ActivityPanelHeader from './activity-panel-header.svelte';
	import ActivityRow from './activity-row.svelte';
	import ActivitySearchBar from './activity-search-bar.svelte';
	import ActivitySectionList from './activity-section-list.svelte';
	import ActivityTrack from './activity-track.svelte';

	const RECENT_LIMIT = 8;

	let {
		titleId,
		dialogTitle = false,
		onExpand
	}: {
		titleId?: string;
		dialogTitle?: boolean;
		/** Opens a fuller layout; without it, the expand button shows the full history inside the tray. */
		onExpand?: () => void;
	} = $props();

	let showAll = $state(false);

	const running = $derived(activityStore.allGroups.filter((group) => isActiveActivityStatus(activityGroupStatus(group))));
	const recent = $derived(
		activityStore.allGroups.filter((group) => !isActiveActivityStatus(activityGroupStatus(group))).slice(0, RECENT_LIMIT)
	);

	function viewAllInternal() {
		if (onExpand) {
			onExpand();
			return;
		}
		showAll = true;
	}
</script>

<div class="flex min-h-0 flex-1 flex-col">
	{#if activityStore.focusedKey}
		<ActivityPanelHeader {titleId} {dialogTitle}>
			{#snippet leading()}
				<ActivityIconButton label={m.common_back()} onclick={() => activityStore.focus(null)} class="-ml-2">
					<ArrowLeftIcon class="size-5" aria-hidden="true" />
				</ActivityIconButton>
			{/snippet}
			{#if onExpand}
				<ActivityIconButton label={m.activity_open_full_view()} onclick={onExpand}>
					<ExpandIcon class="size-4" aria-hidden="true" />
				</ActivityIconButton>
			{/if}
		</ActivityPanelHeader>
		<ActivityDetailView focusKey={activityStore.focusedKey} compact />
	{:else if showAll}
		<ActivityPanelHeader {titleId} {dialogTitle}>
			{#snippet leading()}
				<ActivityIconButton label={m.common_back()} onclick={() => (showAll = false)} class="-ml-2">
					<ArrowLeftIcon class="size-5" aria-hidden="true" />
				</ActivityIconButton>
			{/snippet}
			<ActivityOptionsMenu />
		</ActivityPanelHeader>
		<div class="px-3 pb-2">
			<ActivitySearchBar />
		</div>
		<div class="flex min-h-0 flex-1 flex-col overflow-y-auto border-t border-border/50">
			<ActivityListStates>
				<ActivitySectionList onSelect={(key) => activityStore.focus(key)} />
			</ActivityListStates>
		</div>
	{:else}
		<ActivityPanelHeader {titleId} {dialogTitle}>
			<ActivityOptionsMenu />
			<ActivityIconButton label={m.activity_open_full_view()} onclick={viewAllInternal}>
				<ExpandIcon class="size-4" aria-hidden="true" />
			</ActivityIconButton>
		</ActivityPanelHeader>
		<div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
			<ActivityListStates filtered={false}>
				{#if running.length > 0}
					<div class="flex flex-col gap-2 px-3 pb-2">
						{#each running as group (activityGroupKey(group))}
							<ActivityTrack {group} />
						{/each}
					</div>
				{/if}
				{#if recent.length > 0}
					<h3 class="px-4 pt-2 pb-1 text-2xs font-medium text-muted-foreground">{m.activity_recent()}</h3>
					<div class="flex flex-col px-1.5 pb-1">
						{#each recent as group (activityGroupKey(group))}
							<ActivityRow {group} onSelect={(key) => activityStore.focus(key)} />
						{/each}
					</div>
				{/if}
			</ActivityListStates>
		</div>
	{/if}
</div>

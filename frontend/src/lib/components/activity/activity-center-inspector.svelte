<script lang="ts">
	import { CloseIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';

	import ActivityDetailView from './activity-detail-view.svelte';
	import { activityGroupKey } from './activity-groups';
	import ActivityIconButton from './activity-icon-button.svelte';
	import ActivityListStates from './activity-list-states.svelte';
	import ActivityOptionsMenu from './activity-options-menu.svelte';
	import ActivityPanelHeader from './activity-panel-header.svelte';
	import ActivitySearchBar from './activity-search-bar.svelte';
	import ActivitySectionList from './activity-section-list.svelte';

	let { onClose }: { onClose: () => void } = $props();

	// The detail pane is never blank while there is something to show.
	$effect(() => {
		if (activityStore.focusedKey) return;
		const first = activityStore.runningGroups[0] ?? activityStore.historyGroups[0];
		if (first) activityStore.focus(activityGroupKey(first));
	});
</script>

<div class="grid min-h-0 flex-1 grid-cols-aside-90 grid-rows-1">
	<div class="flex min-h-0 flex-col border-r border-border/50">
		<ActivityPanelHeader dialogTitle>
			<ActivityOptionsMenu />
			<ActivityIconButton label={m.common_close()} onclick={onClose}>
				<CloseIcon class="size-4" aria-hidden="true" />
			</ActivityIconButton>
		</ActivityPanelHeader>
		<div class="px-3 pb-2">
			<ActivitySearchBar />
		</div>
		<div class="flex min-h-0 flex-1 flex-col overflow-y-auto border-t border-border/50">
			<ActivityListStates>
				<ActivitySectionList selectedKey={activityStore.focusedKey} onSelect={(key) => activityStore.focus(key)} />
			</ActivityListStates>
		</div>
	</div>
	<div class="flex min-h-0 min-w-0 flex-col">
		{#if activityStore.focusedKey}
			<ActivityDetailView focusKey={activityStore.focusedKey} />
		{/if}
	</div>
</div>

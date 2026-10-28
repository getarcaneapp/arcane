<script lang="ts">
	import * as InputGroup from '#lib/components/ui/input-group/index.js';
	import { RefreshIcon, SearchIcon } from '#lib/icons/index.js';
	import { m } from '#lib/paraglide/messages.js';
	import { activityStore } from '#lib/stores/activity.store.svelte.js';
	import { cn } from '#lib/utils.js';

	import ActivityFilterPopover from './activity-filter-popover.svelte';
	import ActivityIconButton from './activity-icon-button.svelte';
</script>

<div class="flex items-center gap-1">
	<InputGroup.Root class="h-8 min-w-0 flex-1">
		<InputGroup.Addon>
			<SearchIcon aria-hidden="true" />
		</InputGroup.Addon>
		<InputGroup.Input
			type="search"
			value={activityStore.searchTerm}
			oninput={(event) => activityStore.setSearchTerm(event.currentTarget.value)}
			placeholder={m.activity_search_placeholder()}
			size="sm"
		/>
	</InputGroup.Root>
	<ActivityFilterPopover />
	<ActivityIconButton label={m.common_refresh()} onclick={() => activityStore.refresh()}>
		<RefreshIcon class={cn('size-4', activityStore.loading && 'animate-spin')} aria-hidden="true" />
	</ActivityIconButton>
</div>

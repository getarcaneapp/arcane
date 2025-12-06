<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import { m } from '$lib/paraglide/messages';
	import { useEnvSelector } from './context.svelte';
	import type { EnvironmentFilter } from './types';
	import SavedFilterItem from './saved-filter-item.svelte';

	interface Props {
		onBack?: () => void;
		onApplyFilter?: (filter: EnvironmentFilter) => void;
		onSaveFilter?: (name: string) => void;
		onUpdateFilter?: (filterId: string) => void;
		onDeleteFilter?: (filterId: string) => void;
		onSetDefault?: (filterId: string) => void;
		onClearDefault?: (filterId: string) => void;
		onRenameFilter?: (filterId: string, name: string) => void;
	}

	let {
		onBack,
		onApplyFilter,
		onSaveFilter,
		onUpdateFilter,
		onDeleteFilter,
		onSetDefault,
		onClearDefault,
		onRenameFilter
	}: Props = $props();

	const ctx = useEnvSelector();

	let saveFilterName = $state('');
	let editingFilterId = $state<string | null>(null);
	let editingFilterName = $state('');

	function startEditing(filter: EnvironmentFilter) {
		editingFilterId = filter.id;
		editingFilterName = filter.name;
	}

	function cancelEditing() {
		editingFilterId = null;
		editingFilterName = '';
	}

	function handleSaveEdit(name: string) {
		if (editingFilterId && name.trim()) {
			onRenameFilter?.(editingFilterId, name.trim());
			cancelEditing();
		}
	}

	function handleSaveNew() {
		if (saveFilterName.trim()) {
			onSaveFilter?.(saveFilterName.trim());
			saveFilterName = '';
		}
	}
</script>

<div class="flex flex-col gap-3 pt-2">
	<!-- Header -->
	<div class="flex items-center gap-2">
		<Button variant="ghost" size="sm" class="h-8 px-2" onclick={onBack}>
			<ChevronLeftIcon class="size-4" />
		</Button>
		<span class="text-sm font-medium">{m.env_selector_saved_filters()}</span>
	</div>

	<!-- Save new filter -->
	{#if ctx.hasSaveableFilters}
		<div class="flex gap-2">
			<Input
				type="text"
				placeholder={m.env_selector_filter_name_placeholder()}
				class="h-9 flex-1 text-sm"
				bind:value={saveFilterName}
				onkeydown={(e) => e.key === 'Enter' && handleSaveNew()}
			/>
			<Button size="sm" class="h-9" onclick={handleSaveNew} disabled={!saveFilterName.trim()}>
				<PlusIcon class="mr-1 size-4" />
				{m.common_save()}
			</Button>
		</div>
	{/if}

	<!-- Filters list -->
	<ScrollArea class="max-h-[40vh] min-h-[120px]">
		{#if ctx.savedFilters.length === 0}
			<div class="text-muted-foreground flex h-32 flex-col items-center justify-center text-center">
				<FilterIcon class="mb-2 size-8 opacity-30" />
				<p class="text-sm">{m.env_selector_no_saved_filters()}</p>
				<p class="mt-1 text-xs">{m.env_selector_save_filter_hint()}</p>
			</div>
		{:else}
			<div class="space-y-1 p-1">
				{#each ctx.savedFilters as filter (filter.id)}
					<SavedFilterItem
						{filter}
						isActive={ctx.activeFilterId === filter.id}
						isEditing={editingFilterId === filter.id}
						bind:editingName={editingFilterName}
						showUpdateButton={ctx.isFilterDifferent(filter) && ctx.hasSaveableFilters}
						onApply={() => onApplyFilter?.(filter)}
						onSetDefault={() => onSetDefault?.(filter.id)}
						onClearDefault={() => onClearDefault?.(filter.id)}
						onStartEdit={() => startEditing(filter)}
						onCancelEdit={cancelEditing}
						onSaveEdit={handleSaveEdit}
						onUpdate={() => onUpdateFilter?.(filter.id)}
						onDelete={() => onDeleteFilter?.(filter.id)}
					/>
				{/each}
			</div>
		{/if}
	</ScrollArea>
</div>

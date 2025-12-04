<script lang="ts">
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as Tooltip from '$lib/components/ui/tooltip/index.js';
	import FilterIcon from '@lucide/svelte/icons/filter';
	import CheckIcon from '@lucide/svelte/icons/check';
	import XIcon from '@lucide/svelte/icons/x';
	import StarIcon from '@lucide/svelte/icons/star';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import SaveIcon from '@lucide/svelte/icons/save';
	import PencilIcon from '@lucide/svelte/icons/pencil';
	import { cn } from '$lib/utils';
	import { m } from '$lib/paraglide/messages';
	import type { EnvironmentFilter } from './types';

	interface Props {
		filter: EnvironmentFilter;
		isActive?: boolean;
		isEditing?: boolean;
		editingName?: string;
		showUpdateButton?: boolean;
		onApply?: () => void;
		onSetDefault?: () => void;
		onClearDefault?: () => void;
		onStartEdit?: () => void;
		onCancelEdit?: () => void;
		onSaveEdit?: (name: string) => void;
		onUpdate?: () => void;
		onDelete?: () => void;
	}

	let {
		filter,
		isActive = false,
		isEditing = false,
		editingName = $bindable(''),
		showUpdateButton = false,
		onApply,
		onSetDefault,
		onClearDefault,
		onStartEdit,
		onCancelEdit,
		onSaveEdit,
		onUpdate,
		onDelete
	}: Props = $props();
</script>

<div
	class={cn(
		'group flex items-center gap-3 rounded-lg p-2.5 transition-all',
		isActive ? 'bg-primary/10 ring-primary/40 ring-1' : 'hover:bg-muted/60'
	)}
>
	{#if isEditing}
		<!-- Editing mode -->
		<div class="flex min-w-0 flex-1 items-center gap-2">
			<Input
				type="text"
				class="h-8 text-sm"
				bind:value={editingName}
				onkeydown={(e) => {
					if (e.key === 'Enter') onSaveEdit?.(editingName);
					if (e.key === 'Escape') onCancelEdit?.();
				}}
			/>
			<Button size="sm" class="h-8 px-2" onclick={() => onSaveEdit?.(editingName)} disabled={!editingName.trim()}>
				<CheckIcon class="size-4" />
			</Button>
			<Button variant="ghost" size="sm" class="h-8 px-2" onclick={onCancelEdit}>
				<XIcon class="size-4" />
			</Button>
		</div>
	{:else}
		<!-- Normal mode -->
		<button class="flex min-w-0 flex-1 items-center gap-3 text-left" onclick={onApply}>
			<div
				class={cn(
					'flex size-9 items-center justify-center rounded-md',
					isActive ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
				)}
			>
				<FilterIcon class="size-4" />
			</div>
			<div class="min-w-0 flex-1">
				<div class="flex items-center gap-2">
					<span class="truncate text-sm font-medium">{filter.name}</span>
					{#if filter.isDefault}
						<StarIcon class="size-3 shrink-0 fill-yellow-500 text-yellow-500" />
					{/if}
					{#if isActive}
						<CheckIcon class="text-primary size-4 shrink-0" />
					{/if}
				</div>
				<div class="mt-1 flex flex-wrap items-center gap-1">
					{#if filter.searchQuery}
						<span
							class="bg-muted text-muted-foreground truncate rounded px-1.5 py-0.5 text-[10px] font-medium italic"
							title={filter.searchQuery}
						>
							"{filter.searchQuery.length > 15 ? filter.searchQuery.slice(0, 15) + '…' : filter.searchQuery}"
						</span>
					{/if}
					{#if filter.statusFilter !== 'all'}
						<span
							class="bg-muted text-muted-foreground inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium"
						>
							<span class={cn('size-1.5 rounded-full', filter.statusFilter === 'online' ? 'bg-emerald-500' : 'bg-red-500')}
							></span>
							{filter.statusFilter === 'online' ? m.common_online() : m.common_offline()}
						</span>
					{/if}
					{#if filter.selectedTags.length > 0}
						{#each filter.selectedTags.slice(0, 2) as tag}
							<span class="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400"
								>{tag}</span
							>
						{/each}
						{#if filter.selectedTags.length > 2}
							<Tooltip.Provider>
								<Tooltip.Root>
									<Tooltip.Trigger>
										<span
											class="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400"
											>+{filter.selectedTags.length - 2}</span
										>
									</Tooltip.Trigger>
									<Tooltip.Content>
										<p class="text-xs">{filter.selectedTags.slice(2).join(', ')}</p>
									</Tooltip.Content>
								</Tooltip.Root>
							</Tooltip.Provider>
						{/if}
					{/if}
					{#if filter.excludedTags.length > 0}
						{#each filter.excludedTags.slice(0, 2) as tag}
							<span class="bg-destructive/15 text-destructive rounded px-1.5 py-0.5 text-[10px] font-medium">{tag}</span>
						{/each}
						{#if filter.excludedTags.length > 2}
							<Tooltip.Provider>
								<Tooltip.Root>
									<Tooltip.Trigger>
										<span class="bg-destructive/15 text-destructive rounded px-1.5 py-0.5 text-[10px] font-medium"
											>+{filter.excludedTags.length - 2}</span
										>
									</Tooltip.Trigger>
									<Tooltip.Content>
										<p class="text-xs">{filter.excludedTags.slice(2).join(', ')}</p>
									</Tooltip.Content>
								</Tooltip.Root>
							</Tooltip.Provider>
						{/if}
					{/if}
					{#if filter.groupBy !== 'none'}
						<span class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px] font-medium">
							{filter.groupBy === 'status' ? m.common_status() : m.common_tags()}
						</span>
					{/if}
					{#if filter.statusFilter === 'all' && filter.selectedTags.length === 0 && filter.excludedTags.length === 0 && filter.groupBy === 'none'}
						<span class="text-muted-foreground text-xs">{m.common_all()}</span>
					{/if}
				</div>
			</div>
		</button>

		<Tooltip.Provider>
			<div class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
				{#if filter.isDefault}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<Button variant="ghost" size="sm" class="size-7 p-0 text-yellow-500 hover:text-yellow-600" onclick={onClearDefault}>
								<StarIcon class="size-3.5 fill-current" />
							</Button>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.env_selector_clear_default()}</Tooltip.Content>
					</Tooltip.Root>
				{:else}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<Button variant="ghost" size="sm" class="size-7 p-0" onclick={onSetDefault}>
								<StarIcon class="size-3.5" />
							</Button>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.env_selector_set_as_default()}</Tooltip.Content>
					</Tooltip.Root>
				{/if}
				<Tooltip.Root>
					<Tooltip.Trigger>
						<Button variant="ghost" size="sm" class="size-7 p-0" onclick={onStartEdit}>
							<PencilIcon class="size-3.5" />
						</Button>
					</Tooltip.Trigger>
					<Tooltip.Content>{m.common_rename()}</Tooltip.Content>
				</Tooltip.Root>
				{#if showUpdateButton}
					<Tooltip.Root>
						<Tooltip.Trigger>
							<Button variant="ghost" size="sm" class="size-7 p-0" onclick={onUpdate}>
								<SaveIcon class="size-3.5" />
							</Button>
						</Tooltip.Trigger>
						<Tooltip.Content>{m.env_selector_update_filter()}</Tooltip.Content>
					</Tooltip.Root>
				{/if}
				<Tooltip.Root>
					<Tooltip.Trigger>
						<Button variant="ghost" size="sm" class="size-7 p-0 text-red-500 hover:text-red-600" onclick={onDelete}>
							<Trash2Icon class="size-3.5" />
						</Button>
					</Tooltip.Trigger>
					<Tooltip.Content>{m.common_delete()}</Tooltip.Content>
				</Tooltip.Root>
			</div>
		</Tooltip.Provider>
	{/if}
</div>

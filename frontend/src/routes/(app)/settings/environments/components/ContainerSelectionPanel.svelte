<script lang="ts">
	import { Spinner } from '$lib/components/ui/spinner';
	import { Label } from '$lib/components/ui/label';
	import { Input } from '$lib/components/ui/input';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import * as ScrollArea from '$lib/components/ui/scroll-area';
	import { m } from '$lib/paraglide/messages';
	import type { ContainerSummaryDto } from '$lib/types/container.type';

	type ContainerListItem = {
		value: string;
		label: string;
		selected: boolean;
		disabled?: boolean;
		hint?: string;
	};

	let {
		title,
		description,
		containersPromise,
		mapContainerToItem,
		toggleItem,
		inputIdPrefix,
		searchTerm = $bindable()
	}: {
		title: string;
		description: string;
		containersPromise: Promise<ContainerSummaryDto[]>;
		mapContainerToItem: (container: ContainerSummaryDto) => ContainerListItem;
		toggleItem: (value: string) => void;
		inputIdPrefix: string;
		searchTerm?: string;
	} = $props();

	function getItems(containers: ContainerSummaryDto[]): ContainerListItem[] {
		return containers.map(mapContainerToItem);
	}

	function getFilteredItems(items: ContainerListItem[]): ContainerListItem[] {
		if (!searchTerm) return items;

		const normalizedSearch = searchTerm.toLowerCase();
		return items.filter((item) => item.label.toLowerCase().includes(normalizedSearch));
	}

	function getSelectedCount(containers: ContainerSummaryDto[]): number {
		return getItems(containers).filter((item) => item.selected).length;
	}
</script>

<div class="space-y-3">
	<div class="space-y-1">
		<Label class="text-sm font-medium">
			{title}
			{#await containersPromise then containers}
				<span class="text-muted-foreground ml-1 font-normal">({getSelectedCount(containers)})</span>
			{/await}
		</Label>
		<p class="text-muted-foreground text-xs">{description}</p>
	</div>

	<div class="rounded-md border p-2">
		<Input type="search" placeholder="Search containers..." class="mb-2 h-8" bind:value={searchTerm} />
		<ScrollArea.Root class="h-64 w-full rounded-md border p-2">
			<div class="space-y-2">
				{#await containersPromise}
					<div class="flex items-center justify-center p-4">
						<Spinner class="size-4" />
					</div>
				{:then containers}
					{@const items = getItems(containers)}
					{@const filteredItems = getFilteredItems(items)}

					{#if filteredItems.length === 0}
						<p class="text-muted-foreground py-4 text-center text-sm">{m.common_no_results_found()}</p>
					{:else}
						{#each filteredItems as item (item.value)}
							<div class="flex items-center space-x-2">
								<Checkbox
									id="{inputIdPrefix}-{item.value}"
									checked={item.selected}
									disabled={item.disabled}
									onCheckedChange={() => toggleItem(item.value)}
								/>
								<Label
									for="{inputIdPrefix}-{item.value}"
									class="text-sm font-normal {item.disabled ? 'text-muted-foreground' : ''}"
								>
									{item.label}
									{#if item.hint}
										<span class="ml-1 text-xs opacity-70">{item.hint}</span>
									{/if}
								</Label>
							</div>
						{/each}
					{/if}
				{:catch error}
					<div class="text-destructive p-2 text-sm">{error.message || 'Failed to load containers'}</div>
				{/await}
			</div>
		</ScrollArea.Root>
	</div>
</div>

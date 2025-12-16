<script lang="ts">
	import { getAvailableMobileNavItems } from '$lib/config/navigation-config';
	import { Button } from '$lib/components/ui/button';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { m } from '$lib/paraglide/messages';
	import { CloseIcon } from '$lib/icons';

	let {
		selectedTabs = $bindable([]),
		disabled = false
	}: {
		selectedTabs: string[];
		disabled?: boolean;
	} = $props();

	const availableItems = getAvailableMobileNavItems();

	const selectedItems = $derived(
		selectedTabs.map((url) => availableItems.find((item) => item.url === url)).filter((item) => item !== undefined)
	);

	const unselectedItems = $derived(availableItems.filter((item) => !selectedTabs.includes(item.url)));

	const canAddMore = $derived(selectedTabs.length < 4);
	const isComplete = $derived(selectedTabs.length === 4);

	function addTab(url: string) {
		if (selectedTabs.length < 4 && !selectedTabs.includes(url)) {
			selectedTabs = [...selectedTabs, url];
		}
	}

	function removeTab(url: string) {
		selectedTabs = selectedTabs.filter((tab) => tab !== url);
	}
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<Label class="text-base">{m.mobile_dock_tabs()}</Label>
		<Badge variant={isComplete ? 'default' : 'secondary'}>
			{selectedTabs.length}/4
		</Badge>
	</div>

	<div class="bg-muted/50 rounded-lg border p-4">
		<p class="text-muted-foreground mb-3 text-sm">{m.mobile_dock_tabs_description()}</p>

		{#if selectedItems.length > 0}
			<div class="mb-4 flex flex-wrap gap-2">
				{#each selectedItems as item, index (item.url)}
					<Button variant="outline" size="sm" class="gap-2" {disabled} onclick={() => removeTab(item.url)}>
						{@const Icon = item.icon}
						<Icon class="size-4" />
						<span class="text-xs">{index + 1}. {item.title}</span>
						<CloseIcon class="size-3" />
					</Button>
				{/each}
			</div>
		{/if}

		{#if canAddMore && unselectedItems.length > 0}
			<div class="space-y-2">
				<Label class="text-muted-foreground text-xs">{m.available_tabs()}</Label>
				<div class="flex flex-wrap gap-2">
					{#each unselectedItems as item (item.url)}
						<Button variant="ghost" size="sm" class="gap-2" {disabled} onclick={() => addTab(item.url)}>
							{@const Icon = item.icon}
							<Icon class="size-4" />
							<span class="text-xs">{item.title}</span>
						</Button>
					{/each}
				</div>
			</div>
		{/if}

		{#if !isComplete}
			<p class="text-destructive mt-3 text-xs">{m.mobile_dock_tabs_required()}</p>
		{/if}
	</div>
</div>

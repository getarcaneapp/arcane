<script lang="ts">
	import { Switch } from '$lib/components/ui/switch';
	import type { Environment } from '$lib/types/environment';

	let {
		environments,
		selected = $bindable([]),
		disabled = false
	}: {
		environments: Environment[];
		selected: string[];
		disabled?: boolean;
	} = $props();

	function toggle(id: string, checked: boolean) {
		if (checked) {
			if (!selected.includes(id)) selected = [...selected, id];
		} else {
			selected = selected.filter((envId) => envId !== id);
		}
	}
</script>

<div class="max-h-48 overflow-y-auto rounded-md border border-border/50">
	{#each environments as environment (environment.id)}
		<label class="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-accent/40">
			<span class="min-w-0 flex-1 truncate text-sm">{environment.name}</span>
			<Switch
				checked={selected.includes(environment.id)}
				{disabled}
				onCheckedChange={(checked) => toggle(environment.id, checked === true)}
			/>
		</label>
	{/each}
</div>

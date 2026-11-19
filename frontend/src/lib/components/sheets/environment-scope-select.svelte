<script lang="ts">
	import EnvironmentMultiSelect from '#lib/components/sheets/environment-multi-select.svelte';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as m from '#lib/paraglide/messages.js';
	import type { Environment } from '#lib/types/environment.js';

	let {
		environments,
		scope = $bindable('all'),
		selected = $bindable([]),
		error = $bindable(null)
	}: {
		environments: Environment[];
		scope: 'all' | 'specific';
		selected: string[];
		error: string | null;
	} = $props();
</script>

<div>
	<Label class="mb-0">{m.common_scope()}</Label>
	<RadioGroup.Root
		class="mt-2"
		value={scope}
		onValueChange={(value) => {
			scope = value as 'all' | 'specific';
			error = null;
		}}
	>
		<div class="grid gap-2">
			<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
				<RadioGroup.Item value="all" class="mt-0.5" />
				<div class="grid gap-1 leading-none">
					<span class="text-sm font-medium">{m.all_environments()}</span>
					<span class="text-xs text-muted-foreground">{m.all_environments_description()}</span>
				</div>
			</label>
			<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 p-3 hover:bg-accent/40">
				<RadioGroup.Item value="specific" class="mt-0.5" />
				<div class="grid gap-1 leading-none">
					<span class="text-sm font-medium">{m.specific_environments()}</span>
					<span class="text-xs text-muted-foreground">{m.specific_environments_description()}</span>
				</div>
			</label>
		</div>
	</RadioGroup.Root>
	{#if scope === 'specific'}
		<div class="mt-2">
			<EnvironmentMultiSelect {environments} bind:selected />
		</div>
	{/if}
	{#if error}
		<p class="mt-1 text-sm text-destructive">{error}</p>
	{/if}
</div>
